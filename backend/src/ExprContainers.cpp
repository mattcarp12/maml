#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include <iostream>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view strVal = expr["value"].get<std::string_view>();

  llvm::Type *strTy = llvm::StructType::get(
      ctx.Context, {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});

  // Global source
  llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, strVal, true);
  llvm::GlobalVariable *globalStr = new llvm::GlobalVariable(*ctx.Module, strConst->getType(), true,
                                                             llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");

  llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
  const size_t dataSize = strVal.length() + 1;

  llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), dataSize);
  llvm::Value *heapPtr = Builder->CreateCall(allocFn, {sizeVal}, "str_heap_alloc");

  // Copy the string data
  Builder->CreateMemCpy(heapPtr, llvm::MaybeAlign(1), globalStr, llvm::MaybeAlign(1), dataSize);

  // Create the fat pointer on stack
  llvm::AllocaInst *headerAlloca = Builder->CreateAlloca(strTy, nullptr, "str_header");

  // Store data pointer
  llvm::Value *dataGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(0)});
  Builder->CreateStore(heapPtr, dataGep);

  // Store length (excluding null)
  llvm::Value *lenGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(1)});
  Builder->CreateStore(Builder->getInt32(strVal.length()), lenGep);

  // After populating headerAlloca:
  return ctx.Builder->CreateLoad(strTy, headerAlloca, "str_literal_val");
}

llvm::Value *compileIndexExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;

  // All type info comes from the DTO itself, never from sub-expression nodes.
  const auto &leftTypeJson = expr["left_type"];
  const auto &elemTypeJson = expr["elem_type"];

  llvm::Value *leftVal = evaluateExpression(ctx, expr["left"]);
  llvm::Value *indexVal = evaluateExpression(ctx, expr["index"]);
  llvm::Type *elemTy = llvmTypeFor(ctx, elemTypeJson);

  // After flattening, leftVal is loaded through its alloca by compileIdentifier.
  // For GEP we need the raw alloca pointer. Re-resolve it from the symbol table.
  llvm::Value *leftPtr = leftVal;
  if (expr["left"].contains("value")) {
    std::string leftName = expr["left"]["value"].get<std::string>();
    if (llvm::Value *sym = ctx.resolveSymbol(leftName)) {
      if (llvm::isa<llvm::AllocaInst>(sym)) {
        leftPtr = sym;
      }
    }
  }

  std::string_view kind = "unknown";
  if (leftTypeJson.is_object() && leftTypeJson.contains("kind")) {
    kind = leftTypeJson["kind"].get<std::string_view>();
  }

  llvm::Value *elemPtr = nullptr;

  if (kind == "array") {
    llvm::Type *arrayTy = llvmTypeFor(ctx, leftTypeJson);
    elemPtr = Builder->CreateGEP(arrayTy, leftPtr, {Builder->getInt32(0), indexVal}, "array_elem_ptr");

  } else if (kind == "string") {
    // Fat pointer: { ptr, i32 }. Field 0 is the data pointer.
    llvm::Type *strTy = llvmTypeFor(ctx, leftTypeJson);
    llvm::Value *dataPtrGep =
        Builder->CreateGEP(strTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(0)}, "str_data_gep");
    llvm::Value *dataPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "str_data_ptr");
    elemPtr = Builder->CreateGEP(llvm::Type::getInt8Ty(ctx.Context), dataPtr, indexVal, "char_ptr");

  } else if (kind == "slice" || kind == "vector") {
    // Header layout: { raw_ptr, data_ptr, len, cap } — field 1 is the data pointer.
    llvm::Type *hdrTy = llvmTypeFor(ctx, leftTypeJson);
    llvm::Value *dataPtrGep =
        Builder->CreateGEP(hdrTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)}, "slice_data_gep");
    llvm::Value *dataPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "slice_data_ptr");
    elemPtr = Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_elem_ptr");

  } else if (kind == "map") {
    // Map lookup: m[key]
    llvm::Value *mapHeader = leftVal;
    if (!mapHeader->getType()->isPointerTy()) {
      if (expr["left"].contains("value")) {
        std::string mapName = expr["left"]["value"].get<std::string>();
        if (llvm::Value *sym = ctx.resolveSymbol(mapName)) {
          mapHeader = sym;
        }
      }
    }

    llvm::Value *indexVal = evaluateExpression(ctx, expr["index"]);
    llvm::Value *keyHash = Builder->CreateZExtOrTrunc(indexVal, llvm::Type::getInt64Ty(ctx.Context), "map_key_hash");

    llvm::Function *getFn = ctx.Module->getFunction(rt::MAP_GET);
    llvm::Value *nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));

    std::vector<llvm::Value *> args = {
        mapHeader, keyHash,
        nullPtr,              // str_key_ptr
        Builder->getInt32(0)  // str_key_len
    };

    llvm::Value *valuePtr = Builder->CreateCall(getFn, args, "map_get_ptr");

    // Load the actual value (assume i32 for map_stress)
    llvm::Type *elemTy = llvmTypeFor(ctx, expr["elem_type"]);
    return Builder->CreateLoad(elemTy, valuePtr, "map_value");
  } else {
    ctx.Error.fatal("index_read: unrecognised container kind '" + std::string(kind) + "'", expr);
    return nullptr;
  }

  return Builder->CreateLoad(elemTy, elemPtr, "elem_load");
}

