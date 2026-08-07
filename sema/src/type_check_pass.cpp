#include "ast.h"
#include "capability.h"
#include "compiler_context.h"
#include "passes.h"
#include "scope.h"
#include "semantic_tables.h"
#include "sym.h"
#include "token.h"
#include "type_lowering.h"
#include "type_registry.h"
#include "type_resolver.h"
#include "types.h"

#include <cstddef>
#include <string>
#include <string_view>
#include <variant>
#include <vector>

namespace maml::sema {

// Thread-local transient state for pattern matching and async call checking
static thread_local const types::Type* subjectType_ = nullptr;
static thread_local bool allowAsyncCall_ = false;

// C++ pattern matching helper
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

static const char* capabilityToString(Capability cap)
{
    switch (cap) {
    case Capability::Own:
        return "own";
    case Capability::Mut:
        return "mut";
    case Capability::Ro:
        return "ro";
    }
    return "ro";
}

static bool isValidMemoryPath(const ast::Expr& expr)
{
    return std::visit(
        overloaded { [](ast::Identifier*) { return true; },
            [](ast::FieldAccess* fa) { return fa ? isValidMemoryPath(fa->object) : false; },
            [](ast::IndexExpr* idx) { return idx ? isValidMemoryPath(idx->left) : false; },
            [](auto) { return false; } },
        expr);
}

static bool containsView(const types::Type* t)
{
    if (!t)
        return false;
    switch (t->kind) {
    case types::TypeKind::View:
        return true;
    case types::TypeKind::Struct: {
        const auto& p = std::get<types::StructPayload>(t->payload);
        for (const auto& f : p.fields) {
            if (containsView(f.type))
                return true;
        }
        return false;
    }
    case types::TypeKind::Array:
        return containsView(std::get<types::ArrayPayload>(t->payload).base);
    case types::TypeKind::Vector:
        return containsView(std::get<types::VectorPayload>(t->payload).base);
    case types::TypeKind::Map: {
        const auto& p = std::get<types::MapPayload>(t->payload);
        return containsView(p.key) || containsView(p.value);
    }
    default:
        return false;
    }
}

static int getIntBitWidth(types::TypeKind k)
{
    switch (k) {
    case types::TypeKind::I8:
    case types::TypeKind::U8:
        return 8;
    case types::TypeKind::I16:
    case types::TypeKind::U16:
        return 16;
    case types::TypeKind::I32:
    case types::TypeKind::U32:
        return 32;
    case types::TypeKind::I64:
    case types::TypeKind::U64:
        return 64;
    case types::TypeKind::I128:
    case types::TypeKind::U128:
        return 128;
    default:
        return 0;
    }
}

static bool isUnsignedInt(types::TypeKind k)
{
    return k == types::TypeKind::U8 || k == types::TypeKind::U16 || k == types::TypeKind::U32
        || k == types::TypeKind::U64 || k == types::TypeKind::U128;
}

// Returns the promoted common integer type if safe and lossless, or nullptr if incompatible.
static const types::Type* tryPromoteIntegers(const types::Type* a, const types::Type* b)
{
    if (!a || !b || !a->isInteger() || !b->isInteger())
        return nullptr;
    if (a == b)
        return a;

    int widthA = getIntBitWidth(a->kind);
    int widthB = getIntBitWidth(b->kind);
    bool unsignA = isUnsignedInt(a->kind);
    bool unsignB = isUnsignedInt(b->kind);

    // Rule 1: Smaller unsigned integers can safely widen to any larger signed or unsigned integer.
    // Rule 2: Smaller signed integers can safely widen to larger signed integers.
    if (widthA < widthB && (unsignA || unsignA == unsignB))
        return b;
    if (widthB < widthA && (unsignB || unsignA == unsignB))
        return a;

    return nullptr; // Incompatible (e.g., same-width signed vs. unsigned, or lossy narrowing)
}

static bool isCompatible(const types::Type* expected, const types::Type* actual)
{
    if (!expected || !actual)
        return false;
    if (expected == actual)
        return true;

    if (expected->kind == types::TypeKind::Any || actual->kind == types::TypeKind::Any) {
        return true;
    }

    if (expected->kind == types::TypeKind::Sum && actual->kind == types::TypeKind::Sum) {
        const auto& ep = std::get<types::SumPayload>(expected->payload);
        const auto& ap = std::get<types::SumPayload>(actual->payload);

        if (ep.baseName == ap.baseName && ep.typeArgs.size() == ap.typeArgs.size()) {
            for (size_t i = 0; i < ep.typeArgs.size(); ++i) {
                if (!isCompatible(ep.typeArgs[i], ap.typeArgs[i])) {
                    return false;
                }
            }
            return true;
        }
    }

    return false;
}

static const types::Type* mergeTypes(const types::Type* t1, const types::Type* t2,
    types::TypeRegistry& reg, types::TypeLowering& lowering, SymbolTable& sym)
{
    if (!t1 || !t2)
        return nullptr;
    if (t1 == t2)
        return t1;

    if (t1->kind == types::TypeKind::Sum && t2->kind == types::TypeKind::Sum) {
        const auto& p1 = std::get<types::SumPayload>(t1->payload);
        const auto& p2 = std::get<types::SumPayload>(t2->payload);

        if (p1.baseName == p2.baseName && p1.typeArgs.size() == p2.typeArgs.size()) {
            std::vector<const types::Type*> mergedArgs;
            bool ok = true;
            for (size_t i = 0; i < p1.typeArgs.size(); ++i) {
                const types::Type* m
                    = mergeTypes(p1.typeArgs[i], p2.typeArgs[i], reg, lowering, sym);
                if (!m || m->kind == types::TypeKind::Unknown) {
                    ok = false;
                    break;
                }
                mergedArgs.push_back(m);
            }
            if (ok) {
                std::string_view baseStr = sym.resolve(p1.baseName);
                if (baseStr == "Option" && mergedArgs.size() == 1) {
                    return lowering.getOption(mergedArgs[0]);
                }
                if (baseStr == "Result" && mergedArgs.size() == 2) {
                    return lowering.getResult(mergedArgs[0], mergedArgs[1]);
                }
                return reg.getSum(p1.baseName, p1.variants, mergedArgs);
            }
        }
    }

    if (isCompatible(t1, t2)) {
        if (t1->kind == types::TypeKind::Any)
            return t2;
        if (t2->kind == types::TypeKind::Any)
            return t1;
        return t1;
    }

    if (t1->kind != types::TypeKind::Unknown && t2->kind != types::TypeKind::Unknown) {
        return reg.getPrimitive(types::TypeKind::Unknown);
    }
    return t1->kind == types::TypeKind::Unknown ? t2 : t1;
}

static const types::Type* coerceGenericSum(
    const types::Type* exprType, const types::Type* expectedType)
{
    if (!exprType || !expectedType)
        return exprType;

    if (exprType->kind == types::TypeKind::Sum && expectedType->kind == types::TypeKind::Sum) {
        const auto& ep = std::get<types::SumPayload>(exprType->payload);
        const auto& expP = std::get<types::SumPayload>(expectedType->payload);

        if (ep.baseName == expP.baseName) {
            return expectedType;
        }
    }
    return exprType;
}

// =============================================================================
// Helper Methods
// =============================================================================

void TypeCheckPass::checkExpr(const ast::Expr& expr, const types::Type* expected)
{
    const types::Type* prev = expectedType_;
    expectedType_ = expected;
    dispatch(expr);
    expectedType_ = prev;
}

void TypeCheckPass::checkPattern(const ast::Pattern& pattern, const types::Type* subjectType)
{
    const types::Type* prev = subjectType_;
    subjectType_
        = subjectType ? subjectType : ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
    dispatch(pattern);
    subjectType_ = prev;
}

const types::Type* TypeCheckPass::exprTypeOf(const ast::Expr& expr) const
{
    return std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                          [this](auto&& e) -> const types::Type* {
                              return e ? ctx_.semantic.typeOf(e) : nullptr;
                          } },
        expr);
}

