#include "ExprGenerator.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

void handle(CodegenContext &ctx, const mir::ArrayInitInst &inst) {
  llvm::Value *arrayPtr = ctx.resolveSymbol(inst.dst);
  if (!arrayPtr) return;
  auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(arrayPtr);
  llvm::Type *arrayTy = alloca->getAllocatedType();
  llvm::Value *elemVal = evaluateValue(ctx, inst.value);

  llvm::Value *elemPtr = ctx.Builder->CreateGEP(
      arrayTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(inst.index)}, inst.dst + "_idx");
  ctx.Builder->CreateStore(elemVal, elemPtr);
}

void handle(CodegenContext &ctx, const mir::SliceInst &inst) {
  llvm::Value *dstPtr = ctx.resolveSymbol(inst.dst);
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

  std::string_view leftKind = getTypeKind(inst.container_type);

  if (leftKind == "array") {
    int size = inst.container_type["size"].get<int>();
    originalCap = ctx.Builder->getInt32(size);
    originalDataPtr =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "array_data");
    originalRawPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
    highVal = isEmpty(inst.high) ? originalCap : evaluateValue(ctx, inst.high);
  } else if (leftKind == "string") {
    llvm::Value *dataGep =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)});
    llvm::Value *lenGep = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
    originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
    originalDataPtr = originalRawPtr;
    originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
    highVal = isEmpty(inst.high) ? originalCap : evaluateValue(ctx, inst.high);
  } else if (leftKind == "vector") {
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
  } else if (leftKind == "view") {
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
  }

  llvm::Value *newLen = ctx.Builder->CreateSub(highVal, lowVal, "slice_len");
  llvm::Value *newCap = ctx.Builder->CreateSub(originalCap, lowVal, "slice_cap");

  // FIX: Safely route the base type extraction to prevent nlohmann::json type exceptions
  llvm::Type *baseTy = nullptr;
  if (leftKind == "string") {
    // Frontend exports strings as a raw JSON string ("string"), not an object.
    // Accessing ["base"] on it would throw an exception, so we hardcode the i8 base type.
    baseTy = llvm::Type::getInt8Ty(ctx.Context);
  } else {
    // For arrays, vectors, and views, the Go frontend exports the inner type under "base".
    baseTy = llvmTypeFor(ctx, inst.container_type["base"]);
  }

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

  if (leftKind != "array") {
    llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
        "maml_retain", llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                               {llvm::PointerType::getUnqual(ctx.Context)}, false));
    ctx.Builder->CreateCall(retainFn, {originalRawPtr});
  }
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

  // std::string_view kind = inst.source_type["kind"].get<std::string_view>();
  std::string_view kind = getTypeKind(inst.source_type);
  llvm::Value *elemPtr = nullptr;

  if (kind == "array") {
    llvm::Type *arrayTy = llvmTypeFor(ctx, inst.source_type);
    elemPtr = ctx.Builder->CreateGEP(arrayTy, leftPtr, {ctx.Builder->getInt32(0), indexVal}, "array_elem_ptr");
  } else if (kind == "string") {
    llvm::Type *strTy = llvmTypeFor(ctx, inst.source_type);
    llvm::Value *dataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(strTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
    elemPtr = ctx.Builder->CreateGEP(llvm::Type::getInt8Ty(ctx.Context), dataPtr, indexVal, "char_ptr");
  } else if (kind == "vector") {
    llvm::Value *vecHandle = leftPtr;
    llvm::FunctionCallee getFn = ctx.Module->getOrInsertFunction(
        "maml_vec_get", llvm::FunctionType::get(
                            llvm::PointerType::getUnqual(ctx.Context),
                            {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)}, false));
    elemPtr = ctx.Builder->CreateCall(getFn, {vecHandle, indexVal}, "vec_get_ptr");
  } else if (kind == "view") {
    llvm::Type *hdrTy = llvmTypeFor(ctx, inst.source_type);
    llvm::Value *dataPtr = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context),
        ctx.Builder->CreateGEP(hdrTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_elem_ptr");
  }

  llvm::Value *loadedVal = nullptr;
  if (kind == "string") {
    llvm::Value *charVal = ctx.Builder->CreateLoad(llvm::Type::getInt8Ty(ctx.Context), elemPtr, "char_load");
    loadedVal = ctx.Builder->CreateZExt(charVal, llvm::Type::getInt32Ty(ctx.Context), "char_ext");
  } else {
    loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");
  }
  ctx.SymbolEnv.back()[inst.dst] = loadedVal;
}

void handle(CodegenContext &ctx, const mir::IndexAssignInst &inst) {
  llvm::Value *arrayPtr = ctx.resolveSymbol(inst.target);
  llvm::Value *indexVal = evaluateValue(ctx, inst.index);
  llvm::Value *storeVal = evaluateValue(ctx, inst.value);
  llvm::Value *elemPtr = nullptr;

  std::string_view kind = getTypeKind(inst.target_type);

  if (kind == "array") {
    llvm::Type *arrayTy = llvmTypeFor(ctx, inst.target_type);
    elemPtr = ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), indexVal}, "index_assign_ptr");
  } else if (kind == "vector") {
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
  } else if (kind == "view") {
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