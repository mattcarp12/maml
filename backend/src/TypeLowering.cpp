#include "TypeLowering.h"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>

namespace maml {

llvm::Type *llvmTypeFor(CodegenContext &ctx, const nlohmann::json &typeJson) {
  if (typeJson.is_null()) return llvm::Type::getVoidTy(ctx.Context);

  std::string cacheKey = typeJson.dump();
  if (ctx.TypeCache.find(cacheKey) != ctx.TypeCache.end()) {
    return ctx.TypeCache[cacheKey];
  }

  llvm::Type *resultType = nullptr;

  // 1. Handle explicit primitive strings (e.g., "i32", "ptr")
  if (typeJson.is_string()) {
    std::string_view kind = typeJson.get<std::string_view>();
    if (kind == "i32") {
      resultType = llvm::Type::getInt32Ty(ctx.Context);
    } else if (kind == "i1") {
      resultType = llvm::Type::getInt1Ty(ctx.Context);
    } else if (kind == "void") {
      resultType = llvm::Type::getVoidTy(ctx.Context);
    } else if (kind == "ptr") {
      resultType = llvm::PointerType::getUnqual(ctx.Context);
    } else if (kind == "string") {
      resultType = llvm::StructType::get(
          ctx.Context, {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});
    } else {
      ctx.Error.fatal("Unknown primitive type: " + std::string(kind), typeJson);
    }
  }

  // 2. Handle compound types (structs, sum types, etc.)
  else if (typeJson.is_object()) {
    if (!typeJson.contains("kind")) {
      ctx.Error.fatal("Compound type missing 'kind' field", typeJson);
      return llvm::Type::getVoidTy(ctx.Context);
    }

    std::string_view kind = typeJson["kind"].get<std::string_view>();

    if (kind == "struct") {
      // Build a real LLVM StructType from the field layout emitted by lowerType.
      //
      // The MIR exporter now always includes a "fields" array on struct type
      // descriptors (each entry has "index", "name", and "type").  We use this
      // to construct a named, layout-compatible llvm::StructType so that
      // TempDeclInst allocas, StructInitInst GEPs, and FieldReadInst GEPs all
      // operate on the same concrete type rather than an opaque pointer.
      //
      // Named struct types are interned in the LLVM context by name, so
      // repeated calls for the same struct produce the same type object.
      // We still cache by JSON key to avoid redundant construction work.

      if (!typeJson.contains("fields") || typeJson["fields"].is_null()) {
        // Fallback: no field layout available — return opaque pointer.
        // This should only happen for types the optimizer has erased or for
        // structs used purely as pointer targets (e.g. opaque extern types).
        resultType = llvm::PointerType::getUnqual(ctx.Context);
      } else {
        std::string structName = typeJson.value("name", "__anon_struct");

        // Check if the LLVM context already has a named struct with this name.
        // llvm::StructType::getTypeByName is the canonical way to look one up.
        llvm::StructType *existingST = llvm::StructType::getTypeByName(ctx.Context, structName);
        if (existingST && !existingST->isOpaque()) {
          // Already fully defined — use it directly.
          resultType = existingST;
        } else {
          // Build field type list in declaration order (fields are sorted by
          // "index" in the JSON, matching the order emitted by export.go).
          const auto &fieldsJson = typeJson["fields"];

          // Sort by index to guarantee declaration order regardless of JSON
          // iteration order (nlohmann::json preserves insertion order for
          // objects, but the array was built in order by Go, so this is
          // defensive).
          std::vector<size_t> order(fieldsJson.size());
          std::iota(order.begin(), order.end(), 0);
          std::sort(order.begin(), order.end(), [&](size_t a, size_t b) {
            return fieldsJson[a]["index"].get<int>() < fieldsJson[b]["index"].get<int>();
          });

          std::vector<llvm::Type *> fieldTypes;
          fieldTypes.reserve(fieldsJson.size());
          for (size_t i : order) {
            fieldTypes.push_back(llvmTypeFor(ctx, fieldsJson[i]["type"]));
          }

          // Create or complete the named struct.
          llvm::StructType *st = existingST ? existingST : llvm::StructType::create(ctx.Context, structName);
          st->setBody(fieldTypes, /*isPacked=*/false);
          resultType = st;
        }
      }

    } else if (kind == "sum_type") {
      // Sum types are structurally lowered as a tagged union.
      // Layout: { i32 discriminant, [payloadSize x i8] max_payload_buffer }
      std::string structName = typeJson.value("name", "__anon_sum_type");

      // Check if we've already defined this SumType in the LLVM Context
      llvm::StructType *existingST = llvm::StructType::getTypeByName(ctx.Context, structName);
      if (existingST && !existingST->isOpaque()) {
        resultType = existingST;
      } else {
        // Calculate payload size: Total Size - 4 bytes for the i32 discriminant
        int totalSize = typeJson.value("size", 8);  // Fallback to 8 if missing
        // Calculate payload size in terms of 8-byte (i64) blocks to guarantee pointer alignment!
        int payloadSize = totalSize > 4 ? totalSize - 4 : 0;
        int numBlocks = (payloadSize + 7) / 8;  // Round up to nearest 8-byte boundary

        llvm::Type *discrimTy = llvm::Type::getInt32Ty(ctx.Context);
        llvm::Type *payloadTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(ctx.Context), numBlocks);
        llvm::StructType *st = existingST ? existingST : llvm::StructType::create(ctx.Context, structName);
        st->setBody({discrimTy, payloadTy}, /*isPacked=*/false);
        resultType = st;
      }

    } else if (kind == "array") {
      llvm::Type *elemTy = llvmTypeFor(ctx, typeJson["elem_type"]);
      int size = typeJson["size"].get<int>();
      resultType = llvm::ArrayType::get(elemTy, size);

    } else if (kind == "slice" || kind == "vector") {
      // { ptr raw, ptr data, i32 len, i32 cap }
      resultType = llvm::StructType::get(ctx.Context, {
                                                          llvm::PointerType::getUnqual(ctx.Context),
                                                          llvm::PointerType::getUnqual(ctx.Context),
                                                          llvm::Type::getInt32Ty(ctx.Context),
                                                          llvm::Type::getInt32Ty(ctx.Context),
                                                      });
    } else {
      ctx.Error.fatal("Unknown compound type kind: " + std::string(kind), typeJson);
    }
  } else {
    ctx.Error.fatal("Invalid type format in MIR", typeJson);
  }

  if (resultType) {
    ctx.TypeCache[cacheKey] = resultType;
  }

  return resultType;
}

}  // namespace maml