#include <llvm/IR/DataLayout.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

// -----------------------------------------------------------------------------
// Private, Isolated Instruction Helpers
// -----------------------------------------------------------------------------

static void handleTempDecl(CodegenContext &ctx, const nlohmann::json &stmt) {
  llvm::Type *ty = llvmTypeFor(ctx, stmt["type"]);
  std::string name = stmt["name"].get<std::string>();
  if (ty->isVoidTy()) return;

  llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock &entryBlock = parentFn->getEntryBlock();
  llvm::IRBuilder<> TmpBuilder(&entryBlock, entryBlock.begin());

  llvm::AllocaInst *alloca = TmpBuilder.CreateAlloca(ty, nullptr, name);
  TmpBuilder.CreateStore(llvm::Constant::getNullValue(ty), alloca);
  ctx.SymbolEnv.back()[name] = alloca;
}

static void handleAssign(CodegenContext &ctx, const nlohmann::json &stmt) {
  llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
  std::string dst = stmt["dst"].get<std::string>();
  llvm::Value *target = ctx.resolveSymbol(dst);

  if (!target) {
    if (val && !val->getType()->isVoidTy()) {
      ctx.Error.fatal("Assignment to unknown variable: " + dst, stmt);
    }
    return;
  }
  if (!val || val->getType()->isVoidTy()) return;

  ctx.Builder->CreateStore(val, target);
}

static void handleCopyOrMove(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  std::string src = stmt["src"].get<std::string>();
  llvm::Value *dstVal = ctx.resolveSymbol(dst);
  llvm::Value *srcVal = ctx.resolveSymbol(src);

  if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(srcVal)) {
    llvm::Value *loaded = ctx.Builder->CreateLoad(srcAlloca->getAllocatedType(), srcAlloca);
    ctx.Builder->CreateStore(loaded, dstVal);
  }
}

static void handleLoadPtr(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  llvm::Value *ptrVal = evaluateExpression(ctx, stmt["ptr"]);
  llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);

  llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, ptrVal, dst + "_load");
  ctx.SymbolEnv.back()[dst] = loadedVal;
}

static void handleStore(CodegenContext &ctx, const nlohmann::json &stmt) {
  llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
  std::string dst_ptr_name = stmt["dst_ptr"].get<std::string>();
  llvm::Value *dstPtr = ctx.resolveSymbol(dst_ptr_name);
  if (!dstPtr) {
    ctx.Error.fatal("store: destination pointer not found: " + dst_ptr_name, stmt);
    return;
  }
  ctx.Builder->CreateStore(val, dstPtr);
}

static void handleRefAlloc(CodegenContext &ctx, const nlohmann::json &stmt) {
  llvm::Type *ty = llvmTypeFor(ctx, stmt["type"]);
  std::string dst = stmt["dst"].get<std::string>();

  llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
  llvm::DataLayout DL(ctx.Module.get());
  uint64_t size = DL.getTypeAllocSize(ty);
  llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), size);
  llvm::Value *heapPtr = ctx.Builder->CreateCall(allocFn, {sizeVal}, dst + "_heap");
  llvm::Value *dstTarget = ctx.resolveSymbol(dst);
  ctx.Builder->CreateStore(heapPtr, dstTarget);
}

static void handleRefDec(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string src = stmt["src"].get<std::string>();
  if (llvm::Value *srcTarget = ctx.resolveSymbol(src)) {
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(srcTarget)) {
      llvm::Value *loaded = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
      llvm::Value *ptrToRelease = loaded;

      // NEW: If the type is a fat pointer (struct), extract the raw pointer (field 0)
      if (loaded->getType()->isStructTy()) {
        ptrToRelease = ctx.Builder->CreateExtractValue(loaded, 0);
      }

      llvm::Function *releaseFn = ctx.Module->getFunction(rt::RELEASE);
      if (releaseFn) ctx.Builder->CreateCall(releaseFn, {ptrToRelease});
    }
  }
}

static void handleRefInc(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string src = stmt["src"].get<std::string>();
  if (llvm::Value *srcTarget = ctx.resolveSymbol(src)) {
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(srcTarget)) {
      llvm::Value *loaded = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
      llvm::Value *ptrToRetain = loaded;

      // NEW: Extract raw pointer for retains as well
      if (loaded->getType()->isStructTy()) {
        ptrToRetain = ctx.Builder->CreateExtractValue(loaded, 0);
      }

      llvm::Function *retainFn = ctx.Module->getFunction(rt::RETAIN);
      if (retainFn) ctx.Builder->CreateCall(retainFn, {ptrToRetain});
    }
  }
}

static void handleCast(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  llvm::Value *srcVal = evaluateExpression(ctx, stmt["src"]);
  llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);
  llvm::Value *castVal = nullptr;

  if (srcVal->getType()->isIntegerTy() && targetTy->isIntegerTy()) {
    castVal = ctx.Builder->CreateZExtOrTrunc(srcVal, targetTy, dst + "_cast");
  } else if (srcVal->getType()->isPointerTy() && targetTy->isPointerTy()) {
    castVal = ctx.Builder->CreatePointerCast(srcVal, targetTy, dst + "_cast");
  } else if (srcVal->getType()->isIntegerTy() && targetTy->isPointerTy()) {
    castVal = ctx.Builder->CreateIntToPtr(srcVal, targetTy, dst + "_cast");
  } else if (srcVal->getType()->isPointerTy() && targetTy->isIntegerTy()) {
    castVal = ctx.Builder->CreatePtrToInt(srcVal, targetTy, dst + "_cast");
  } else {
    ctx.Error.fatal("cast: unsupported cast operation", stmt);
    return;
  }
  ctx.SymbolEnv.back()[dst] = castVal;
}

// -----------------------------------------------------------------------------
// Public Cluster Entry Point
// -----------------------------------------------------------------------------

void compileMemoryOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt) {
  switch (op) {
    case MirOp::TempDecl:
      return handleTempDecl(ctx, stmt);
    case MirOp::Assign:
      return handleAssign(ctx, stmt);
    case MirOp::Copy:
    case MirOp::Move:
      return handleCopyOrMove(ctx, stmt);
    case MirOp::LoadPtr:
      return handleLoadPtr(ctx, stmt);
    case MirOp::Store:
      return handleStore(ctx, stmt);
    case MirOp::RefAlloc:
      return handleRefAlloc(ctx, stmt);
    case MirOp::RefInc:
      return handleRefInc(ctx, stmt);
    case MirOp::RefDec:
      return handleRefDec(ctx, stmt);
    case MirOp::Cast:
      return handleCast(ctx, stmt);
    default:
      break;
  }
}

}  // namespace maml