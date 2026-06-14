#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

void handle(CodegenContext &ctx, const mir::TempDeclInst &inst) {
  llvm::Type *ty = llvmTypeFor(ctx, inst.type);
  if (ty->isVoidTy()) return;
  llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
  llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
  llvm::AllocaInst *alloca = TmpBuilder.CreateAlloca(ty, nullptr, inst.name);
  TmpBuilder.CreateStore(llvm::Constant::getNullValue(ty), alloca);
  ctx.SymbolEnv.back()[inst.name] = alloca;
}

void handle(CodegenContext &ctx, const mir::AssignInst &inst) {
  llvm::Value *val = evaluateValue(ctx, inst.r_value);
  llvm::Value *target = ctx.resolveSymbol(inst.dst);
  if (!target) {
    ctx.Error.fatal("Assignment to unknown variable: " + inst.dst);
    return;
  }
  if (!val || val->getType()->isVoidTy()) return;
  ctx.Builder->CreateStore(val, target);
}

void handle(CodegenContext &ctx, const mir::CopyInst &inst) {
  llvm::Value *dstVal = ctx.resolveSymbol(inst.dst);
  llvm::Value *srcVal = ctx.resolveSymbol(inst.src);
  if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(srcVal)) {
    llvm::Value *loaded = ctx.Builder->CreateLoad(srcAlloca->getAllocatedType(), srcAlloca);
    ctx.Builder->CreateStore(loaded, dstVal);
  } else if (srcVal) {
    ctx.Builder->CreateStore(srcVal, dstVal);
  }
}

void handle(CodegenContext &ctx, const mir::MoveInst &inst) {
  llvm::Value *dstVal = ctx.resolveSymbol(inst.dst);
  llvm::Value *srcVal = ctx.resolveSymbol(inst.src);
  if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(srcVal)) {
    llvm::Value *loaded = ctx.Builder->CreateLoad(srcAlloca->getAllocatedType(), srcAlloca);
    ctx.Builder->CreateStore(loaded, dstVal);
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

void handle(CodegenContext &ctx, const mir::RefDecInst &inst) {
  // Look up the symbol safely
  llvm::Value *targetVar = ctx.resolveSymbol(inst.src);
  if (!targetVar) {
    // Optional: warn or fail if the symbol doesn't exist
    return;
  }

  llvm::Value *valToManage = targetVar;

  // 1. Resolve Alloca vs SSA:
  // If the symbol is a stack allocation, we must load the actual value from it first.
  // If it's already an SSA value (like from a load_ptr), we just use it directly.
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetVar)) {
    valToManage = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca, inst.src + "_load_for_release");
  }

  llvm::Type *ty = valToManage->getType();
  llvm::Value *rawPtr = nullptr;

  // 2. Extract the actual heap pointer based on the type
  if (ty->isStructTy()) {
    // For fat pointers (like your String struct { ptr, i32 }), the heap pointer is at index 0
    rawPtr = ctx.Builder->CreateExtractValue(valToManage, {0}, inst.src + "_raw_ptr");
  } else if (ty->isPointerTy()) {
    // For standard opaque pointers (like Map, Vec, or unboxed objects)
    rawPtr = valToManage;
  }

  // 3. Emit the Release call safely
  if (rawPtr) {
    llvm::Function *releaseFn = ctx.Module->getFunction("maml_release");
    if (!releaseFn) {
      ctx.Error.fatal("maml_release function not found in module. Ensure RuntimeConstants.h is linked.");
      return;
    }

    ctx.Builder->CreateCall(releaseFn, rawPtr);
  }
}

void handle(CodegenContext &ctx, const mir::RefIncInst &inst) {
  llvm::Value *targetVar = ctx.resolveSymbol(inst.src);
  if (!targetVar) return;

  llvm::Value *valToManage = targetVar;

  // 1. If the symbol is an Alloca, we must load the actual value from the stack first
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetVar)) {
    valToManage = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
  }

  llvm::Type *ty = valToManage->getType();
  llvm::Value *rawPtr = nullptr;

  // 2. Extract the actual heap pointer
  if (ty->isStructTy()) {
    // For fat pointers like String { ptr, len }, the heap pointer is at index 0
    rawPtr = ctx.Builder->CreateExtractValue(valToManage, {0});
  } else if (ty->isPointerTy()) {
    // For standard pointers like Map or Vec
    rawPtr = valToManage;
  }

  // 3. Emit the Retain call safely
  if (rawPtr) {
    llvm::Function *retainFn = ctx.Module->getFunction("maml_retain");
    ctx.Builder->CreateCall(retainFn, rawPtr);
  }
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

void handle(CodegenContext &ctx, const mir::MutBorrowInst &inst) { /* No-op in backend */ }
void handle(CodegenContext &ctx, const mir::CoroPrologueInst &inst) { /* Not implemented */ }
void handle(CodegenContext &ctx, const mir::KeepAliveInst &inst) { /* No-op in backend */ }

}  // namespace maml