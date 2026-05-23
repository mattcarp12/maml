#include "StmtGenerator.h"
#include "ExprGenerator.h"
#include "MemoryManager.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"
#include <iostream>

namespace maml {

static void compileBlockStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  ctx.pushScope();
  for (const auto &s : stmt["statements"]) {
    compileStatement(ctx, s);
    if (ctx.Builder->GetInsertBlock()->getTerminator())
      break;
  }
  llvm::FunctionCallee releaseFn = ctx.Module->getOrInsertFunction(
      "maml_release", llvm::FunctionType::get(
                          llvm::Type::getVoidTy(ctx.Context),
                          {llvm::PointerType::getUnqual(ctx.Context)}, false));
  if (!ctx.ScopeStack.empty())
    ctx.popScope(releaseFn);
}

static void compileDeclareStmt(CodegenContext &ctx,
                               const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  std::string_view varName = stmt["name"].get<std::string_view>();
  llvm::Value *initVal = evaluateExpression(ctx, stmt["value"]);
  nlohmann::json typeJson = stmt["value"]["maml_type"];
  llvm::Type *valTy = llvmTypeFor(ctx, typeJson);

  bool isHeap = stmt["value"].value("is_heap", false);
  if (isHeap && (valTy->isArrayTy() || valTy->isStructTy())) {
    llvm::AllocaInst *ptrAlloca = Builder->CreateAlloca(
        initVal->getType(), nullptr, std::string(varName));
    Builder->CreateStore(initVal, ptrAlloca);
    ctx.SymbolEnv.back()[std::string(varName)] = ptrAlloca;
    return;
  }

  llvm::AllocaInst *alloca =
      Builder->CreateAlloca(valTy, nullptr, std::string(varName));

  // Zero-initialize any alloca whose type participates in ARC. This ensures
  // that compileAssignStmt's emitRelease-before-write always sees a null
  // heap pointer on the first assignment, rather than stack garbage.
  // maml_release is required to be a no-op on null (standard ARC contract).
  if (MemoryManager::needsARC(typeJson)) {
    llvm::DataLayout DL(ctx.Module.get());
    uint64_t byteSize = DL.getTypeAllocSize(valTy);
    Builder->CreateMemSet(
        alloca, llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 0),
        llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), byteSize),
        alloca->getAlign());
  }

  if (initVal->getType()->isPointerTy() &&
      (valTy->isStructTy() || valTy->isArrayTy())) {
    llvm::Value *loadedData = Builder->CreateLoad(valTy, initVal, "array_load");
    Builder->CreateStore(loadedData, alloca);
  } else {
    Builder->CreateStore(initVal, alloca);
  }

  ctx.SymbolEnv.back()[std::string(varName)] = alloca;
  MemoryManager::trackDeepForRelease(ctx, alloca, typeJson);
}

static llvm::Value *computeLValue(CodegenContext &ctx,
                                  const nlohmann::json &lvalueNode,
                                  bool &isPointerReassignment) {
  auto &Builder = ctx.Builder;
  std::string_view lnodeType = "unknown";
  if (lvalueNode.contains("node_type")) {
    lnodeType = lvalueNode["node_type"].get<std::string_view>();
  }

  if (lnodeType == "Identifier") {
    std::string_view targetName = lvalueNode["value"].get<std::string_view>();
    llvm::Value *targetPtr = ctx.resolveSymbol(targetName);
    if (targetPtr) {
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetPtr)) {
        if (alloca->getAllocatedType()->isPointerTy()) {
          isPointerReassignment = true;
        }
      }
    }
    return targetPtr;
  }

  if (lnodeType == "FieldAccess") {
    return compileFieldAccess(ctx, lvalueNode);
  }

  if (lnodeType == "IndexExpr") {
    llvm::Value *leftVal = evaluateExpression(ctx, lvalueNode["left"]);
    llvm::Value *indexVal = evaluateExpression(ctx, lvalueNode["index"]);
    llvm::Type *leftTy = llvmTypeFor(ctx, lvalueNode["left"]["maml_type"]);

    llvm::Value *leftPtr = leftVal;
    if (!leftVal->getType()->isPointerTy()) {
      llvm::AllocaInst *spill =
          Builder->CreateAlloca(leftTy, nullptr, "lvalue_spill");
      Builder->CreateStore(leftVal, spill);
      leftPtr = spill;
    }

    if (leftTy->isArrayTy()) {
      return Builder->CreateGEP(leftTy, leftPtr,
                                {Builder->getInt32(0), indexVal},
                                "array_idx_assign");
    } else {
      llvm::Value *dataPtr = Builder->CreateGEP(
          leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)},
          "data_ptr_assign");
      llvm::Value *data = Builder->CreateLoad(
          llvm::PointerType::getUnqual(ctx.Context), dataPtr);
      return Builder->CreateGEP(llvmTypeFor(ctx, lvalueNode["maml_type"]), data,
                                indexVal, "slice_idx_assign");
    }
  }

  std::cerr << "Error: Invalid or undefined LValue in assignment\n";
  return nullptr;
}

