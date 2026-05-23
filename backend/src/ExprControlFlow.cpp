#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileIfExpr(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *thenBB = llvm::BasicBlock::Create(ctx.Context, "then", F);
  llvm::BasicBlock *elseBB = llvm::BasicBlock::Create(ctx.Context, "else", F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(ctx.Context, "merge", F);

  llvm::Value *cond = evaluateExpression(ctx, e["condition"]);
  Builder->CreateCondBr(cond, thenBB, elseBB);

  llvm::Type *expectedTy = nullptr;
  if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" && e["maml_type"]["kind"] != "Unknown") {
    expectedTy = llvmTypeFor(ctx, e["maml_type"]);
  }

  Builder->SetInsertPoint(thenBB);
  llvm::Value *thenVal = evaluateExpression(ctx, e["consequence"]);

  if (expectedTy && thenVal && thenVal->getType()->isPointerTy() && !expectedTy->isPointerTy()) {
    thenVal = Builder->CreateLoad(expectedTy, thenVal, "then_load");
  }
  if (!Builder->GetInsertBlock()->getTerminator()) Builder->CreateBr(mergeBB);
  thenBB = Builder->GetInsertBlock();

  Builder->SetInsertPoint(elseBB);
  llvm::Value *elseVal = nullptr;
  if (e.contains("alternative")) {
    elseVal = evaluateExpression(ctx, e["alternative"]);
  }

  if (expectedTy && elseVal && elseVal->getType()->isPointerTy() && !expectedTy->isPointerTy()) {
    elseVal = Builder->CreateLoad(expectedTy, elseVal, "else_load");
  }
  if (!elseVal && expectedTy) elseVal = llvm::Constant::getNullValue(expectedTy);
  if (!Builder->GetInsertBlock()->getTerminator()) Builder->CreateBr(mergeBB);
  elseBB = Builder->GetInsertBlock();

  Builder->SetInsertPoint(mergeBB);
  if (expectedTy && thenVal && elseVal) {
    llvm::PHINode *phi = Builder->CreatePHI(expectedTy, 2, "iftmp");
    phi->addIncoming(thenVal, thenBB);
    phi->addIncoming(elseVal, elseBB);
    return phi;
  }
  return nullptr;
}

llvm::Value *compileBlockExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  ctx.pushScope();
  llvm::Value *blockResult = nullptr;
  for (const auto &s : expr["statements"]) {
    if (s.contains("node_type") && s["node_type"].get<std::string_view>() == "YieldStmt") {
      blockResult = evaluateExpression(ctx, s["value"]);
      break;
    } else {
      compileStatement(ctx, s);
    }
    if (Builder->GetInsertBlock()->getTerminator()) break;
  }
  ctx.popScope();
  return blockResult;
}

llvm::Value *compileAwaitExpression(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Value *taskVal = evaluateExpression(ctx, e["value"]);
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  llvm::BasicBlock *resumeBB = llvm::BasicBlock::Create(ctx.Context, "await.resume", parentFn);
  llvm::BasicBlock *cleanupBB = llvm::BasicBlock::Create(ctx.Context, "await.cleanup", parentFn);
  llvm::BasicBlock *suspendBB = llvm::BasicBlock::Create(ctx.Context, "await.suspend", parentFn);

  llvm::Function *coroSuspendFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_suspend);
  llvm::Value *noneToken = llvm::ConstantTokenNone::get(ctx.Context);
  llvm::Value *isFinalValue = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);

  llvm::Value *suspendResult = Builder->CreateCall(coroSuspendFn, {noneToken, isFinalValue}, "suspend_res");
  llvm::SwitchInst *sw = Builder->CreateSwitch(suspendResult, suspendBB, 2);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 0), resumeBB);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 1), cleanupBB);

  Builder->SetInsertPoint(cleanupBB);
  llvm::Function *coroFreeFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_free);
  Builder->CreateCall(coroFreeFn, {ctx.resolveSymbol(rt::CORO_ID), ctx.resolveSymbol(rt::CORO_HDL)});
  Builder->CreateBr(suspendBB);

  Builder->SetInsertPoint(suspendBB);
  Builder->CreateRet(ctx.resolveSymbol(rt::CORO_HDL));

  Builder->SetInsertPoint(resumeBB);
  return taskVal;
}

llvm::Value *compileAsyncPrologueExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  auto &Context = ctx.Context;
  auto &Module = ctx.Module;

  llvm::Value *align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
  llvm::Value *nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));

  // Create a basic 8-bit Promise object on the stack to hold the coroutine return state
  llvm::Type *promiseTy = llvm::Type::getInt8Ty(Context);
  llvm::Value *promiseAlloc = Builder->CreateAlloca(promiseTy, nullptr, "promise");
  llvm::Value *promisePtr = Builder->CreateBitCast(promiseAlloc, llvm::PointerType::getUnqual(Context));

  llvm::Function *coroIdFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_id);
  llvm::Value *coroId = Builder->CreateCall(coroIdFn, {align, promisePtr, nullPtr, nullPtr}, "coro.id");
  ctx.SymbolTable[rt::CORO_ID] = coroId;

  llvm::Function *coroSizeFn =
      llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_size, {llvm::Type::getInt64Ty(Context)});
  llvm::Value *coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");

  llvm::FunctionCallee mamlAlloc = Module->getOrInsertFunction(
      rt::ALLOC,
      llvm::FunctionType::get(llvm::PointerType::getUnqual(Context), {llvm::Type::getInt64Ty(Context)}, false));
  llvm::Value *allocMem = Builder->CreateCall(mamlAlloc, {coroSize}, "coro.alloc.mem");

  llvm::Function *coroBeginFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_begin);
  llvm::Value *coroHdl = Builder->CreateCall(coroBeginFn, {coroId, allocMem}, "coro.hdl");
  ctx.SymbolTable[rt::CORO_HDL] = coroHdl;

  return coroHdl;  // This evaluates to the Task<T> handle!
}

}  // namespace maml