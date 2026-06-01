#ifndef MAML_EXPR_GENERATOR_H
#define MAML_EXPR_GENERATOR_H

#include <llvm/IR/Value.h>

#include <nlohmann/json.hpp>

#include "CodegenContext.h"

namespace maml {

// --- ExprCore ---
llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr);

// --- ExprOperators ---
llvm::Value *compilePrefixExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileInfixExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileLogicalAnd(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal);
llvm::Value *compileLogicalOr(CodegenContext &ctx, const nlohmann::json &expr, llvm::Value *leftVal);


// --- ExprContainers ---
llvm::Value *compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileIndexExpr(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileFieldAccess(CodegenContext &ctx, const nlohmann::json &e);
llvm::Value *compileZeroAllocExpr(CodegenContext &ctx, const nlohmann::json &expr);

// --- ExprCalls ---
llvm::Value *compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr);

}  // namespace maml

#endif  // MAML_EXPR_GENERATOR_H