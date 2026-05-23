#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view strVal = expr["value"].get<std::string_view>();
  llvm::Type *strTy = llvmTypeFor(ctx, expr["maml_type"]);

  llvm::StringRef llStr(strVal.data(), strVal.size());
  llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, llStr, true);
  llvm::GlobalVariable *globalStr = new llvm::GlobalVariable(*ctx.Module, strConst->getType(), true,
                                                             llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");

  llvm::AllocaInst *alloca = Builder->CreateAlloca(strTy, nullptr, "str_header");

  // On opaque-pointer LLVM, globalStr is already a ptr — no cast needed.
  llvm::Value *ptrGep = Builder->CreateGEP(strTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
  Builder->CreateStore(globalStr, ptrGep);

  llvm::Value *lenGep = Builder->CreateGEP(strTy, alloca, {Builder->getInt32(0), Builder->getInt32(1)});
  Builder->CreateStore(Builder->getInt32(strVal.length()), lenGep);

  return alloca;
}

llvm::Value *compileVariantLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Type *sumTy = llvmTypeFor(ctx, expr["maml_type"]);
  llvm::AllocaInst *alloca = Builder->CreateAlloca(sumTy, nullptr, "variant_lit");

  std::string_view variantName = expr["variant_name"].get<std::string_view>();
  nlohmann::json typeJson = expr["maml_type"];

  int discriminant = 0;
  nlohmann::json variantDef;
  for (const auto &v : typeJson["variants"]) {
    if (v["name"].get<std::string_view>() == variantName) {
      discriminant = v["discriminant"];
      variantDef = v;
      break;
    }
  }

  // Guard: if variantDef is still null, the variant name doesn't exist in the
  // type. This is a frontend/backend contract violation.
  if (variantDef.is_null()) {
    ctx.Error.fatal("Variant '" + std::string(variantName) + "' not found in sum type definition", expr);
    return nullptr;
  }

  llvm::Value *discrimGep = Builder->CreateGEP(sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
  Builder->CreateStore(Builder->getInt32(discriminant), discrimGep);

  if (!expr["fields"].empty()) {
    llvm::Value *payloadGep = Builder->CreateGEP(sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(1)});
    std::vector<llvm::Type *> payloadTypes;
    for (const auto &f : variantDef["fields"]) payloadTypes.push_back(llvmTypeFor(ctx, f["type"]));
    llvm::StructType *payloadStructTy = llvm::StructType::get(ctx.Context, payloadTypes);

    // payloadGep is already an opaque ptr — no cast needed.
    for (const auto &field : expr["fields"]) {
      std::string_view fieldName = field["name"].get<std::string_view>();
      llvm::Value *fieldVal = evaluateExpression(ctx, field["value"]);

      int index = -1;
      for (int i = 0; i < (int)variantDef["fields"].size(); ++i) {
        if (variantDef["fields"][i]["name"].get<std::string_view>() == fieldName) {
          index = i;
          break;
        }
      }

      if (index < 0) {
        ctx.Error.fatal(
            "Variant field '" + std::string(fieldName) + "' not found in variant '" + std::string(variantName) + "'",
            field);
        return nullptr;
      }

      llvm::Type *fieldTy = llvmTypeFor(ctx, field["value"]["maml_type"]);
      if (fieldVal->getType()->isPointerTy() && (fieldTy->isStructTy() || fieldTy->isArrayTy())) {
        fieldVal = Builder->CreateLoad(fieldTy, fieldVal, "variant_field_load");
      }

      llvm::Value *fieldGep =
          Builder->CreateGEP(payloadStructTy, payloadGep, {Builder->getInt32(0), Builder->getInt32(index)});
      Builder->CreateStore(fieldVal, fieldGep);
    }
  }
  return alloca;
}

