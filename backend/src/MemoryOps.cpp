#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.hpp"
#include "RuntimeConstants.h"
#include "TypeLowering.hpp"

namespace maml {

void handle(CodegenContext &ctx, const mir::TempDeclInst &inst) {
  llvm::Type *ty = llvmTypeFor(ctx, inst.type);
  if (ty->isVoidTy()) return;

  // 1. Track the structural type for all downstream GEPs and Loads
  ctx.SymbolTypes[inst.name] = ty;

  llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
  llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());

  llvm::AllocaInst *alloca;
  if (ctx.HeapVars.count(inst.name)) {
    // 2. Variable escapes: allocate only a pointer to hold the heap address
    llvm::Type *ptrTy = llvm::PointerType::getUnqual(ctx.Context);
    alloca = TmpBuilder.CreateAlloca(ptrTy, nullptr, inst.name + "_ptr");
    TmpBuilder.CreateStore(llvm::Constant::getNullValue(ptrTy), alloca);
  } else {
    // 3. Stack bound: allocate the full structure
    alloca = TmpBuilder.CreateAlloca(ty, nullptr, inst.name);
    TmpBuilder.CreateStore(llvm::Constant::getNullValue(ty), alloca);
  }
  ctx.SymbolEnv.back()[inst.name] = alloca;
}

void handle(CodegenContext &ctx, const mir::AssignInst &inst) {
  llvm::Value *val = evaluateValue(ctx, inst.r_value);
  llvm::Value *target = ctx.getMemoryBase(inst.dst);  // Use Router
  if (!target || !val || val->getType()->isVoidTy()) return;
  ctx.Builder->CreateStore(val, target);
}

void handle(CodegenContext &ctx, const mir::CopyInst &inst) {
  llvm::Value *dstPtr = ctx.getMemoryBase(inst.dst);
  llvm::Value *srcVal = ctx.getMemoryBase(inst.src);
  if (!dstPtr || !srcVal) return;

  // Check if the symbol is a true memory allocation, or an overwritten SSA value
  bool srcIsMemory = llvm::isa<llvm::AllocaInst>(ctx.resolveSymbol(inst.src));

  if (srcIsMemory) {
    llvm::Type *ty = ctx.SymbolTypes[inst.src];
    llvm::Value *loaded = ctx.Builder->CreateLoad(ty, srcVal, inst.src + "_copy_load");
    ctx.Builder->CreateStore(loaded, dstPtr);
  } else {
    // It's already an SSA value, store it directly into the destination memory
    ctx.Builder->CreateStore(srcVal, dstPtr);
  }
}

void handle(CodegenContext &ctx, const mir::MoveInst &inst) {
  llvm::Value *dstPtr = ctx.getMemoryBase(inst.dst);
  llvm::Value *srcVal = ctx.getMemoryBase(inst.src);
  if (!dstPtr || !srcVal) return;

  bool srcIsMemory = llvm::isa<llvm::AllocaInst>(ctx.resolveSymbol(inst.src));

  if (srcIsMemory) {
    llvm::Type *ty = ctx.SymbolTypes[inst.src];
    llvm::Value *loaded = ctx.Builder->CreateLoad(ty, srcVal, inst.src + "_move_load");
    ctx.Builder->CreateStore(loaded, dstPtr);

    // Null out source memory to prevent Use-After-Free during frontend cleanup
    llvm::Value *nullVal = llvm::Constant::getNullValue(ty);
    ctx.Builder->CreateStore(nullVal, srcVal);
  } else {
    // SSA values don't hold backing allocations locally, so just store the value
    ctx.Builder->CreateStore(srcVal, dstPtr);
  }
}

void handle(CodegenContext &ctx, const mir::RefDecInst &inst) {
  llvm::Value *basePtr = ctx.getMemoryBase(inst.src);
  if (!basePtr) return;

  bool srcIsMemory = llvm::isa<llvm::AllocaInst>(ctx.resolveSymbol(inst.src));
  llvm::Type *ty = ctx.SymbolTypes[inst.src];

  llvm::Value *valToManage = basePtr;
  if (srcIsMemory) {
    valToManage = ctx.Builder->CreateLoad(ty, basePtr, inst.src + "_release_load");
  }

  llvm::Value *rawPtr = nullptr;
  if (ty->isStructTy()) {
    rawPtr = ctx.Builder->CreateExtractValue(valToManage, {0}, inst.src + "_raw_ptr");
  } else if (ty->isPointerTy()) {
    rawPtr = valToManage;
  }

  if (rawPtr) {
    llvm::Function *releaseFn = ctx.Module->getFunction("maml_release");
    ctx.Builder->CreateCall(releaseFn, rawPtr);
  }
}

