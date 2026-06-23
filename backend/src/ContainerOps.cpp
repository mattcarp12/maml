#include "ExprGenerator.hpp"
#include "StmtGenerator.hpp"
#include "TypeLowering.hpp"

namespace maml {

void handle(CodegenContext &ctx, const mir::ArrayInitInst &inst) {
  llvm::Value *arrayPtr = ctx.getMemoryBase(inst.dst);
  llvm::Type *arrayTy = ctx.SymbolTypes[inst.dst];
  if (!arrayPtr || !arrayTy) return;

  llvm::Value *elemVal = evaluateValue(ctx, inst.value);
  llvm::Value *elemPtr = ctx.Builder->CreateGEP(
      arrayTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(inst.index)}, inst.dst + "_idx");
  ctx.Builder->CreateStore(elemVal, elemPtr);
}

void handle(CodegenContext &ctx, const mir::SliceInst &inst) {
  llvm::Value *dstPtr = ctx.getMemoryBase(inst.dst);
  llvm::Value *leftVal = evaluateValue(ctx, inst.left);
  llvm::Type *leftTy = llvmTypeFor(ctx, inst.container_type);
  llvm::Value *leftPtr = leftVal;

  // Auto-spill literal arrays
  if (!leftPtr->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = ctx.Builder->CreateAlloca(leftTy, nullptr, "slice_source_spill");
    ctx.Builder->CreateStore(leftVal, spill);
    leftPtr = spill;
  }

  llvm::Value *lowVal = isEmpty(inst.low) ? ctx.Builder->getInt32(0) : evaluateValue(ctx, inst.low);
  llvm::Value *highVal = nullptr;
  llvm::Value *originalCap = nullptr;
  llvm::Value *originalRawPtr = nullptr;
  llvm::Value *originalDataPtr = nullptr;
  llvm::Type *baseTy = nullptr;

  if (auto *arrTy = std::get_if<maml::ArrayType>(&inst.container_type->inner)) {
    originalCap = ctx.Builder->getInt32(arrTy->size);
    originalDataPtr =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "array_data");
    originalRawPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
    highVal = isEmpty(inst.high) ? originalCap : evaluateValue(ctx, inst.high);
    baseTy = llvmTypeFor(ctx, arrTy->base);

  } else if (std::holds_alternative<maml::StringType>(inst.container_type->inner)) {
    llvm::Value *dataGep =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)});
    llvm::Value *lenGep = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
    originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
    originalDataPtr = originalRawPtr;
    originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
    highVal = isEmpty(inst.high) ? originalCap : evaluateValue(ctx, inst.high);
    baseTy = llvm::Type::getInt8Ty(ctx.Context);

  } else if (auto *vecTy = std::get_if<maml::VectorType>(&inst.container_type->inner)) {
    llvm::Value *vecHandle = leftPtr;
    llvm::StructType *hdrTy =
        llvm::StructType::get(ctx.Context, {llvm::PointerType::getUnqual(ctx.Context),
                                            llvm::Type::getInt32Ty(ctx.Context), llvm::Type::getInt32Ty(ctx.Context),
                                            llvm::Type::getInt32Ty(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});
    originalRawPtr = vecHandle;
    originalDataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
    originalCap = ctx.Builder->CreateLoad(
        llvm::Type::getInt32Ty(ctx.Context),
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    highVal = isEmpty(inst.high)
                  ? ctx.Builder->CreateLoad(
                        llvm::Type::getInt32Ty(ctx.Context),
                        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)}))
                  : evaluateValue(ctx, inst.high);
    baseTy = llvmTypeFor(ctx, vecTy->base);

  } else if (auto *viewTy = std::get_if<maml::ViewType>(&inst.container_type->inner)) {
    originalRawPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
    originalDataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    originalCap = ctx.Builder->CreateLoad(
        llvm::Type::getInt32Ty(ctx.Context),
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)}));
    highVal = isEmpty(inst.high)
                  ? ctx.Builder->CreateLoad(
                        llvm::Type::getInt32Ty(ctx.Context),
                        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)}))
                  : evaluateValue(ctx, inst.high);
    baseTy = llvmTypeFor(ctx, viewTy->base);
  }

  if (lowVal->getType() != ctx.Builder->getInt32Ty()) {
    lowVal = ctx.Builder->CreateZExtOrTrunc(lowVal, ctx.Builder->getInt32Ty(), "low_cast");
  }
  if (highVal->getType() != ctx.Builder->getInt32Ty()) {
    highVal = ctx.Builder->CreateZExtOrTrunc(highVal, ctx.Builder->getInt32Ty(), "high_cast");
  }

  llvm::Value *newLen = ctx.Builder->CreateSub(highVal, lowVal, "slice_len");
  llvm::Value *newCap = ctx.Builder->CreateSub(originalCap, lowVal, "slice_cap");

  llvm::Value *newDataPtr = ctx.Builder->CreateGEP(baseTy, originalDataPtr, lowVal, "slice_data_ptr");
  llvm::Type *sliceTy = llvmTypeFor(ctx, inst.result_type);
  ctx.Builder->CreateStore(
      originalRawPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
  ctx.Builder->CreateStore(
      newDataPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
  ctx.Builder->CreateStore(
      newLen, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)}));
  ctx.Builder->CreateStore(
      newCap, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)}));
}

