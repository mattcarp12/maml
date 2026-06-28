#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.hpp"
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
  llvm::Value *rawSym = ctx.resolveSymbol(inst.src);
  bool srcIsMemory = rawSym->getType()->isPointerTy();

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
  llvm::Value *rawSym = ctx.resolveSymbol(inst.src);
  bool srcIsMemory = rawSym->getType()->isPointerTy();
  if (srcIsMemory) {
    llvm::Type *ty = ctx.SymbolTypes[inst.src];
    llvm::Value *loaded = ctx.Builder->CreateLoad(ty, srcVal, inst.src + "_move_load");
    ctx.Builder->CreateStore(loaded, dstPtr);
  } else {
    ctx.Builder->CreateStore(srcVal, dstPtr);
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

void handle(CodegenContext &ctx, const mir::DropInst &inst) {
  llvm::Value *target = ctx.getMemoryBase(inst.src);
  if (!target) return;

  llvm::Type *ty = ctx.SymbolTypes[inst.src];
  llvm::Value *ptrToFree = nullptr;
  llvm::Function *freeFn = ctx.Module->getFunction("maml_free");
  if (!freeFn) return;

  if (ty->isStructTy()) {
    // Check if this is a String layout: { ptr, len, is_owned (i1) }
    if (ty->getStructNumElements() == 3 && ty->getStructElementType(2)->isIntegerTy(1)) {
      // 1. Get the is_owned flag at index 2
      llvm::Value *ownedGep = ctx.Builder->CreateStructGEP(ty, target, 2, inst.src + "_owned_gep");
      llvm::Value *isOwned =
          ctx.Builder->CreateLoad(llvm::Type::getInt1Ty(ctx.Context), ownedGep, inst.src + "_is_owned");

      // 2. Create basic blocks for the conditional free
      llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
      llvm::BasicBlock *freeBB = llvm::BasicBlock::Create(ctx.Context, inst.src + "_do_free", F);
      llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(ctx.Context, inst.src + "_skip_free", F);

      // 3. Branch based on the flag
      ctx.Builder->CreateCondBr(isOwned, freeBB, mergeBB);

      // 4. Populate the Free block
      ctx.Builder->SetInsertPoint(freeBB);
      llvm::Value *ptrGep = ctx.Builder->CreateStructGEP(ty, target, 0, inst.src + "_ptr_gep");
      ptrToFree = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), ptrGep);
      ctx.Builder->CreateCall(freeFn, {ptrToFree});
      ctx.Builder->CreateBr(mergeBB);

      // 5. Resume compilation in the merge block
      ctx.Builder->SetInsertPoint(mergeBB);
      return;  // Early return because we already handled the call!
    } else {
      // Standard fat pointer (like Vec {ptr, len, cap}) without an is_owned flag
      llvm::Value *gep = ctx.Builder->CreateStructGEP(ty, target, 0, inst.src + "_drop_gep");
      ptrToFree = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), gep, inst.src + "_drop_load");
    }
  } else {
    // Opaque pointers (like from maml_vec_create)
    ptrToFree = ctx.Builder->CreateLoad(ty, target, inst.src + "_drop_load");
  }

  // Fallback for standard fat pointers and opaque pointers
  if (ptrToFree) {
    ctx.Builder->CreateCall(freeFn, {ptrToFree});
  }
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

  // Initial suspend
  llvm::Function *coroSaveFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
  llvm::Value *initSaveToken = Builder->CreateCall(coroSaveFn, {ctx.CurrentCoroHandle}, "init.save");
  llvm::Function *coroSuspendFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
  llvm::Value *suspendResult =
      Builder->CreateCall(coroSuspendFn, {initSaveToken, Builder->getInt1(false)}, "init.suspend");

  // Route: resume → bb0 (the actual MIR entry block), destroy → cleanup
  // NOTE: no userCodeBB needed — bb0 IS the user code block
  llvm::BasicBlock *mirEntryBB = ctx.Blocks[ctx.CoroEntryBlockId];
  llvm::SwitchInst *sw = Builder->CreateSwitch(suspendResult, ctx.CoroSuspendBlock, 2);
  sw->addCase(Builder->getInt8(0), mirEntryBB);
  sw->addCase(Builder->getInt8(1), ctx.CoroCleanupBlock);

  // allocBB is now terminated. Point builder at bb0 for the
  // second-pass instruction emission that's about to happen.
  // Builder->SetInsertPoint(mirEntryBB);
}

}  // namespace maml