// -----------------------------------------------------------------------------
// compileFieldAccess
//
// This function is now a FALLBACK for the legacy `field_access` expression op.
// Under normal compilation the MIR flatten pass decomposes every FieldAccess
// into a `field_read` STATEMENT (handled in StmtGenerator.cpp), so this path
// should never be reached for named-struct field access.
//
// It is retained for:
//   (a) SumType discriminant / payload field access (still routed through the
//       expression evaluator because match desugaring emits it directly), and
//   (b) Any regression path that somehow bypasses flattening.
//
// Key change from the old implementation: named-struct field access no longer
// relies on `e["object"]["maml_type"]` being present in the expression DTO
// (it is not, post-rewrite).  Instead we use the pre-resolved `field_index`
// that the MIR exporter attaches directly to the field_access op.
// -----------------------------------------------------------------------------
llvm::Value *compileFieldAccess(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Value *objVal = evaluateExpression(ctx, e["object"]);

  // ---- SumType path (unchanged) ----
  // SumType field_access nodes carry object.maml_type because the match
  // desugarer still emits full TAST FieldAccess nodes annotated with the
  // sum type descriptor.
  if (e["object"].contains("maml_type") && !e["object"]["maml_type"].is_null()) {
    nlohmann::json structType = e["object"]["maml_type"];

    if (structType.contains("kind") && structType["kind"].get<std::string_view>() == "SumType") {
      llvm::Type *sumTy = llvmTypeFor(ctx, structType);
      llvm::Value *objPtr = objVal;

      if (!objVal->getType()->isPointerTy()) {
        llvm::AllocaInst *spill = Builder->CreateAlloca(sumTy, nullptr, "sum_spill");
        Builder->CreateStore(objVal, spill);
        objPtr = spill;
      }

      std::string_view fieldName =
          e["field_name"].is_string() ? e["field_name"].get<std::string_view>() : e["field"].get<std::string_view>();

      if (fieldName == "__discriminant") {
        llvm::Value *discrimPtr = Builder->CreateGEP(sumTy, objPtr, {Builder->getInt32(0), Builder->getInt32(0)});
        return Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimPtr, "discrim_load");
      }

      int fieldIndex = -1;
      nlohmann::json targetVariant;
      for (const auto &v : structType["variants"]) {
        for (int i = 0; i < (int)v["fields"].size(); ++i) {
          if (v["fields"][i]["name"].get<std::string_view>() == fieldName) {
            targetVariant = v;
            fieldIndex = i;
            break;
          }
        }
        if (fieldIndex != -1) break;
      }

      if (fieldIndex != -1) {
        llvm::Value *payloadPtr =
            Builder->CreateGEP(sumTy, objPtr, {Builder->getInt32(0), Builder->getInt32(1)}, "payload_raw_ptr");

        std::vector<llvm::Type *> payloadTypes;
        for (const auto &f : targetVariant["fields"]) payloadTypes.push_back(llvmTypeFor(ctx, f["type"]));
        llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, payloadTypes);

        llvm::Value *fieldPtr = Builder->CreateGEP(
            variantStructTy, payloadPtr, {Builder->getInt32(0), Builder->getInt32(fieldIndex)}, "variant_field_ptr");

        llvm::Type *fieldTy = llvmTypeFor(ctx, e["maml_type"]);
        if (fieldTy->isArrayTy()) return fieldPtr;
        return Builder->CreateLoad(fieldTy, fieldPtr, "variant_field_load");
      }
    }
  }

  // ---- Named-struct path: use pre-resolved field_index ----
  //
  // field_index is always present on the field_access DTO emitted by export.go
  // (buildExprDTO's safety-net case).  We do NOT require object.maml_type here
  // because after the flatten rewrite the object is always a plain identifier
  // whose LLVM type is already recorded in the symbol table.
  if (!e.contains("field_index") || e["field_index"].get<int>() < 0) {
    ctx.Error.fatal(
        "field_access: missing or negative field_index; "
        "this expression was not properly flattened by the MIR pass",
        e);
    return nullptr;
  }

  int fieldIndex = e["field_index"].get<int>();

  // Re-resolve the object's raw alloca (compileIdentifier loads through it).
  llvm::Value *objPtr = objVal;
  if (e["object"].contains("value")) {
    std::string objName = e["object"]["value"].get<std::string>();
    if (llvm::Value *sym = ctx.resolveSymbol(objName)) {
      if (llvm::isa<llvm::AllocaInst>(sym)) {
        objPtr = sym;
      }
    }
  }
  if (!objPtr->getType()->isPointerTy()) {
    ctx.Error.fatal("field_access: object is not a pointer — cannot GEP", e);
    return nullptr;
  }

  llvm::Type *structTy = nullptr;
  if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(objPtr)) {
    structTy = srcAlloca->getAllocatedType();
  } else {
    ctx.Error.fatal("field_access: could not determine struct type from object alloca", e);
    return nullptr;
  }

  std::string fieldNameStr =
      e.contains("field_name") ? e["field_name"].get<std::string>() : e["field"].get<std::string>();

  llvm::Value *fieldPtr = Builder->CreateGEP(structTy, objPtr, {Builder->getInt32(0), Builder->getInt32(fieldIndex)},
                                             fieldNameStr + "_ptr");

  llvm::Type *fieldTy = llvmTypeFor(ctx, e["maml_type"]);
  if (fieldTy->isArrayTy()) return fieldPtr;
  return Builder->CreateLoad(fieldTy, fieldPtr, fieldNameStr + "_load");
}