void handle(CodegenContext &ctx, const mir::IndexReadInst &inst) {
  llvm::Value *leftVal = evaluateValue(ctx, inst.source);
  llvm::Value *indexVal = evaluateValue(ctx, inst.index);
  llvm::Type *elemTy = llvmTypeFor(ctx, inst.type);

  llvm::Value *leftPtr = leftVal;
  if (!leftPtr->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = ctx.Builder->CreateAlloca(leftVal->getType(), nullptr, "index_spill");
    ctx.Builder->CreateStore(leftVal, spill);
    leftPtr = spill;
  }

  llvm::Value *elemPtr = nullptr;
  llvm::Value *loadedVal = nullptr;

  if (std::holds_alternative<maml::ArrayType>(inst.source_type->inner)) {
    llvm::Type *arrayTy = llvmTypeFor(ctx, inst.source_type);
    elemPtr = ctx.Builder->CreateGEP(arrayTy, leftPtr, {ctx.Builder->getInt32(0), indexVal}, "array_elem_ptr");
    loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");

  } else if (std::holds_alternative<maml::StringType>(inst.source_type->inner)) {
    llvm::Type *strTy = llvmTypeFor(ctx, inst.source_type);
    llvm::Value *dataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(strTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
    elemPtr = ctx.Builder->CreateGEP(llvm::Type::getInt8Ty(ctx.Context), dataPtr, indexVal, "char_ptr");
    llvm::Value *charVal = ctx.Builder->CreateLoad(llvm::Type::getInt8Ty(ctx.Context), elemPtr, "char_load");
    loadedVal = ctx.Builder->CreateZExt(charVal, llvm::Type::getInt32Ty(ctx.Context), "char_ext");

  } else if (std::holds_alternative<maml::VectorType>(inst.source_type->inner)) {
    llvm::Value *vecHandle = leftPtr;
    llvm::FunctionCallee getFn = ctx.Module->getOrInsertFunction(
        "maml_vec_get", llvm::FunctionType::get(
                            llvm::PointerType::getUnqual(ctx.Context),
                            {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)}, false));
    elemPtr = ctx.Builder->CreateCall(getFn, {vecHandle, indexVal}, "vec_get_ptr");
    loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");

  } else if (std::holds_alternative<maml::ViewType>(inst.source_type->inner)) {
    llvm::Type *hdrTy = llvmTypeFor(ctx, inst.source_type);
    llvm::Value *dataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(hdrTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_elem_ptr");
    loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");
  }

  if (loadedVal) {
    ctx.SymbolEnv.back()[inst.dst] = loadedVal;
  }
}

void handle(CodegenContext &ctx, const mir::IndexAssignInst &inst) {
  llvm::Value *arrayPtr = ctx.getMemoryBase(inst.target);
  llvm::Value *indexVal = evaluateValue(ctx, inst.index);
  llvm::Value *storeVal = evaluateValue(ctx, inst.value);
  llvm::Value *elemPtr = nullptr;

  if (std::holds_alternative<maml::ArrayType>(inst.target_type->inner)) {
    llvm::Type *arrayTy = llvmTypeFor(ctx, inst.target_type);
    elemPtr = ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), indexVal}, "index_assign_ptr");

  } else if (std::holds_alternative<maml::VectorType>(inst.target_type->inner)) {
    llvm::Value *vecHandle = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), arrayPtr);
    llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
    llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
    llvm::AllocaInst *itemSpill = TmpBuilder.CreateAlloca(storeVal->getType(), nullptr, "vec_assign_spill");
    ctx.Builder->CreateStore(storeVal, itemSpill);
    llvm::FunctionCallee setFn = ctx.Module->getOrInsertFunction(
        "maml_vec_set",
        llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context)},
                                false));
    ctx.Builder->CreateCall(setFn, {vecHandle, indexVal, itemSpill});

  } else if (std::holds_alternative<maml::ViewType>(inst.target_type->inner)) {
    llvm::Type *hdrTy = llvmTypeFor(ctx, inst.target_type);
    llvm::Value *dataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(hdrTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    elemPtr = ctx.Builder->CreateGEP(storeVal->getType(), dataPtr, indexVal, "slice_assign_ptr");
  }

  if (elemPtr) {
    ctx.Builder->CreateStore(storeVal, elemPtr);
  }
}

}  // namespace maml
