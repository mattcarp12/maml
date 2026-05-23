#include "ExprGenerator.h"

namespace maml {

llvm::Value *compilePrefixExpr(CodegenContext &ctx,
                               const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Value *right = evaluateExpression(ctx, expr["right"]);
  std::string_view op = expr["operator"].get<std::string_view>();

  if (op == "!")
    return Builder->CreateXor(
        right, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1),
        "nottmp");
  if (op == "-")
    return Builder->CreateSub(
        llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), right,
        "negtmp");
  return nullptr;
}

llvm::Value *compileInfixExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view op = expr["operator"].get<std::string_view>();
  llvm::Value *left = evaluateExpression(ctx, expr["left"]);

  if (op == "&&")
    return compileLogicalAnd(ctx, expr, left);
  if (op == "||")
    return compileLogicalOr(ctx, expr, left);

  llvm::Value *right = evaluateExpression(ctx, expr["right"]);

  if (op == "+")
    return Builder->CreateAdd(left, right, "addtmp");
  if (op == "-")
    return Builder->CreateSub(left, right, "subtmp");
  if (op == "*")
    return Builder->CreateMul(left, right, "multmp");
  if (op == "/")
    return Builder->CreateSDiv(left, right, "divtmp");
  if (op == "%")
    return Builder->CreateSRem(left, right, "remtmp");
  if (op == "==")
    return Builder->CreateICmpEQ(left, right, "eqtmp");
  if (op == "!=")
    return Builder->CreateICmpNE(left, right, "netmp");
  if (op == "<")
    return Builder->CreateICmpSLT(left, right, "lttmp");
  if (op == "<=")
    return Builder->CreateICmpSLE(left, right, "letmp");
  if (op == ">")
    return Builder->CreateICmpSGT(left, right, "gttmp");
  if (op == ">=")
    return Builder->CreateICmpSGE(left, right, "getmp");
  return nullptr;
}

// Logical AND/OR remain structurally unchanged
llvm::Value *compileLogicalAnd(CodegenContext &ctx, const nlohmann::json &expr,
                               llvm::Value *leftVal) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *rightBB =
      llvm::BasicBlock::Create(ctx.Context, "and_right", F);
  llvm::BasicBlock *mergeBB =
      llvm::BasicBlock::Create(ctx.Context, "and_merge", F);
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();
  Builder->CreateCondBr(leftVal, rightBB, mergeBB);
  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(ctx, expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);
  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi =
      Builder->CreatePHI(llvm::Type::getInt1Ty(ctx.Context), 2, "and_phi");
  phi->addIncoming(
      llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0),
      leftExitBB);
  phi->addIncoming(rightVal, rightExitBB);
  return phi;
}

llvm::Value *compileLogicalOr(CodegenContext &ctx, const nlohmann::json &expr,
                              llvm::Value *leftVal) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *rightBB =
      llvm::BasicBlock::Create(ctx.Context, "or_right", F);
  llvm::BasicBlock *mergeBB =
      llvm::BasicBlock::Create(ctx.Context, "or_merge", F);
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();
  Builder->CreateCondBr(leftVal, mergeBB, rightBB);
  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(ctx, expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);
  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi =
      Builder->CreatePHI(llvm::Type::getInt1Ty(ctx.Context), 2, "or_phi");
  phi->addIncoming(
      llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1),
      leftExitBB);
  phi->addIncoming(rightVal, rightExitBB);
  return phi;
}

} // namespace maml