llvm::Value *compileSliceExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Value *leftVal = evaluateExpression(ctx, expr["left"]);
  llvm::Type *leftTy = llvmTypeFor(ctx, expr["left"]["maml_type"]);

  llvm::Value *leftPtr = leftVal;
  if (!leftVal->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = Builder->CreateAlloca(leftTy, nullptr, "slice_source_spill");
    Builder->CreateStore(leftVal, spill);
    leftPtr = spill;
  }

  llvm::Value *lowVal =
      expr.contains("low") && !expr["low"].is_null() ? evaluateExpression(ctx, expr["low"]) : Builder->getInt32(0);
  llvm::Value *highVal = nullptr;
  llvm::Value *originalCap = nullptr;
  llvm::Value *originalRawPtr = nullptr;
  llvm::Value *originalDataPtr = nullptr;

  std::string_view leftKind = expr["left"]["maml_type"]["kind"].get<std::string_view>();

  if (leftKind == "Array") {
    int size = expr["left"]["maml_type"]["size"];
    originalCap = Builder->getInt32(size);
    originalDataPtr = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(0)});

    bool isHeapArray = llvm::isa<llvm::LoadInst>(leftVal) || llvm::isa<llvm::CallInst>(leftVal);
    if (isHeapArray || expr.value("is_heap", false) || expr["left"].value("is_heap", false)) {
      originalRawPtr = Builder->CreatePointerCast(leftPtr, llvm::PointerType::getUnqual(ctx.Context));
    } else {
      originalRawPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
    }

    highVal = expr.contains("high") && !expr["high"].is_null() ? evaluateExpression(ctx, expr["high"]) : originalCap;

  } else if (leftKind == "String") {
    llvm::Value *charPtrGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(0)});
    llvm::Value *lenGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)});
    originalRawPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), charPtrGep);
    originalDataPtr = originalRawPtr;
    originalCap = Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
    highVal = expr.contains("high") && !expr["high"].is_null() ? evaluateExpression(ctx, expr["high"]) : originalCap;

  } else if (leftKind == "Slice" || leftKind == "Vector") {
    llvm::Value *rawGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(0)});
    llvm::Value *dataGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)});
    llvm::Value *lenGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(2)});
    llvm::Value *capGep = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(3)});

    originalRawPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), rawGep);
    originalDataPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
    originalCap = Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), capGep);
    highVal = expr.contains("high") && !expr["high"].is_null()
                  ? evaluateExpression(ctx, expr["high"])
                  : Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
  }

  llvm::Value *newLen = Builder->CreateSub(highVal, lowVal, "slice_len");
  llvm::Value *newCap = Builder->CreateSub(originalCap, lowVal, "slice_cap");
  llvm::Type *baseTy = llvmTypeFor(ctx, expr["maml_type"]["base"]);
  llvm::Value *newDataPtr = Builder->CreateGEP(baseTy, originalDataPtr, lowVal, "slice_data_ptr");

  llvm::Type *sliceTy = llvmTypeFor(ctx, expr["maml_type"]);
  llvm::AllocaInst *sliceAlloca = Builder->CreateAlloca(sliceTy, nullptr, "slice_header");

  Builder->CreateStore(originalRawPtr,
                       Builder->CreateGEP(sliceTy, sliceAlloca, {Builder->getInt32(0), Builder->getInt32(0)}));
  Builder->CreateStore(newDataPtr,
                       Builder->CreateGEP(sliceTy, sliceAlloca, {Builder->getInt32(0), Builder->getInt32(1)}));
  Builder->CreateStore(newLen, Builder->CreateGEP(sliceTy, sliceAlloca, {Builder->getInt32(0), Builder->getInt32(2)}));
  Builder->CreateStore(newCap, Builder->CreateGEP(sliceTy, sliceAlloca, {Builder->getInt32(0), Builder->getInt32(3)}));

  if (leftKind == "Slice" || leftKind == "Vector" || leftKind == "String" || leftKind == "Array") {
    llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
        "maml_retain", llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                               {llvm::PointerType::getUnqual(ctx.Context)}, false));
    Builder->CreateCall(retainFn, {originalRawPtr});
  }

  return sliceAlloca;
}