Position TypeCheckPass::posOf(const ast::Expr& expr) const
{
    return std::visit(overloaded { [](std::monostate) -> Position { return Position {}; },
                          [](auto&& e) -> Position { return e ? e->pos : Position {}; } },
        expr);
}

Symbol* TypeCheckPass::getRootSymbol(const ast::Expr& expr)
{
    if (auto* idPtr = std::get_if<ast::Identifier*>(&expr))
        return currentScope_ ? currentScope_->resolveSymbol((*idPtr)->name) : nullptr;
    if (auto* faPtr = std::get_if<ast::FieldAccess*>(&expr))
        return getRootSymbol((*faPtr)->object);
    if (auto* idxPtr = std::get_if<ast::IndexExpr*>(&expr))
        return getRootSymbol((*idxPtr)->left);
    return nullptr;
}

const ValueCategory* TypeCheckPass::valueCategoryOf(const ast::Expr& expr) const
{
    return std::visit(overloaded { [](std::monostate) -> const ValueCategory* { return nullptr; },
                          [this](auto&& e) -> const ValueCategory* {
                              return e ? ctx_.semantic.valueCategoryOf(e) : nullptr;
                          } },
        expr);
}

// =============================================================================
// Declarations & Statements
// =============================================================================

void TypeCheckPass::visit(ast::Program& program)
{
    for (auto& decl : program.decls) {
        dispatch(decl);
    }
}

void TypeCheckPass::visit(ast::FnDecl& fn)
{
    Symbol* fnSym = currentScope_ ? currentScope_->resolveSymbol(fn.name) : nullptr;
    if (!fnSym || !fnSym->type || fnSym->type->kind != types::TypeKind::Function) {
        return;
    }

    if (ctx_.symbols.resolve(fn.name) == "main" && fn.isAsync) {
        ctx_.diagnostics.error(
            fn.pos, "the 'main' function cannot be async; you must manually spawn tasks");
    }

    const auto& fnPayload = std::get<types::FunctionPayload>(fnSym->type->payload);

    if (fn.isAsync && fnPayload.returnType->kind == types::TypeKind::Future) {
        expectedReturn_ = std::get<types::FuturePayload>(fnPayload.returnType->payload).base;
    } else {
        expectedReturn_ = fnPayload.returnType;
    }
    isAsync_ = fn.isAsync;

    pushScope();

    for (size_t i = 0; i < fn.params.size(); ++i) {
        Symbol paramSym;
        paramSym.kind = SymbolKind::Param;
        paramSym.name = fn.params[i].name;
        paramSym.type = fnPayload.params[i];
        paramSym.cap = fnPayload.caps[i];
        currentScope_->defineSymbol(fn.params[i].name, paramSym);
    }

    if (fn.body) {
        visit(*fn.body);
    }

    popScope();
    expectedReturn_ = nullptr;
    isAsync_ = false;
}

void TypeCheckPass::visit(ast::BlockStmt& block)
{
    pushScope();
    for (auto& stmt : block.statements) {
        dispatch(stmt);
    }

    const types::Type* blockType = ctx_.types.registry.getPrimitive(types::TypeKind::Unit);
    if (!block.statements.empty()) {
        if (auto** yieldPtr = std::get_if<ast::YieldStmt*>(&block.statements.back())) {
            blockType = exprTypeOf((*yieldPtr)->value);
        }
    }

    ctx_.semantic.setTypeOf(&block, blockType);
    ctx_.semantic.setValueCategory(&block, ValueCategory::RValue);
    popScope();
}

void TypeCheckPass::visit(ast::DeclareStmt& decl)
{
    checkExpr(decl.value);
    const types::Type* valType = exprTypeOf(decl.value);

    if (valType && valType->kind == types::TypeKind::Unit) {
        ctx_.diagnostics.error(
            decl.pos, "cannot assign the result of a function that returns 'unit'");
    }

    if (currentScope_ && currentScope_->resolveSymbolLocal(decl.name) != nullptr) {
        ctx_.diagnostics.error(
            decl.pos, "variable '{}' is already declared", ctx_.symbols.resolve(decl.name));
    }

    Symbol sym;
    sym.kind = SymbolKind::Var;
    sym.name = decl.name;
    sym.type = valType ? valType : ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
    sym.isMutable = decl.isMutable;
    sym.cap = Capability::Own;

    if (currentScope_) {
        currentScope_->defineSymbol(decl.name, sym);
    }
}

