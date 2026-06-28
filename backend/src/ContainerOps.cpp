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

void handle(CodegenContext &ctx, const mir::IndexAddrInst &inst) {
  llvm::Value *basePtr = nullptr;
  llvm::Type *baseTy = nullptr;

  // 1. Fetch the raw pointer from the symbol table (bypass auto-load)
  if (auto *reg = std::get_if<mir::Register>(&inst.source.inner)) {
    if (llvm::Value *sym = ctx.resolveSymbol(reg->name)) {
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(sym)) {
        basePtr = alloca;
        baseTy = alloca->getAllocatedType();
      } else {
        // It might be a pointer register generated by a previous FieldAddrInst/IndexAddrInst
        basePtr = sym;
        baseTy = llvmTypeFor(ctx, inst.source_type);
      }
    }
  }

  if (!basePtr) {
    ctx.Error.fatal("index_addr: Failed to locate base array pointer.");
    return;
  }

  // 2. Evaluate the index value
  llvm::Value *idxVal = evaluateValue(ctx, inst.index);

  // 3. Generate the GEP for array indexing
  llvm::Value *elementGep = nullptr;

  if (baseTy->isArrayTy()) {
    // For native fixed-size arrays: GEP needs {0, index} to step into the array
    llvm::Value *zero = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
    elementGep = ctx.Builder->CreateInBoundsGEP(baseTy, basePtr, {zero, idxVal}, inst.dst + "_gep");
  } else if (baseTy->isStructTy()) {
    // Views, Slices, and Strings are fat pointers (structs).
    // The raw data pointer to the heap/stack buffer is always at field index 0.

    // 1. Get the memory address of the 'ptr' field inside the slice struct
    llvm::Value *ptrFieldGep = ctx.Builder->CreateStructGEP(baseTy, basePtr, 0, inst.dst + "_ptr_field");

    // 2. Load the actual raw memory pointer out of the slice struct
    llvm::Type *rawPtrTy = llvm::PointerType::getUnqual(ctx.Context);
    llvm::Value *rawDataPtr = ctx.Builder->CreateLoad(rawPtrTy, ptrFieldGep, inst.dst + "_raw_ptr");

    // 3. GEP into the raw data buffer using the user's index!
    llvm::Type *elemTy = llvmTypeFor(ctx, inst.type);
    elementGep = ctx.Builder->CreateInBoundsGEP(elemTy, rawDataPtr, idxVal, inst.dst + "_gep");
  } else {
    // For raw pointers
    elementGep = ctx.Builder->CreateInBoundsGEP(llvmTypeFor(ctx, inst.type), basePtr, idxVal, inst.dst + "_gep");
  }

  // 4. Save the calculated pointer back into the environment!
  ctx.SymbolEnv.back()[inst.dst] = elementGep;
}

}  // namespace maml