void handle(CodegenContext &ctx, const mir::RefIncInst &inst) {
  llvm::Value *basePtr = ctx.getMemoryBase(inst.src);
  if (!basePtr) return;

  bool srcIsMemory = llvm::isa<llvm::AllocaInst>(ctx.resolveSymbol(inst.src));
  llvm::Type *ty = ctx.SymbolTypes[inst.src];

  llvm::Value *valToManage = basePtr;
  if (srcIsMemory) {
    valToManage = ctx.Builder->CreateLoad(ty, basePtr, inst.src + "_retain_load");
  }

  llvm::Value *rawPtr = nullptr;
  if (ty->isStructTy()) {
    rawPtr = ctx.Builder->CreateExtractValue(valToManage, {0});
  } else if (ty->isPointerTy()) {
    rawPtr = valToManage;
  }

  if (rawPtr) {
    llvm::Function *retainFn = ctx.Module->getFunction("maml_retain");
    ctx.Builder->CreateCall(retainFn, rawPtr);
  }
}

void handle(CodegenContext &ctx, const mir::LoadPtrInst &inst) {
  llvm::Value *ptrVal = evaluateValue(ctx, inst.ptr);
  llvm::Type *targetTy = llvmTypeFor(ctx, inst.type);
  llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, ptrVal, inst.dst + "_load");
  ctx.SymbolEnv.back()[inst.dst] = loadedVal;
}

void handle(CodegenContext &ctx, const mir::StoreInst &inst) {
  llvm::Value *val = evaluateValue(ctx, inst.value);
  llvm::Value *dstPtr = ctx.resolveSymbol(inst.dst_ptr);
  if (!dstPtr) {
    ctx.Error.fatal("store: destination pointer not found: " + inst.dst_ptr);
    return;
  }
  ctx.Builder->CreateStore(val, dstPtr);
}

void handle(CodegenContext &ctx, const mir::RefAllocInst &inst) {
  llvm::Type *ty = llvmTypeFor(ctx, inst.type);
  llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
  llvm::DataLayout DL(ctx.Module.get());
  llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), DL.getTypeAllocSize(ty));
  llvm::Value *heapPtr = ctx.Builder->CreateCall(allocFn, {sizeVal}, inst.dst + "_heap");
  ctx.Builder->CreateStore(heapPtr, ctx.resolveSymbol(inst.dst));
}

void handle(CodegenContext &ctx, const mir::CastInst &inst) {
  llvm::Value *srcVal = evaluateValue(ctx, inst.src);
  llvm::Type *targetTy = llvmTypeFor(ctx, inst.type);
  llvm::Value *castVal = nullptr;

  if (srcVal->getType()->isIntegerTy() && targetTy->isIntegerTy()) {
    castVal = ctx.Builder->CreateZExtOrTrunc(srcVal, targetTy, inst.dst + "_cast");
  } else if (srcVal->getType()->isPointerTy() && targetTy->isPointerTy()) {
    castVal = ctx.Builder->CreatePointerCast(srcVal, targetTy, inst.dst + "_cast");
  } else if (srcVal->getType()->isIntegerTy() && targetTy->isPointerTy()) {
    castVal = ctx.Builder->CreateIntToPtr(srcVal, targetTy, inst.dst + "_cast");
  } else if (srcVal->getType()->isPointerTy() && targetTy->isIntegerTy()) {
    castVal = ctx.Builder->CreatePtrToInt(srcVal, targetTy, inst.dst + "_cast");
  } else {
    ctx.Error.fatal("cast: unsupported cast operation");
    return;
  }
  ctx.SymbolEnv.back()[inst.dst] = castVal;
}

void handle(CodegenContext &ctx, const mir::CoroPrologueInst &inst) {
  auto &Builder = ctx.Builder;
  auto &Context = ctx.Context;
  auto *Module = ctx.Module.get();

  // 1. Allocate a generic 32-byte Promise slot to hold the return value
  llvm::Type *promiseTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(Context), 4);
  ctx.PromiseSlot = Builder->CreateAlloca(promiseTy, nullptr, "promise");

  // 2. Pass the Promise into coro.id so LLVM tracks its offset in the frame
  llvm::Function *coroIdFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_id);
  llvm::Value *nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));
  llvm::Value *promisePtr = Builder->CreatePointerCast(ctx.PromiseSlot, llvm::PointerType::getUnqual(Context));

  ctx.CoroId = Builder->CreateCall(
      coroIdFn, {llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0), promisePtr, nullPtr, nullPtr}, "coro.id");

  // 3. Size and Alloc
  llvm::Function *coroSizeFn =
      llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_size, {llvm::Type::getInt64Ty(Context)});
  llvm::Value *coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");
  llvm::Function *allocFn = Module->getFunction("maml_alloc");
  llvm::Value *framePtr = Builder->CreateCall(allocFn, {coroSize}, "coro.frame.alloc");

  // 4. Begin
  llvm::Function *coroBeginFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_begin);
  ctx.CurrentCoroHandle = Builder->CreateCall(coroBeginFn, {ctx.CoroId, framePtr}, "coro.handle");
}

}  // namespace maml