static void compileAssignStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  const nlohmann::json &lvalueNode =
      stmt.contains("lvalue") ? stmt["lvalue"] : stmt["target"];

  bool isPointerReassignment = false;
  llvm::Value *targetPtr =
      computeLValue(ctx, lvalueNode, isPointerReassignment);
  if (!targetPtr)
    return;

  const nlohmann::json &rhsNode =
      stmt.contains("rvalue") ? stmt["rvalue"] : stmt["value"];
  llvm::Value *rhsVal = evaluateExpression(ctx, rhsNode);
  nlohmann::json typeJson = rhsNode["maml_type"];

  MemoryManager::emitRelease(ctx, targetPtr, typeJson);

  llvm::Type *valTy = llvmTypeFor(ctx, typeJson);
  if (isPointerReassignment) {
    Builder->CreateStore(rhsVal, targetPtr);
  } else if (rhsVal->getType()->isPointerTy() &&
             (valTy->isStructTy() || valTy->isArrayTy())) {
    llvm::Value *loadedData = Builder->CreateLoad(valTy, rhsVal, "array_load");
    Builder->CreateStore(loadedData, targetPtr);
  } else {
    Builder->CreateStore(rhsVal, targetPtr);
  }

  MemoryManager::emitRetain(ctx, targetPtr, typeJson);
}

static void compileReturnStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;

  // Coroutine early-out: suspend points handle ARC, just return the handle.
  // Still need to drain the scope metadata so compileFunction's popScope
  // doesn't double-free on the (already-terminated) exit block.
  if (llvm::Value *coroHdl = ctx.resolveSymbol(rt::CORO_HDL)) {
    Builder->CreateRet(coroHdl);
    ctx.ScopeStack.clear();
    ctx.SymbolEnv.clear();
    return;
  }

  llvm::Value *retVal = evaluateExpression(ctx, stmt["value"]);

  if (retVal) {
    llvm::Function *F = Builder->GetInsertBlock()->getParent();
    llvm::Type *expectedRetTy = F->getReturnType();
    if (retVal->getType()->isPointerTy() && !expectedRetTy->isPointerTy()) {
      retVal = Builder->CreateLoad(expectedRetTy, retVal, "ret_load");
    }
  }

  // Emit ARC releases for every live scope frame, inner-to-outer.
  // This mirrors what popScope does per frame, but across all frames at once
  // since a return unwinds the entire function stack.
  llvm::FunctionCallee releaseFn = ctx.Module->getOrInsertFunction(
      rt::RELEASE, llvm::FunctionType::get(
                       llvm::Type::getVoidTy(ctx.Context),
                       {llvm::PointerType::getUnqual(ctx.Context)}, false));

  for (auto it = ctx.ScopeStack.rbegin(); it != ctx.ScopeStack.rend(); ++it) {
    for (auto itemIt = it->rbegin(); itemIt != it->rend(); ++itemIt) {
      if (itemIt->isRaw) {
        // No cast needed on opaque-pointer LLVM — pass the pointer directly.
        Builder->CreateCall(releaseFn, {itemIt->ptr});
      } else {
        MemoryManager::emitRelease(ctx, itemIt->ptr, itemIt->typeJson);
      }
    }
  }

  if (retVal) {
    Builder->CreateRet(retVal);
  } else {
    Builder->CreateRetVoid();
  }

  // Drain the scope metadata. The block is now terminated, so compileFunction's
  // trailing popScope will skip IR emission (terminator guard), then pop these
  // now-empty frames. We clear them here so the stack sizes are balanced.
  ctx.ScopeStack.clear();
  ctx.SymbolEnv.clear();
}

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (stmt.is_null())
    return;

  std::string_view nodeType = "unknown";
  if (stmt.contains("node_type")) {
    nodeType = stmt["node_type"].get<std::string_view>();
  }

  if (nodeType == "BlockStmt") {
    compileBlockStmt(ctx, stmt);
    return;
  }
  if (nodeType == "DeclareStmt") {
    compileDeclareStmt(ctx, stmt);
    return;
  }
  if (nodeType == "AssignStmt") {
    compileAssignStmt(ctx, stmt);
    return;
  }
  if (nodeType == "ReturnStmt") {
    compileReturnStmt(ctx, stmt);
    return;
  }
  if (nodeType == "YieldStmt" || nodeType == "ExprStmt") {
    evaluateExpression(ctx, stmt["value"]);
    return;
  }
  if (nodeType == "ForStmt") {
    compileForLoop(ctx, stmt);
    return;
  }
}

void compileForLoop(CodegenContext &ctx, const nlohmann::json &s) {
  auto &Builder = ctx.Builder;
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  llvm::BasicBlock *condBB =
      llvm::BasicBlock::Create(ctx.Context, "for.cond", parentFn);
  llvm::BasicBlock *bodyBB =
      llvm::BasicBlock::Create(ctx.Context, "for.body", parentFn);
  llvm::BasicBlock *loopExitBB =
      llvm::BasicBlock::Create(ctx.Context, "for.exit", parentFn);

  if (s.contains("init") && !s["init"].is_null()) {
    compileStatement(ctx, s["init"]);
  }
  Builder->CreateBr(condBB);

  Builder->SetInsertPoint(condBB);
  if (s.contains("condition") && !s["condition"].is_null()) {
    llvm::Value *condVal = evaluateExpression(ctx, s["condition"]);
    Builder->CreateCondBr(condVal, bodyBB, loopExitBB);
  } else {
    Builder->CreateBr(bodyBB);
  }

  Builder->SetInsertPoint(bodyBB);
  if (s.contains("body") && !s["body"].is_null()) {
    compileStatement(ctx, s["body"]);
  }

  // The body may have emitted a ReturnStmt or other terminator.
  // Only compile the post-statement and back-edge if the block is still open.
  if (!Builder->GetInsertBlock()->getTerminator()) {
    if (s.contains("post") && !s["post"].is_null()) {
      compileStatement(ctx, s["post"]);
    }
    // Guard again: the post-statement itself could also terminate (e.g. return
    // inside a for-init expression, though unusual — be safe).
    if (!Builder->GetInsertBlock()->getTerminator()) {
      Builder->CreateBr(condBB);
    }
  }

  Builder->SetInsertPoint(loopExitBB);
}

} // namespace maml