llvm::Value *compileIndexExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Value *leftVal = evaluateExpression(ctx, expr["left"]);
  llvm::Value *indexVal = evaluateExpression(ctx, expr["index"]);
  llvm::Type *leftTy = llvmTypeFor(ctx, expr["left"]["maml_type"]);

  std::string_view leftKind = "Unknown";
  if (expr["left"]["maml_type"].contains("kind")) leftKind = expr["left"]["maml_type"]["kind"].get<std::string_view>();

  llvm::Value *leftPtr = leftVal;
  if (!leftVal->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = Builder->CreateAlloca(leftTy, nullptr, "slice_spill");
    Builder->CreateStore(leftVal, spill);
    leftPtr = spill;
  }

  llvm::Value *elemPtr;
  if (leftTy->isArrayTy()) {
    elemPtr = Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), indexVal}, "array_idx");
  } else if (leftKind == "String") {
    llvm::Value *dataPtrGep =
        Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(0)}, "str_data_ptr");
    llvm::Value *dataPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "str_ptr");
    llvm::Value *charPtr = Builder->CreateGEP(llvm::Type::getInt8Ty(ctx.Context), dataPtr, indexVal, "char_ptr");
    return Builder->CreateLoad(llvm::Type::getInt8Ty(ctx.Context), charPtr, "char_val");
  } else {
    llvm::Value *dataPtr =
        Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)}, "data_ptr");
    llvm::Value *data = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtr);
    elemPtr = Builder->CreateGEP(llvmTypeFor(ctx, expr["maml_type"]), data, indexVal, "slice_idx");
  }

  return Builder->CreateLoad(llvmTypeFor(ctx, expr["maml_type"]), elemPtr, "element_load");
}

llvm::Value *compileFieldAccess(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Value *objVal = evaluateExpression(ctx, e["object"]);
  std::string_view fieldName = e["field"].get<std::string_view>();
  nlohmann::json structType = e["object"]["maml_type"];

  if (structType["kind"].get<std::string_view>() == "SumType") {
    llvm::Type *sumTy = llvmTypeFor(ctx, structType);
    llvm::Value *objPtr = objVal;

    if (!objVal->getType()->isPointerTy()) {
      llvm::AllocaInst *spill = Builder->CreateAlloca(sumTy, nullptr, "sum_spill");
      Builder->CreateStore(objVal, spill);
      objPtr = spill;
    }

    if (fieldName == "0") {
      llvm::Value *discrimPtr = Builder->CreateGEP(sumTy, objPtr, {Builder->getInt32(0), Builder->getInt32(0)});
      return Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimPtr, "discrim_load");
    } else if (fieldName == "1") {
      llvm::Value *payloadPtr = Builder->CreateGEP(sumTy, objPtr, {Builder->getInt32(0), Builder->getInt32(1)});
      llvm::Type *payloadTy = llvmTypeFor(ctx, e["maml_type"]);
      return Builder->CreateLoad(payloadTy, payloadPtr, "payload_load");
    }
  }

  int index = -1;
  for (int i = 0; i < structType["fields"].size(); ++i) {
    if (structType["fields"][i]["name"].get<std::string_view>() == fieldName) {
      index = i;
      break;
    }
  }

  if (index < 0) {
    ctx.Error.fatal("Field '" + std::string(fieldName) + "' not found in struct type", e);
    return nullptr;
  }

  llvm::Type *baseTy = llvmTypeFor(ctx, structType);
  llvm::Value *objPtr = objVal;
  if (!objVal->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = Builder->CreateAlloca(baseTy, nullptr, "struct_spill");
    Builder->CreateStore(objVal, spill);
    objPtr = spill;
  }

  llvm::Value *fieldPtr =
      Builder->CreateGEP(baseTy, objPtr, {Builder->getInt32(0), Builder->getInt32(index)}, "fieldptr");
  llvm::Type *fieldTy = llvmTypeFor(ctx, e["maml_type"]);

  if (fieldTy->isArrayTy()) return fieldPtr;
  return Builder->CreateLoad(fieldTy, fieldPtr, "field_load");
}

llvm::Value *compileArrayLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  llvm::Type *arrayTy = llvmTypeFor(ctx, expr["maml_type"]);
  return ctx.Builder->CreateAlloca(arrayTy, nullptr, "array_lit");
}

llvm::Value *compileStructLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  llvm::Type *structTy = llvmTypeFor(ctx, expr["maml_type"]);
  return ctx.Builder->CreateAlloca(structTy, nullptr, "struct_lit");
}

}  // namespace maml