// =============================================================================
// backend/src/TypeLowering.cpp
// =============================================================================

#include "TypeLowering.h"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>
#include <llvm/IR/Module.h>

#include <string_view>

#include "mir/types_generated.hpp"

namespace maml {

// Forward declaration to allow recursive type resolution inside the visitor
llvm::Type* llvmTypeForVariant(CodegenContext& ctx, const maml::Type& generatedType);

// -----------------------------------------------------------------------------
// std::visit Router Engine
// -----------------------------------------------------------------------------

struct TypeVisitor {
  CodegenContext& ctx;

  // --- Primitives ---
  llvm::Type* operator()(const IntType&) { return llvm::Type::getInt32Ty(ctx.Context); }
  llvm::Type* operator()(const BoolType&) { return llvm::Type::getInt1Ty(ctx.Context); }
  llvm::Type* operator()(const UnitType&) { return llvm::Type::getVoidTy(ctx.Context); }
  llvm::Type* operator()(const AnyType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const PtrType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const UnknownType&) {
    ctx.Error.fatal("Unknown primitive type reached backend pipeline.");
    return nullptr;
  }

  llvm::Type* operator()(const StringType&) {
    // String is a fat pointer: { ptr, i32 len }
    return llvm::StructType::get(ctx.Context,
                                 {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});
  }

  // --- Composites ---
  llvm::Type* operator()(const StructType& t) {
    llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, t.name);
    if (existingST && !existingST->isOpaque()) {
      return existingST;
    }

    std::vector<llvm::Type*> fieldTypes;
    fieldTypes.reserve(t.fields.size());

    for (const auto& field : t.fields) {
      fieldTypes.push_back(llvmTypeForVariant(ctx, *field.type));
    }

    llvm::StructType* st = existingST ? existingST : llvm::StructType::create(ctx.Context, t.name);

    // Note: isPacked=false tells LLVM to apply standard alignment padding.
    // If MAML dictates a custom packed layout, the Go exporter must pass the fields in the exact sorted order.
    st->setBody(fieldTypes, /*isPacked=*/false);
    return st;
  }

  llvm::Type* operator()(const SumType& t) {
    llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, t.base_name);
    if (existingST && !existingST->isOpaque()) {
      return existingST;
    }

    // We must dynamically compute the maximum payload size across all variants
    // using LLVM's target-aware DataLayout, since the frontend no longer hardcodes it.
    uint64_t maxPayloadSize = 0;
    const llvm::DataLayout& DL = ctx.Module->getDataLayout();

    for (const auto& variant : t.variants) {
      std::vector<llvm::Type*> payloadFields;

      // Variants can hold named fields...
      for (const auto& f : variant.fields) {
        payloadFields.push_back(llvmTypeForVariant(ctx, *f.type));
      }
      // ...or unnamed tuple types
      for (const auto& tupleTy : variant.tuple_types) {
        payloadFields.push_back(llvmTypeForVariant(ctx, *tupleTy));
      }

      if (!payloadFields.empty()) {
        llvm::StructType* variantTy = llvm::StructType::get(ctx.Context, payloadFields, false);
        uint64_t variantSize = DL.getTypeAllocSize(variantTy);
        if (variantSize > maxPayloadSize) {
          maxPayloadSize = variantSize;
        }
      }
    }

    // Calculate how many i64 blocks we need to hold the largest payload
    uint64_t numBlocks = (maxPayloadSize + 7) / 8;

    llvm::Type* discrimTy = llvm::Type::getInt32Ty(ctx.Context);
    llvm::Type* payloadTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(ctx.Context), numBlocks);

    llvm::StructType* st = existingST ? existingST : llvm::StructType::create(ctx.Context, t.base_name);
    st->setBody({discrimTy, payloadTy}, /*isPacked=*/false);
    return st;
  }

  // --- Containers ---
  llvm::Type* operator()(const ArrayType& t) { return llvm::ArrayType::get(llvmTypeForVariant(ctx, *t.base), t.size); }

  llvm::Type* operator()(const ViewType&) {
    // View is a slice fat pointer: { raw_ptr, data_ptr, len, cap }
    return llvm::StructType::get(ctx.Context, {
                                                  llvm::PointerType::getUnqual(ctx.Context),
                                                  llvm::PointerType::getUnqual(ctx.Context),
                                                  llvm::Type::getInt32Ty(ctx.Context),
                                                  llvm::Type::getInt32Ty(ctx.Context),
                                              });
  }

  // Heap-allocated dynamic containers decay to simple pointers in the LLVM IR
  llvm::Type* operator()(const VectorType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const MapType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const FutureType&) { return llvm::PointerType::getUnqual(ctx.Context); }
};

// Helper function to initiate the std::visit loop
llvm::Type* llvmTypeForVariant(CodegenContext& ctx, const maml::Type& generatedType) {
  return std::visit(TypeVisitor{ctx}, generatedType.inner);
}

// -----------------------------------------------------------------------------
// Public AST/MIR Type Router Entry Point
// -----------------------------------------------------------------------------

llvm::Type* llvmTypeFor(CodegenContext& ctx, const nlohmann::json& typeJson) {
  if (typeJson.is_null()) {
    return llvm::Type::getVoidTy(ctx.Context);
  }

  std::string cacheKey = typeJson.dump();
  auto cached = ctx.TypeCache.find(cacheKey);
  if (cached != ctx.TypeCache.end()) {
    return cached->second;
  }

  // 1. Parse the JSON directly into the schema-generated C++ structs.
  // If the frontend exported malformed MIR that violates the YAML schema,
  // nlohmann::json will throw a safe exception here.
  maml::Type safeType;
  try {
    safeType = typeJson.get<maml::Type>();
  } catch (const std::exception& e) {
    ctx.Error.fatal(std::string("Schema Deserialization Error: ") + e.what());
    return nullptr;
  }

  // 2. Dispatch the type to LLVM safely using std::visit
  llvm::Type* resultType = llvmTypeForVariant(ctx, safeType);

  if (resultType) {
    ctx.TypeCache[cacheKey] = resultType;
  }

  return resultType;
}

std::string_view getTypeKind(const nlohmann::json& typeJson) {
  if (typeJson.is_string()) {
    return typeJson.get<std::string_view>();
  }
  if (typeJson.is_object() && typeJson.contains("kind")) {
    return typeJson["kind"].get<std::string_view>();
  }
  return "";
}

}  // namespace maml