// -----------------------------------------------------------------------------
// compileZeroAllocExpr
//
// Handles the `alloc_composite` op emitted for Map / Vec / SumType literals
// that pass through the MIR as a single AssignInst rather than being
// decomposed field-by-field.  Named struct literals are NEVER routed here;
// they are handled by struct_init instructions.
//
// The "type" field on the DTO is the full lowerType() output and is always
// present for the alloc_composite path.
// -----------------------------------------------------------------------------
llvm::Value *compileZeroAllocExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  // Prefer "type" (alloc_composite) but also accept "maml_type" (legacy alloc_struct).
  const nlohmann::json *typeNode = nullptr;
  if (expr.contains("type") && !expr["type"].is_null()) {
    typeNode = &expr["type"];
  } else if (expr.contains("maml_type") && !expr["maml_type"].is_null()) {
    typeNode = &expr["maml_type"];
  }

  if (!typeNode) {
    ctx.Error.fatal("CRITICAL: Missing type descriptor on alloc_composite / alloc_struct expr!", expr);
    return nullptr;
  }

  // 1. Resolve the LLVM type.
  llvm::Type *allocTy = llvmTypeFor(ctx, *typeNode);

  // 2. Allocate the raw memory.
  llvm::AllocaInst *alloca = ctx.Builder->CreateAlloca(allocTy, nullptr, "zero_alloc");

  // 3. Guarantee zero-initialization.
  ctx.Builder->CreateStore(llvm::Constant::getNullValue(allocTy), alloca);

  return alloca;
}

}  // namespace maml