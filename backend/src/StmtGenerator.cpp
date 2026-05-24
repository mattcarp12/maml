#include "StmtGenerator.h"

#include <iostream>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

static void compileBlockStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  ctx.pushScope();
  for (const auto &s : stmt["statements"]) {
    compileStatement(ctx, s);
    if (ctx.Builder->GetInsertBlock()->getTerminator()) break;
  }
  ctx.popScope();
}

static void compileDeclareStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  std::string_view varName = stmt["name"].get<std::string_view>();

  if (stmt["value"]["maml_type"].is_null()) {
    std::cerr << "\n[CRITICAL ERROR] 'maml_type' is missing for declaration of variable: " << varName << "\n";
    ctx.Error.fatal("Missing type information aborted compilation.");
    return;
  }

  llvm::Value *initVal = evaluateExpression(ctx, stmt["value"]);
  if (!initVal) return;

  nlohmann::json typeJson = stmt["value"]["maml_type"];
  llvm::Type *valTy = llvmTypeFor(ctx, typeJson);

  // 1. Extract the RHS node type
  std::string_view rhsNodeType = "unknown";
  if (stmt["value"].contains("node_type")) {
    rhsNodeType = stmt["value"]["node_type"].get<std::string_view>();
  }

  llvm::Value *storagePtr = nullptr;

  // 2. If the frontend explicitly heap-allocated this (e.g., an escaping array),
  // bind the variable directly to the heap pointer. Do NOT copy it to the stack!
  if (rhsNodeType == "HeapAllocExpr") {
    storagePtr = initVal;
  } else {
    storagePtr = Builder->CreateAlloca(valTy, nullptr, std::string(varName));

    if (initVal->getType()->isPointerTy() && (valTy->isStructTy() || valTy->isArrayTy())) {
      llvm::Value *loadedData = Builder->CreateLoad(valTy, initVal, "array_load");
      Builder->CreateStore(loadedData, storagePtr);
    } else {
      Builder->CreateStore(initVal, storagePtr);
    }
  }

  ctx.SymbolEnv.back()[std::string(varName)] = storagePtr;
}

static llvm::Value *computeLValue(CodegenContext &ctx, const nlohmann::json &lvalueNode, bool &isPointerReassignment) {
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
    auto &Builder = ctx.Builder;

    // 1. Recursively compute the true L-Value pointer of the base struct object
    bool dummy = false;
    llvm::Value *objPtr = computeLValue(ctx, lvalueNode["object"], dummy);

    if (!objPtr) {
      ctx.Error.fatal("Could not compute LValue for struct object", lvalueNode);
      return nullptr;
    }

    // 2. Extract the struct type and field name from the AST node
    nlohmann::json structType = lvalueNode["object"]["maml_type"];
    std::string_view fieldName = lvalueNode["field"].get<std::string_view>();

    // 3. Locate the matching field index
    int index = -1;
    for (int i = 0; i < structType["fields"].size(); ++i) {
      if (structType["fields"][i]["name"].get<std::string_view>() == fieldName) {
        index = i;
        break;
      }
    }

    if (index < 0) {
      ctx.Error.fatal("Field '" + std::string(fieldName) + "' not found in struct LValue", lvalueNode);
      return nullptr;
    }

    // 4. Return the GEP pointer directly into the struct's actual, permanent memory!
    llvm::Type *baseTy = llvmTypeFor(ctx, structType);
    return Builder->CreateGEP(baseTy, objPtr, {Builder->getInt32(0), Builder->getInt32(index)}, "fieldptr");
  }

  if (lnodeType == "IndexExpr") {
    if (!lvalueNode["left"].contains("maml_type") || lvalueNode["left"]["maml_type"].is_null()) {
      ctx.Error.fatal("CRITICAL: Missing 'maml_type' on left side of assignment!", lvalueNode);
      return nullptr;
    }

    llvm::Value *leftVal = evaluateExpression(ctx, lvalueNode["left"]);
    llvm::Value *indexVal = evaluateExpression(ctx, lvalueNode["index"]);
    llvm::Type *leftTy = llvmTypeFor(ctx, lvalueNode["left"]["maml_type"]);

    llvm::Value *leftPtr = leftVal;
    if (!leftVal->getType()->isPointerTy()) {
      llvm::AllocaInst *spill = Builder->CreateAlloca(leftTy, nullptr, "slice_spill");
      Builder->CreateStore(leftVal, spill);
      leftPtr = spill;
    }

    if (leftTy->isArrayTy()) {
      return Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), indexVal}, "array_idx_assign");
    } else {
      // Slices
      llvm::Value *dataPtr =
          Builder->CreateGEP(leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)}, "data_ptr");
      llvm::Value *data = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtr);
      return Builder->CreateGEP(llvmTypeFor(ctx, lvalueNode["maml_type"]), data, indexVal, "slice_idx_assign");
    }
  }

  std::cerr << "Error: Invalid or undefined LValue in assignment\n";
  return nullptr;
}

