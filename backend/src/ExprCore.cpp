#include "ExprGenerator.h"

namespace maml {

llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view varName = expr["value"].get<std::string_view>();
  llvm::Value *val = ctx.resolveSymbol(varName);

  if (!val) {
    ctx.Error.fatal("Variable '" + std::string(varName) + "' is not defined in the current scope.", expr);
    return nullptr;
  }

  // In our flattened MIR, all local variables are explicit pointers (Allocas).
  // When evaluating an identifier as an expression, we implicitly load its value.
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(val)) {
    return Builder->CreateLoad(alloca->getAllocatedType(), alloca, std::string(varName) + "_load");
  }

  return val;
}

llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr) {
  if (expr.is_null()) return nullptr;
  auto &Context = ctx.Context;

  std::string_view op = "unknown";
  if (expr.contains("op")) {
    op = expr["op"].get<std::string_view>();
  }

  // Primitive Literals
  if (op == "const_int") return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), expr["value"].get<int64_t>());
  if (op == "const_bool")
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), expr["value"].get<bool>() ? 1 : 0);
  if (op == "const_string") return compileStringLiteral(ctx, expr);

  // Core Evaluators
  if (op == "ident") return compileIdentifier(ctx, expr);
  if (op == "prefix") return compilePrefixExpr(ctx, expr);
  if (op == "infix") return compileInfixExpr(ctx, expr);
  if (op == "call") return compileCallExpr(ctx, expr);

  // Complex Types
  if (op == "slice") return compileSliceExpr(ctx, expr);
  if (op == "index_read") return compileIndexExpr(ctx, expr);

  // -------------------------------------------------------------------------
  // alloc_composite — Map / Vec / SumType pass-through literal.
  //
  // Named struct literals are never seen here: they are fully decomposed into
  // temp_decl + struct_init instructions by the MIR flatten pass and never
  // appear as an expression operand at codegen time.
  //
  // Map and Vec literals still arrive as alloc_composite because they delegate
  // to runtime constructors rather than GEP sequences.
  // -------------------------------------------------------------------------
  if (op == "alloc_composite") return compileZeroAllocExpr(ctx, expr);

  // -------------------------------------------------------------------------
  // field_access — safety net for any FieldAccess node that bypassed the MIR
  // flatten pass (should not occur in normal compilation, but kept to avoid a
  // cryptic "unknown op" error if something regresses upstream).
  //
  // NOTE: The canonical path for field access after the MIR rewrite is the
  // field_read STATEMENT instruction handled in StmtGenerator.cpp, not this
  // expression evaluator.  field_access as an expression no longer carries a
  // "maml_type" annotation on the object, so compileFieldAccess will fatal if
  // it is reached — which is intentional: it surfaces the upstream issue.
  // -------------------------------------------------------------------------
  if (op == "field_access") return compileFieldAccess(ctx, expr);

  ctx.Error.fatal("Unknown expression op: " + std::string(op), expr);
  return nullptr;
}

}  // namespace maml