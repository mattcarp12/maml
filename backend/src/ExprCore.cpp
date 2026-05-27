#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view varName = expr["value"].get<std::string_view>();
  llvm::Value *val = ctx.resolveSymbol(varName);

  if (!val) {
    if (expr.contains("maml_type") && expr["maml_type"]["kind"] == "SumType") {
      for (const auto &v : expr["maml_type"]["variants"]) {
        if (v["name"].get<std::string_view>() == varName) {
          llvm::Type *sumTy = llvmTypeFor(ctx, expr["maml_type"]);
          llvm::AllocaInst *alloca =
              Builder->CreateAlloca(sumTy, nullptr, std::string("unit_variant_") + std::string(varName));
          llvm::Value *discrimGep = Builder->CreateGEP(sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
          Builder->CreateStore(Builder->getInt32(v["discriminant"].get<int>()), discrimGep);
          return alloca;
        }
      }
    }
    ctx.Error.fatal("Variable '" + std::string(varName) + "' is not defined in the current scope.", expr);
    return nullptr;
  }

  llvm::Type *expectedTy = llvmTypeFor(ctx, expr["maml_type"]);
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(val)) {
    llvm::Type *allocTy = alloca->getAllocatedType();
    if (allocTy->isArrayTy()) return alloca;
    return Builder->CreateLoad(allocTy, alloca, std::string(varName) + "_load");
  }

  if (val->getType()->isPointerTy() && !expectedTy->isArrayTy()) {
    return Builder->CreateLoad(expectedTy, val, std::string(varName) + "_load");
  }
  return val;
}

llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr) {
  if (expr.is_null()) return nullptr;

  auto &Context = ctx.Context;

  std::string_view nodeType = "unknown";
  if (expr.contains("node_type")) {
    nodeType = expr["node_type"].get<std::string_view>();
  }

  if (nodeType == "IntLiteral")
    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), expr["value"].get<int>());
  if (nodeType == "BoolLiteral")
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), expr["value"].get<bool>() ? 1 : 0);
  if (nodeType == "Identifier") return compileIdentifier(ctx, expr);
  if (nodeType == "PrefixExpr") return compilePrefixExpr(ctx, expr);
  if (nodeType == "InfixExpr") return compileInfixExpr(ctx, expr);
  if (nodeType == "FieldAccess") return compileFieldAccess(ctx, expr);
  if (nodeType == "StringLiteral") return compileStringLiteral(ctx, expr);
  if (nodeType == "VariantLiteral") return compileVariantLiteral(ctx, expr);
  if (nodeType == "SliceExpr") return compileSliceExpr(ctx, expr);
  if (nodeType == "CallExpr") return compileCallExpr(ctx, expr);
  if (nodeType == "IndexExpr") return compileIndexExpr(ctx, expr);
  if (nodeType == "ZeroAllocExpr") return compileZeroAllocExpr(ctx, expr);
  if (nodeType == "AsyncPrologueExpr") return compileAsyncPrologueExpr(ctx, expr);

  ctx.Error.fatal("Unsupported expression node type encountered: " + std::string(nodeType), expr);
  return nullptr;
}

}  // namespace maml