static void compileAssignStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  const nlohmann::json &lvalueNode = stmt.contains("lvalue") ? stmt["lvalue"] : stmt["target"];

  bool isPointerReassignment = false;
  llvm::Value *targetPtr = computeLValue(ctx, lvalueNode, isPointerReassignment);
  if (!targetPtr) return;

  const nlohmann::json &rhsNode = stmt.contains("rvalue") ? stmt["rvalue"] : stmt["value"];
  llvm::Value *rhsVal = evaluateExpression(ctx, rhsNode);
  if (!rhsVal) return;

  nlohmann::json typeJson = rhsNode["maml_type"];

  llvm::Type *valTy = llvmTypeFor(ctx, typeJson);
  if (isPointerReassignment) {
    Builder->CreateStore(rhsVal, targetPtr);
  } else if (rhsVal->getType()->isPointerTy() && (valTy->isStructTy() || valTy->isArrayTy())) {
    llvm::Value *loadedData = Builder->CreateLoad(valTy, rhsVal, "array_load");
    Builder->CreateStore(loadedData, targetPtr);
  } else {
    Builder->CreateStore(rhsVal, targetPtr);
  }
}

static void compileReturnStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;

  // Coroutine early-out: suspend points handle ARC, just return the handle.
  if (llvm::Value *coroHdl = ctx.resolveSymbol(rt::CORO_HDL)) {
    Builder->CreateRet(coroHdl);
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

  if (retVal) {
    Builder->CreateRet(retVal);
  } else {
    Builder->CreateRetVoid();
  }
}

static void compileLoopStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *loopBB = llvm::BasicBlock::Create(ctx.Context, "loop.body", F);
  llvm::BasicBlock *exitBB = llvm::BasicBlock::Create(ctx.Context, "loop.exit", F);

  Builder->CreateBr(loopBB);
  Builder->SetInsertPoint(loopBB);

  ctx.LoopExitStack.push_back(exitBB);
  compileBlockStmt(ctx, stmt["body"]);
  ctx.LoopExitStack.pop_back();

  if (!Builder->GetInsertBlock()->getTerminator()) {
    Builder->CreateBr(loopBB);
  }
  Builder->SetInsertPoint(exitBB);
}

static void compileBreakStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (!ctx.LoopExitStack.empty()) {
    ctx.Builder->CreateBr(ctx.LoopExitStack.back());
  } else {
    ctx.Error.fatal("Break statement encountered outside of a loop", stmt);
  }
}

static void compileRetainStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
  llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
      "maml_retain",
      llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context), {llvm::PointerType::getUnqual(ctx.Context)}, false));

  if (val && val->getType()->isPointerTy()) {
    Builder->CreateCall(retainFn, {val});
  }
}

static void compileReleaseStmt(CodegenContext &ctx, const nlohmann::json &stmt) {
  auto &Builder = ctx.Builder;
  llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
  llvm::FunctionCallee releaseFn = ctx.Module->getOrInsertFunction(
      "maml_release",
      llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context), {llvm::PointerType::getUnqual(ctx.Context)}, false));

  if (val && val->getType()->isPointerTy()) {
    Builder->CreateCall(releaseFn, {val});
  }
}

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (stmt.is_null()) return;

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
  if (nodeType == "LoopStmt") {
    compileLoopStmt(ctx, stmt);
    return;
  }
  if (nodeType == "BreakStmt") {
    compileBreakStmt(ctx, stmt);
    return;
  }
  if (nodeType == "RetainStmt") {
    compileRetainStmt(ctx, stmt);
    return;
  }
  if (nodeType == "ReleaseStmt") {
    compileReleaseStmt(ctx, stmt);
    return;
  }
  if (nodeType == "YieldStmt" || nodeType == "ExprStmt") {
    evaluateExpression(ctx, stmt["value"]);
    return;
  }
}

}  // namespace maml