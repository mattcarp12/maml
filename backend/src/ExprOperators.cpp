#include "ExprGenerator.h"

namespace maml {

llvm::Value *compilePrefixExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Value *right = evaluateExpression(ctx, expr["right"]);
  std::string_view opSymbol = expr["operator"].get<std::string_view>();

  if (opSymbol == "!") {
    return Builder->CreateXor(right, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
  }
  if (opSymbol == "-") {
    return Builder->CreateSub(llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), right, "negtmp");
  }
  return nullptr;
}

llvm::Value *compileInfixExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view opSymbol = expr["operator"].get<std::string_view>();

  llvm::Value *left = evaluateExpression(ctx, expr["left"]);

  // TEMPORARY: Short-circuit evaluations (Move this to Go MIR Builder later)
  if (opSymbol == "&&") return compileLogicalAnd(ctx, expr, left);
  if (opSymbol == "||") return compileLogicalOr(ctx, expr, left);

  llvm::Value *right = evaluateExpression(ctx, expr["right"]);

  // Division by Zero Safety Check
  if (opSymbol == "/" || opSymbol == "%") {
    llvm::Value *isZero = Builder->CreateICmpEQ(right, llvm::ConstantInt::get(right->getType(), 0), "is_zero");

    llvm::Function *F = Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock *trapBB = llvm::BasicBlock::Create(ctx.Context, "trap_div_zero", F);
    llvm::BasicBlock *contBB = llvm::BasicBlock::Create(ctx.Context, "cont_div", F);

    Builder->CreateCondBr(isZero, trapBB, contBB);

    // Trap Block: Abort execution
    Builder->SetInsertPoint(trapBB);
    llvm::Function *trapFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::trap);
    Builder->CreateCall(trapFn);
    Builder->CreateUnreachable();

    // Continue Block: Safe to divide
    Builder->SetInsertPoint(contBB);
    if (opSymbol == "/") return Builder->CreateSDiv(left, right, "divtmp");
    if (opSymbol == "%") return Builder->CreateSRem(left, right, "modtmp");
  }

  // Standard Math Evaluators
  if (opSymbol == "+") return Builder->CreateAdd(left, right, "addtmp");
  if (opSymbol == "-") return Builder->CreateSub(left, right, "subtmp");
  if (opSymbol == "*") return Builder->CreateMul(left, right, "multmp");

  // Comparison Evaluators
  if (opSymbol == "==") return Builder->CreateICmpEQ(left, right, "eqtmp");
  if (opSymbol == "!=") return Builder->CreateICmpNE(left, right, "neqtmp");
  if (opSymbol == "<") return Builder->CreateICmpSLT(left, right, "lttmp");
  if (opSymbol == ">") return Builder->CreateICmpSGT(left, right, "gttmp");
  if (opSymbol == "<=") return Builder->CreateICmpSLE(left, right, "letmp");
  if (opSymbol == ">=") return Builder->CreateICmpSGE(left, right, "getmp");

  ctx.Error.fatal("Unknown infix operator: " + std::string(opSymbol), expr);
  return nullptr;
}

static unsigned LogicBlockCounter = 0;

llvm::Value *compileLogicalAnd(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();

  std::string id = std::to_string(LogicBlockCounter++);
  llvm::BasicBlock *rightBB = llvm::BasicBlock::Create(ctx.Context, "and_right_" + id, F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(ctx.Context, "and_merge_" + id, F);
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();

  Builder->CreateCondBr(leftVal, rightBB, mergeBB);

  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(ctx, expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);

  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi = Builder->CreatePHI(llvm::Type::getInt1Ty(ctx.Context), 2, "and_phi_" + id);
  phi->addIncoming(llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0), leftExitBB);
  phi->addIncoming(rightVal, rightExitBB);

  return phi;
}

llvm::Value *compileLogicalOr(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();

  std::string id = std::to_string(LogicBlockCounter++);
  llvm::BasicBlock *rightBB = llvm::BasicBlock::Create(ctx.Context, "or_right_" + id, F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(ctx.Context, "or_merge_" + id, F);
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();

  // For OR: If left is true, short-circuit to merge. If false, evaluate right.
  Builder->CreateCondBr(leftVal, mergeBB, rightBB);

  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(ctx, expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);

  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi = Builder->CreatePHI(llvm::Type::getInt1Ty(ctx.Context), 2, "or_phi_" + id);

  // If we came from the left block, it means leftVal was true.
  phi->addIncoming(llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), leftExitBB);
  // If we came from the right block, the result is whatever rightVal evaluated to.
  phi->addIncoming(rightVal, rightExitBB);

  return phi;
}

}  // namespace maml