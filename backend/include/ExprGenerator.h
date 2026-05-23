#ifndef MAML_EXPR_GENERATOR_H
#define MAML_EXPR_GENERATOR_H

#include "CodegenContext.h"
#include <nlohmann/json.hpp>
#include <llvm/IR/Value.h>
#include <string_view>

namespace maml {

// --- ExprCore ---
llvm::Value* evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr);

// --- ExprOperators ---
llvm::Value* compilePrefixExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileInfixExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileLogicalAnd(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal);
llvm::Value* compileLogicalOr(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal);

// --- ExprControlFlow ---
llvm::Value* compileIfExpr(CodegenContext &ctx, const nlohmann::json &e);
llvm::Value* compileMatchExpr(CodegenContext &ctx, const nlohmann::json &e);
llvm::Value* compilePatternCheck(CodegenContext &ctx, const nlohmann::json &pattern, llvm::Value *subject, const nlohmann::json &subjectType);
void injectPatternBindings(CodegenContext &ctx, const nlohmann::json &pattern, llvm::Value *subject, const nlohmann::json &subjectType);
llvm::Value* compileBlockExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileAwaitExpression(CodegenContext &ctx, const nlohmann::json &e);

// --- ExprContainers ---
llvm::Value* compileArrayLiteral(CodegenContext &ctx, const nlohmann::json &e);
llvm::Value* compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileStructLiteral(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileVariantLiteral(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileSliceExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileIndexExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* compileFieldAccess(CodegenContext &ctx, const nlohmann::json &e);

// --- ExprCalls ---
llvm::Value* compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value* handleVectorBuiltin(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *objVal, llvm::Type *objTy, std::string_view methodName);
llvm::Value* handleMapBuiltin(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *objVal, llvm::Type *objTy, std::string_view methodName);

} // namespace maml

#endif // MAML_EXPR_GENERATOR_H