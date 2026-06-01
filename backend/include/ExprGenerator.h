// backend/include/ExprGenerator.h

#ifndef MAML_EXPR_GENERATOR_H
#define MAML_EXPR_GENERATOR_H

#include <llvm/IR/Value.h>

#include <nlohmann/json.hpp>

#include "CodegenContext.h"

namespace maml {

// Evaluates fully-flattened atomic operands (identifiers and primitive literals)
llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr);

llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr);
llvm::Value *compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr);

}  // namespace maml

#endif  // MAML_EXPR_GENERATOR_H