void TypeCheckPass::visit(ast::AliasDecl& alias)
{
    checkExpr(alias.value);
    const types::Type* valType = exprTypeOf(alias.value);

    Symbol sym;
    sym.kind = SymbolKind::Var;
    sym.name = alias.name;
    sym.type = valType ? valType : ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
    sym.isMutable = false;
    sym.cap = alias.cap;
    if (currentScope_) {
        currentScope_->defineSymbol(alias.name, sym);
    }

    if (alias.cap == Capability::Mut) {
        Symbol* rootSym = getRootSymbol(alias.value);
        if (rootSym && !rootSym->isMutable && rootSym->cap != Capability::Mut) {
            ctx_.diagnostics.error(alias.pos,
                "cannot take a 'mut' alias of immutable variable '{}'",
                ctx_.symbols.resolve(rootSym->name));
        }
    }

    if (!isValidMemoryPath(alias.value)) {
        ctx_.diagnostics.error(alias.pos,
            "the '{}' capability can only be applied to variables and their fields",
            capabilityToString(alias.cap));
    }
}

void TypeCheckPass::visit(ast::AssignStmt& assign)
{
    checkExpr(assign.lValue);
    const types::Type* lType = exprTypeOf(assign.lValue);

    if (auto** idxPtr = std::get_if<ast::IndexExpr*>(&assign.lValue)) {
        const types::Type* leftType = exprTypeOf((*idxPtr)->left);
        if (leftType && leftType->kind == types::TypeKind::Map) {
            lType = std::get<types::MapPayload>(leftType->payload).value;
        }
    }

    checkExpr(assign.rValue, lType);
    const types::Type* rType = exprTypeOf(assign.rValue);

    if (lType && rType && !isCompatible(lType, rType) && lType->kind != types::TypeKind::Unknown
        && rType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(assign.pos, "type mismatch: cannot assign '{}' to '{}'",
            rType->toString(ctx_.symbols), lType->toString(ctx_.symbols));
    }

    if (std::holds_alternative<ast::Identifier*>(assign.lValue)) {
        Symbol* rootSym = getRootSymbol(assign.lValue);
        if (!rootSym) {
            ctx_.diagnostics.error(assign.pos, "cannot assign to non-variable expression");
        } else if (!rootSym->isMutable && rootSym->cap != Capability::Mut) {
            ctx_.diagnostics.error(assign.pos, "cannot mutate immutable variable '{}'",
                ctx_.symbols.resolve(rootSym->name));
        }
    }

    if (auto** idPtr = std::get_if<ast::Identifier*>(&assign.lValue)) {
        Symbol* sym = currentScope_ ? currentScope_->resolveSymbol((*idPtr)->name) : nullptr;
        if (sym && sym->cap == Capability::Ro) {
            ctx_.diagnostics.error(assign.pos, "cannot reassign read-only borrow '{}'",
                ctx_.symbols.resolve(sym->name));
        }
    }
}

void TypeCheckPass::visit(ast::ReturnStmt& ret)
{
    const types::Type* retType = ctx_.types.registry.getPrimitive(types::TypeKind::Unit);
    if (!std::holds_alternative<std::monostate>(ret.value)) {
        checkExpr(ret.value, expectedReturn_);
        retType = exprTypeOf(ret.value);
    }

    const types::Type* expected = expectedReturn_
        ? expectedReturn_
        : ctx_.types.registry.getPrimitive(types::TypeKind::Unit);

    if (expectedReturn_ && retType) {
        retType = coerceGenericSum(retType, expectedReturn_);
    }

    if (retType && containsView(retType)) {
        ctx_.diagnostics.error(ret.pos, "cannot return a View from a function");
    }

    if (retType && expected && !isCompatible(expected, retType)
        && retType->kind != types::TypeKind::Unknown
        && expected->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(ret.pos, "type mismatch: expected return type '{}', got '{}'",
            expected->toString(ctx_.symbols), retType->toString(ctx_.symbols));
    }
}

void TypeCheckPass::visit(ast::ForStmt& forStmt)
{
    pushScope();
    dispatch(forStmt.init);
    checkExpr(forStmt.condition);
    dispatch(forStmt.post);

    const types::Type* condType = exprTypeOf(forStmt.condition);
    if (condType && condType->kind != types::TypeKind::Bool
        && condType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(forStmt.pos, "for-loop condition must be of type 'bool', got '{}'",
            condType->toString(ctx_.symbols));
    }

    if (forStmt.body) {
        visit(*forStmt.body);
    }
    popScope();
}

void TypeCheckPass::visit(ast::ExprStmt& exprStmt) { checkExpr(exprStmt.value); }
void TypeCheckPass::visit(ast::YieldStmt& yield) { checkExpr(yield.value); }

// =============================================================================
// Expression Visitors
// =============================================================================

void TypeCheckPass::visit(ast::Identifier& id)
{
    Symbol* sym = currentScope_ ? currentScope_->resolveSymbol(id.name) : nullptr;
    if (!sym) {
        ctx_.diagnostics.error(id.pos, "undefined name '{}'", ctx_.symbols.resolve(id.name));
        ctx_.semantic.setTypeOf(&id, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        ctx_.semantic.setValueCategory(&id, ValueCategory::RValue);
        return;
    }

    ctx_.semantic.setResolvedSymbol(&id, id.name);

    if (sym->kind == SymbolKind::Var || sym->kind == SymbolKind::Param) {
        ctx_.semantic.setValueCategory(&id, ValueCategory::LValue);
    } else {
        ctx_.semantic.setValueCategory(&id, ValueCategory::RValue);
    }

    if (sym->kind == SymbolKind::Variant) {
        if (expectedType_ && expectedType_->kind == types::TypeKind::Sum) {
            const auto& expectedPayload = std::get<types::SumPayload>(expectedType_->payload);
            const auto& symPayload = std::get<types::SumPayload>(sym->sumType->payload);

            if (expectedPayload.baseName == symPayload.baseName) {
                ctx_.semantic.setTypeOf(&id, expectedType_);
                return;
            }
        }
        ctx_.semantic.setTypeOf(&id, sym->sumType);
        return;
    }

    ctx_.semantic.setTypeOf(&id, sym->type);
}

void TypeCheckPass::visit(ast::IntLiteral& lit)
{
    ctx_.semantic.setTypeOf(&lit, ctx_.types.registry.getPrimitive(types::TypeKind::I64));
    ctx_.semantic.setValueCategory(&lit, ValueCategory::RValue);
}

void TypeCheckPass::visit(ast::BoolLiteral& lit)
{
    ctx_.semantic.setTypeOf(&lit, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
    ctx_.semantic.setValueCategory(&lit, ValueCategory::RValue);
}

void TypeCheckPass::visit(ast::StringLiteral& lit)
{
    ctx_.semantic.setTypeOf(&lit, ctx_.types.registry.getPrimitive(types::TypeKind::String));
    ctx_.semantic.setValueCategory(&lit, ValueCategory::RValue);
}

void TypeCheckPass::visit(ast::InfixExpr& infix)
{
    checkExpr(infix.left);
    checkExpr(infix.right);
    ctx_.semantic.setValueCategory(&infix, ValueCategory::RValue);

    const types::Type* lType = exprTypeOf(infix.left);
    const types::Type* rType = exprTypeOf(infix.right);

    if (auto* leftLit = std::get_if<ast::IntLiteral*>(&infix.left)) {
        if (rType && rType->isInteger() && rType->canRepresentInt((*leftLit)->value)) {
            ctx_.semantic.setTypeOf(*leftLit, rType);
            lType = rType;
        }
    }
    if (auto* rightLit = std::get_if<ast::IntLiteral*>(&infix.right)) {
        if (lType && lType->isInteger() && lType->canRepresentInt((*rightLit)->value)) {
            ctx_.semantic.setTypeOf(*rightLit, lType);
            rType = lType;
        }
    }

    if (!lType || !rType || lType->kind == types::TypeKind::Unknown
        || rType->kind == types::TypeKind::Unknown) {
        ctx_.semantic.setTypeOf(&infix, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    // Check for safe integer widening before reporting a type mismatch
    const types::Type* commonType = lType;
    if (lType != rType) {
        commonType = tryPromoteIntegers(lType, rType);
        if (!commonType) {
            ctx_.diagnostics.error(infix.pos,
                "type mismatch: cannot apply operator to '{}' and '{}'",
                lType->toString(ctx_.symbols), rType->toString(ctx_.symbols));
            ctx_.semantic.setTypeOf(
                &infix, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
            return;
        }
    }

    switch (infix.op) {
    case TokenType::PLUS:
    case TokenType::MINUS:
    case TokenType::MULTIPLY:
    case TokenType::DIVIDE:
    case TokenType::MODULO:
        if (!commonType->isInteger()) {
            ctx_.diagnostics.error(infix.pos, "operator requires integer operands, got '{}'",
                commonType->toString(ctx_.symbols));
        }
        // Set the expression type to the promoted common type (e.g., i64 for u8 + i64)
        ctx_.semantic.setTypeOf(&infix, commonType);
        break;
    case TokenType::EQ:
    case TokenType::NOT_EQ:
    case TokenType::LT:
    case TokenType::LTE:
    case TokenType::GT:
    case TokenType::GTE:
        ctx_.semantic.setTypeOf(&infix, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
        break;
    case TokenType::AND:
    case TokenType::OR:
        if (lType->kind != types::TypeKind::Bool) {
            ctx_.diagnostics.error(infix.pos, "operator requires 'bool' operands");
        }
        ctx_.semantic.setTypeOf(&infix, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
        break;
    default:
        ctx_.semantic.setTypeOf(&infix, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
    }
}

void TypeCheckPass::visit(ast::PrefixExpr& prefix)
{
    checkExpr(prefix.right);
    ctx_.semantic.setValueCategory(&prefix, ValueCategory::RValue);

    const types::Type* rType = exprTypeOf(prefix.right);

    if (!rType || rType->kind == types::TypeKind::Unknown) {
        ctx_.semantic.setTypeOf(
            &prefix, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    if (prefix.op == TokenType::NOT) {
        if (rType->kind != types::TypeKind::Bool) {
            ctx_.diagnostics.error(
                prefix.pos, "operator '!' expects 'bool', got '{}'", rType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&prefix, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
    } else if (prefix.op == TokenType::MINUS) {
        if (!rType->isInteger()) {
            ctx_.diagnostics.error(prefix.pos, "operator '-' expects integer, got '{}'",
                rType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&prefix, rType);
    }
}

void TypeCheckPass::visit(ast::CallExpr& call)
{
    checkExpr(call.function);
    ctx_.semantic.setValueCategory(&call, ValueCategory::RValue);

    if (auto** funcIdPtr = std::get_if<ast::Identifier*>(&call.function)) {
        Symbol* sym = currentScope_ ? currentScope_->resolveSymbol((*funcIdPtr)->name) : nullptr;
        if (sym && sym->kind == SymbolKind::Variant) {
            std::string_view varName = ctx_.symbols.resolve(sym->name);

            auto checkIntrinsicVariant = [&](std::string_view expectedName,
                                             auto buildFallbackType) {
                if (call.arguments.size() != 1) {
                    ctx_.diagnostics.error(call.pos, "variant '{}' expects 1 argument, got {}",
                        expectedName, call.arguments.size());
                    ctx_.semantic.setTypeOf(
                        &call, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
                    return true;
                }
                const types::Type* expectedT = nullptr;
                if (expectedType_ && expectedType_->kind == types::TypeKind::Sum) {
                    const auto& payload = std::get<types::SumPayload>(expectedType_->payload);
                    if (ctx_.symbols.resolve(payload.baseName)
                            == (expectedName == "Some" ? "Option" : "Result")
                        && !payload.typeArgs.empty()) {
                        expectedT = (expectedName == "Err" && payload.typeArgs.size() >= 2)
                            ? payload.typeArgs[1]
                            : payload.typeArgs[0];
                    }
                }

                checkExpr(call.arguments[0].argument, expectedT);
                const types::Type* argType = exprTypeOf(call.arguments[0].argument);

                if (expectedType_ && expectedT && isCompatible(expectedT, argType)) {
                    ctx_.semantic.setTypeOf(&call, expectedType_);
                    return true;
                }

                ctx_.semantic.setTypeOf(&call, buildFallbackType(argType));
                return true;
            };

            if (varName == "Some") {
                if (checkIntrinsicVariant("Some",
                        [this](const types::Type* t) { return ctx_.types.lowering.getOption(t); }))
                    return;
            } else if (varName == "Ok") {
                if (checkIntrinsicVariant("Ok", [this](const types::Type* t) {
                        const types::Type* anyType
                            = ctx_.types.registry.getPrimitive(types::TypeKind::Any);
                        return ctx_.types.lowering.getResult(t, anyType);
                    }))
                    return;
            } else if (varName == "Err") {
                if (checkIntrinsicVariant("Err", [this](const types::Type* e) {
                        const types::Type* anyType
                            = ctx_.types.registry.getPrimitive(types::TypeKind::Any);
                        return ctx_.types.lowering.getResult(anyType, e);
                    }))
                    return;
            }

            const types::Type* sumType = sym->sumType;
            const auto& payload = std::get<types::SumPayload>(sumType->payload);
            const types::SumVariant* targetVariant = nullptr;

            for (const auto& v : payload.variants) {
                if (v.name == sym->name) {
                    targetVariant = &v;
                    break;
                }
            }

            if (!targetVariant) {
                ctx_.semantic.setTypeOf(
                    &call, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
                return;
            }

            if (call.arguments.size() != targetVariant->tupleTypes.size()) {
                ctx_.diagnostics.error(call.pos, "variant '{}' expects {} argument(s), got {}",
                    ctx_.symbols.resolve(sym->name), targetVariant->tupleTypes.size(),
                    call.arguments.size());
                ctx_.semantic.setTypeOf(&call, sumType);
                return;
            }

            for (size_t i = 0; i < call.arguments.size(); ++i) {
                const types::Type* expType = targetVariant->tupleTypes[i];
                checkExpr(call.arguments[i].argument, expType);
                const types::Type* argType = exprTypeOf(call.arguments[i].argument);

                if (argType && argType != expType && argType->kind != types::TypeKind::Unknown
                    && expType->kind != types::TypeKind::Unknown
                    && expType->kind != types::TypeKind::Any) {
                    ctx_.diagnostics.error(call.arguments[i].pos,
                        "type mismatch for variant argument {}: expected '{}', got '{}'", i + 1,
                        expType->toString(ctx_.symbols), argType->toString(ctx_.symbols));
                }
            }

            ctx_.semantic.setTypeOf(&call, sumType);
            return;
        }
    }

    const types::Type* fnType = exprTypeOf(call.function);
    if (!fnType || fnType->kind != types::TypeKind::Function) {
        if (fnType && fnType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(
                call.pos, "cannot call non-function type '{}'", fnType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&call, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    const auto& fnPayload = std::get<types::FunctionPayload>(fnType->payload);
    ctx_.semantic.setTypeOf(&call, fnPayload.returnType);

    if (call.arguments.size() != fnPayload.params.size()) {
        ctx_.diagnostics.error(call.pos, "wrong number of arguments: expected {}, got {}",
            fnPayload.params.size(), call.arguments.size());
        return;
    }

    for (size_t i = 0; i < call.arguments.size(); ++i) {
        const types::Type* expType = fnPayload.params[i];
        checkExpr(call.arguments[i].argument, expType);
        const types::Type* argType = exprTypeOf(call.arguments[i].argument);
        Capability actualCap = call.arguments[i].cap;
        Capability expectedCap = fnPayload.caps[i];

        bool isLiteral = std::holds_alternative<ast::StringLiteral*>(call.arguments[i].argument);
        if (isLiteral && actualCap == Capability::Ro && expectedCap == Capability::Own) {
            actualCap = Capability::Own;
        }

        if (auto* argLit = std::get_if<ast::IntLiteral*>(&call.arguments[i].argument)) {
            if (expType->isInteger() && expType->canRepresentInt((*argLit)->value)) {
                ctx_.semantic.setTypeOf(*argLit, expType);
                argType = expType;
            }
        }

        if (argType && argType != expType && argType->kind != types::TypeKind::Unknown
            && expType->kind != types::TypeKind::Unknown && expType->kind != types::TypeKind::Any) {
            ctx_.diagnostics.error(call.arguments[i].pos,
                "argument {} type mismatch: expected '{}', got '{}'", i + 1,
                expType->toString(ctx_.symbols), argType->toString(ctx_.symbols));
        }

        if (expectedCap != actualCap) {
            ctx_.diagnostics.error(call.arguments[i].pos,
                "capability mismatch on argument {}: function expects '{}', but call site passed "
                "'{}'",
                i + 1, capabilityToString(expectedCap), capabilityToString(actualCap));
        }

        if (actualCap == Capability::Mut) {
            if (!isValidMemoryPath(call.arguments[i].argument)) {
                ctx_.diagnostics.error(call.arguments[i].pos,
                    "the 'mut' capability can only be applied to variables and their fields");
            }
        }
    }

    if (auto** funcIdPtr = std::get_if<ast::Identifier*>(&call.function)) {
        if (ctx_.symbols.resolve((*funcIdPtr)->name) == "run_executor") {
            if (call.arguments.size() != 1) {
                ctx_.diagnostics.error(call.pos, "'run_executor' expects exactly 1 argument");
            } else {
                const types::Type* argType = exprTypeOf(call.arguments[0].argument);
                if (argType && argType->kind == types::TypeKind::Future) {
                    ctx_.semantic.setTypeOf(
                        &call, std::get<types::FuturePayload>(argType->payload).base);
                } else if (argType && argType->kind != types::TypeKind::Unknown) {
                    ctx_.diagnostics.error(call.pos, "'run_executor' expects a Future, got '{}'",
                        argType->toString(ctx_.symbols));
                }
            }
        }
    }

    bool isAsyncCall = (fnPayload.returnType->kind == types::TypeKind::Future);
    if (isAsyncCall && !allowAsyncCall_) {
        std::string fnName = "async function";
        if (auto** idPtr = std::get_if<ast::Identifier*>(&call.function)) {
            fnName = "'" + std::string(ctx_.symbols.resolve((*idPtr)->name)) + "'";
        }
        ctx_.diagnostics.error(call.pos, "{} must be called with 'await' or 'spawn'", fnName);
    }
}

void TypeCheckPass::visit(ast::IfExpr& ifExpr)
{
    checkExpr(ifExpr.condition);
    if (ifExpr.consequence)
        visit(*ifExpr.consequence);
    if (ifExpr.alternative)
        visit(*ifExpr.alternative);

    ctx_.semantic.setValueCategory(&ifExpr, ValueCategory::RValue);

    const types::Type* condType = exprTypeOf(ifExpr.condition);
    if (condType && condType->kind != types::TypeKind::Bool
        && condType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(ifExpr.pos, "if condition must be of type 'bool'");
    }

    const types::Type* consType = ifExpr.consequence
        ? exprTypeOf(ast::Expr(ifExpr.consequence))
        : ctx_.types.registry.getPrimitive(types::TypeKind::Unit);
    const types::Type* altType = ifExpr.alternative
        ? exprTypeOf(ast::Expr(ifExpr.alternative))
        : ctx_.types.registry.getPrimitive(types::TypeKind::Unit);

    const types::Type* merged
        = mergeTypes(consType, altType, ctx_.types.registry, ctx_.types.lowering, ctx_.symbols);
    ctx_.semantic.setTypeOf(&ifExpr, merged);

    if (merged->kind == types::TypeKind::Unknown && consType->kind != types::TypeKind::Unknown
        && altType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(ifExpr.pos, "if/else branches have incompatible types");
    }
}

void TypeCheckPass::visit(ast::AwaitExpr& await)
{
    allowAsyncCall_ = true;
    checkExpr(await.value);
    allowAsyncCall_ = false;

    ctx_.semantic.setValueCategory(&await, ValueCategory::RValue);

    const types::Type* valType = exprTypeOf(await.value);

    if (!isAsync_) {
        ctx_.diagnostics.error(await.pos, "cannot use 'await' outside of an async function");
    }

    if (valType && valType->kind == types::TypeKind::Future) {
        ctx_.semantic.setTypeOf(&await, std::get<types::FuturePayload>(valType->payload).base);
    } else {
        if (valType && valType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(
                await.pos, "cannot await non-Future type '{}'", valType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&await, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
    }
}

void TypeCheckPass::visit(ast::SpawnExpr& spawn)
{
    allowAsyncCall_ = true;
    if (spawn.value) {
        visit(*spawn.value);
    }
    allowAsyncCall_ = false;

    ctx_.semantic.setValueCategory(&spawn, ValueCategory::RValue);

    const types::Type* valType = spawn.value ? exprTypeOf(ast::Expr(spawn.value)) : nullptr;
    if (valType && valType->kind == types::TypeKind::Future) {
        ctx_.semantic.setTypeOf(&spawn, valType);
    } else {
        if (valType && valType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(spawn.pos,
                "spawn can only be applied to async functions, got '{}'",
                valType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&spawn, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
    }
}

void TypeCheckPass::visit(ast::FieldAccess& fa)
{
    checkExpr(fa.object);
    const types::Type* objType = exprTypeOf(fa.object);

    const ValueCategory* objCat = valueCategoryOf(fa.object);
    ValueCategory faCat = (objCat && *objCat == ValueCategory::LValue) ? ValueCategory::LValue
                                                                       : ValueCategory::RValue;

    ctx_.semantic.setValueCategory(&fa, faCat);
    ctx_.semantic.setValueCategory(fa.field, faCat);

    if (!objType || objType->kind == types::TypeKind::Unknown) {
        ctx_.semantic.setTypeOf(&fa, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    if (objType->kind != types::TypeKind::Struct) {
        ctx_.diagnostics.error(fa.pos, "cannot access field '{}' on non-struct type '{}'",
            ctx_.symbols.resolve(fa.field->name), objType->toString(ctx_.symbols));
        ctx_.semantic.setTypeOf(&fa, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    const auto& payload = std::get<types::StructPayload>(objType->payload);
    for (const auto& field : payload.fields) {
        if (field.name == fa.field->name) {
            ctx_.semantic.setTypeOf(&fa, field.type);
            ctx_.semantic.setTypeOf(fa.field, field.type);
            return;
        }
    }

    ctx_.diagnostics.error(fa.field->pos, "field '{}' does not exist on struct '{}'",
        ctx_.symbols.resolve(fa.field->name), ctx_.symbols.resolve(payload.name));
    ctx_.semantic.setTypeOf(&fa, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
}

void TypeCheckPass::visit(ast::IndexExpr& idx)
{
    checkExpr(idx.left);
    checkExpr(idx.index);

    const types::Type* leftType = exprTypeOf(idx.left);
    const types::Type* idxType = exprTypeOf(idx.index);

    const ValueCategory* leftCat = valueCategoryOf(idx.left);
    bool isContainerLValue = (leftCat && *leftCat == ValueCategory::LValue);

    if (leftType
        && (leftType->kind == types::TypeKind::Array || leftType->kind == types::TypeKind::Vector
            || leftType->kind == types::TypeKind::View)
        && isContainerLValue) {
        ctx_.semantic.setValueCategory(&idx, ValueCategory::LValue);
    } else {
        ctx_.semantic.setValueCategory(&idx, ValueCategory::RValue);
    }

    if (!leftType || leftType->kind == types::TypeKind::Unknown) {
        ctx_.semantic.setTypeOf(&idx, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
        return;
    }

    if (leftType->kind == types::TypeKind::Array || leftType->kind == types::TypeKind::Vector
        || leftType->kind == types::TypeKind::View || leftType->kind == types::TypeKind::String) {

        if (idxType && idxType->kind != types::TypeKind::I64
            && idxType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(posOf(idx.index), "index must be an integer, got '{}'",
                idxType->toString(ctx_.symbols));
        }

        if (leftType->kind == types::TypeKind::Array)
            ctx_.semantic.setTypeOf(&idx, std::get<types::ArrayPayload>(leftType->payload).base);
        else if (leftType->kind == types::TypeKind::Vector)
            ctx_.semantic.setTypeOf(&idx, std::get<types::VectorPayload>(leftType->payload).base);
        else if (leftType->kind == types::TypeKind::View)
            ctx_.semantic.setTypeOf(&idx, std::get<types::ViewPayload>(leftType->payload).base);
        else
            ctx_.semantic.setTypeOf(&idx, ctx_.types.registry.getPrimitive(types::TypeKind::U8));
    } else if (leftType->kind == types::TypeKind::Map) {
        const auto& mapPayload = std::get<types::MapPayload>(leftType->payload);
        if (idxType && idxType != mapPayload.key && idxType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(posOf(idx.index), "map index must be of type '{}', got '{}'",
                mapPayload.key->toString(ctx_.symbols), idxType->toString(ctx_.symbols));
        }
        ctx_.semantic.setTypeOf(&idx, ctx_.types.lowering.getOption(mapPayload.value));
    } else {
        ctx_.diagnostics.error(idx.pos, "cannot index into non-collection type '{}'",
            leftType->toString(ctx_.symbols));
        ctx_.semantic.setTypeOf(&idx, ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
    }
}

void TypeCheckPass::visit(ast::SliceExpr& slice)
{
    checkExpr(slice.left);
    ctx_.semantic.setValueCategory(&slice, ValueCategory::RValue);

    const types::Type* leftType = exprTypeOf(slice.left);

    if (!std::holds_alternative<std::monostate>(slice.low)) {
        checkExpr(slice.low);
        const types::Type* lowType = exprTypeOf(slice.low);
        if (lowType && lowType->kind != types::TypeKind::I64
            && lowType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(posOf(slice.low), "slice low index must be an integer");
        }
    }

    if (!std::holds_alternative<std::monostate>(slice.high)) {
        checkExpr(slice.high);
        const types::Type* highType = exprTypeOf(slice.high);
        if (highType && highType->kind != types::TypeKind::I64
            && highType->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(posOf(slice.high), "slice high index must be an integer");
        }
    }

    const types::Type* resultType = ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
    if (leftType) {
        switch (leftType->kind) {
        case types::TypeKind::Array:
            resultType = ctx_.types.registry.getView(
                std::get<types::ArrayPayload>(leftType->payload).base, false);
            break;
        case types::TypeKind::Vector:
            resultType = ctx_.types.registry.getView(
                std::get<types::VectorPayload>(leftType->payload).base, false);
            break;
        case types::TypeKind::View:
            resultType = ctx_.types.registry.getView(
                std::get<types::ViewPayload>(leftType->payload).base, false);
            break;
        case types::TypeKind::String:
            resultType = ctx_.types.registry.getPrimitive(types::TypeKind::String);
            break;
        default:
            if (leftType->kind != types::TypeKind::Unknown) {
                ctx_.diagnostics.error(slice.pos, "cannot slice non-array/vector/view type '{}'",
                    leftType->toString(ctx_.symbols));
            }
        }
    }
    ctx_.semantic.setTypeOf(&slice, resultType);
}

void TypeCheckPass::visit(ast::CompositeLiteral& comp)
{
    types::TypeResolver resolver(ctx_.types.registry, ctx_.symbols, ctx_.globalScope);
    const types::Type* resolvedType = resolver.resolve(comp.typeExpr, ctx_.diagnostics);
    ctx_.semantic.setTypeOf(&comp, resolvedType);
    ctx_.semantic.setValueCategory(&comp, ValueCategory::RValue);

    if (!resolvedType || resolvedType->kind == types::TypeKind::Unknown)
        return;

    if (resolvedType->kind == types::TypeKind::Struct) {
        const auto& payload = std::get<types::StructPayload>(resolvedType->payload);
        std::vector<bool> seen(payload.fields.size(), false);

        for (auto& el : comp.elements) {
            auto** keyIdentPtr = std::get_if<ast::Identifier*>(&el.key);
            if (!keyIdentPtr) {
                ctx_.diagnostics.error(el.pos, "struct fields must be keyed with identifiers");
                checkExpr(el.value);
                continue;
            }
            SymID fieldName = (*keyIdentPtr)->name;
            checkExpr(el.value);
            const types::Type* valType = exprTypeOf(el.value);

            int foundIdx = -1;
            for (size_t i = 0; i < payload.fields.size(); ++i) {
                if (payload.fields[i].name == fieldName) {
                    foundIdx = static_cast<int>(i);
                    break;
                }
            }

            if (foundIdx == -1) {
                ctx_.diagnostics.error(el.pos, "field '{}' does not exist on struct '{}'",
                    ctx_.symbols.resolve(fieldName), ctx_.symbols.resolve(payload.name));
            } else {
                if (seen[foundIdx]) {
                    ctx_.diagnostics.error(el.pos, "duplicate field '{}' in struct literal",
                        ctx_.symbols.resolve(fieldName));
                }
                seen[foundIdx] = true;

                const types::Type* expectedType = payload.fields[foundIdx].type;
                if (valType && valType != expectedType && valType->kind != types::TypeKind::Unknown
                    && expectedType->kind != types::TypeKind::Unknown) {
                    ctx_.diagnostics.error(el.pos,
                        "type mismatch for field '{}': expected '{}', got '{}'",
                        ctx_.symbols.resolve(fieldName), expectedType->toString(ctx_.symbols),
                        valType->toString(ctx_.symbols));
                }
            }
        }

        for (size_t i = 0; i < payload.fields.size(); ++i) {
            if (!seen[i]) {
                ctx_.diagnostics.error(comp.pos, "missing field '{}' in struct literal",
                    ctx_.symbols.resolve(payload.fields[i].name));
            }
        }
    }
}

void TypeCheckPass::visit(ast::MatchExpr& match)
{
    checkExpr(match.subject);
    ctx_.semantic.setValueCategory(&match, ValueCategory::RValue);

    const types::Type* subjectType = exprTypeOf(match.subject);
    const types::Type* resultType = nullptr;

    for (auto& arm : match.arms) {
        pushScope();
        checkPattern(arm.pattern, subjectType);
        checkExpr(arm.body);
        popScope();

        const types::Type* armType = exprTypeOf(arm.body);
        if (!resultType) {
            resultType = armType;
        } else {
            const types::Type* merged = mergeTypes(
                resultType, armType, ctx_.types.registry, ctx_.types.lowering, ctx_.symbols);
            if (!merged) {
                if (armType && armType->kind != types::TypeKind::Unknown
                    && resultType->kind != types::TypeKind::Unknown) {
                    ctx_.diagnostics.error(arm.pos,
                        "match arm has incompatible type: expected '{}', got '{}'",
                        resultType->toString(ctx_.symbols), armType->toString(ctx_.symbols));
                }
                resultType = ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
            } else {
                resultType = merged;
            }
        }
    }

    ctx_.semantic.setTypeOf(&match,
        resultType ? resultType : ctx_.types.registry.getPrimitive(types::TypeKind::Unknown));
}

// =============================================================================
// Patterns
// =============================================================================

void TypeCheckPass::visit(ast::WildcardPattern&) { }

void TypeCheckPass::visit(ast::LiteralPattern& p)
{
    checkExpr(p.value);
    const types::Type* valType = exprTypeOf(p.value);
    if (valType && subjectType_ && valType != subjectType_
        && valType->kind != types::TypeKind::Unknown
        && subjectType_->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(p.pos, "pattern type mismatch: expected '{}', got '{}'",
            subjectType_->toString(ctx_.symbols), valType->toString(ctx_.symbols));
    }
}

void TypeCheckPass::visit(ast::IdentifierPattern& p)
{
    if (subjectType_ && subjectType_->kind == types::TypeKind::Sum) {
        const auto& payload = std::get<types::SumPayload>(subjectType_->payload);
        for (const auto& variant : payload.variants) {
            if (variant.name == p.name) {
                return;
            }
        }
    }

    if (currentScope_ && currentScope_->resolveSymbol(p.name) != nullptr) {
        ctx_.diagnostics.error(
            p.pos, "variable '{}' is already bound in this pattern", ctx_.symbols.resolve(p.name));
        return;
    }

    Symbol sym;
    sym.kind = SymbolKind::Var;
    sym.name = p.name;
    sym.type = subjectType_;
    if (currentScope_) {
        currentScope_->defineSymbol(p.name, sym);
    }
}

void TypeCheckPass::visit(ast::CompositePattern& p)
{
    if (!subjectType_ || subjectType_->kind != types::TypeKind::Sum) {
        if (subjectType_ && subjectType_->kind != types::TypeKind::Unknown) {
            ctx_.diagnostics.error(p.pos, "cannot match variant pattern on non-sum type '{}'",
                subjectType_->toString(ctx_.symbols));
        }
        return;
    }

    auto** namedType = std::get_if<ast::NamedTypeExpr*>(&p.typeExpr);
    if (!namedType) {
        ctx_.diagnostics.error(p.pos, "invalid type in composite pattern");
        return;
    }
    SymID variantName = (*namedType)->name->name;

    const auto& payload = std::get<types::SumPayload>(subjectType_->payload);
    const types::SumVariant* targetVariant = nullptr;
    for (const auto& v : payload.variants) {
        if (v.name == variantName) {
            targetVariant = &v;
            break;
        }
    }

    if (!targetVariant) {
        ctx_.diagnostics.error(p.pos, "variant '{}' does not exist on type '{}'",
            ctx_.symbols.resolve(variantName), ctx_.symbols.resolve(payload.baseName));
        return;
    }

    size_t tupleIdx = 0;
    for (const auto& elem : p.elements) {
        if (std::holds_alternative<std::monostate>(elem.key)) {
            const types::Type* fieldType = nullptr;
            if (tupleIdx < targetVariant->tupleTypes.size()) {
                fieldType = targetVariant->tupleTypes[tupleIdx];
            } else {
                ctx_.diagnostics.error(elem.pos, "extra positional argument in tuple variant '{}'",
                    ctx_.symbols.resolve(variantName));
                fieldType = ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);
            }

            if (auto identPat = std::get_if<ast::IdentifierPattern*>(&elem.pattern)) {
                if (currentScope_ && currentScope_->resolveSymbol((*identPat)->name)) {
                    ctx_.diagnostics.error(elem.pos,
                        "variable '{}' is already bound in this pattern",
                        ctx_.symbols.resolve((*identPat)->name));
                } else if (currentScope_) {
                    Symbol sym;
                    sym.kind = SymbolKind::Var;
                    sym.name = (*identPat)->name;
                    sym.type = fieldType;
                    currentScope_->defineSymbol((*identPat)->name, sym);
                }
            }
            tupleIdx++;
        }
    }
}

void TypeCheckPass::visit(ast::VecPushStmt& v)
{
    checkExpr(v.lValue);
    const types::Type* lType = exprTypeOf(v.lValue);

    const types::Type* elemType = nullptr;
    if (lType && lType->kind == types::TypeKind::Vector) {
        elemType = std::get<types::VectorPayload>(lType->payload).base;
    } else if (lType && lType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(
            v.pos, "cannot push to non-vector type '{}'", lType->toString(ctx_.symbols));
    }

    checkExpr(v.rValue, elemType);
    const types::Type* rType = exprTypeOf(v.rValue);

    if (elemType && rType && !isCompatible(elemType, rType)
        && elemType->kind != types::TypeKind::Unknown && rType->kind != types::TypeKind::Unknown) {
        ctx_.diagnostics.error(v.pos, "type mismatch: cannot push '{}' to vector of '{}'",
            rType->toString(ctx_.symbols), elemType->toString(ctx_.symbols));
    }
}

} // namespace maml::sema