#include "ast.h"
#include "compiler_context.h"
#include "passes.h"
#include "semantic_tables.h"
#include "sym.h"
#include "types.h"

#include <cstddef>
#include <variant>
#include <vector>

namespace maml::sema {

// C++ pattern matching helper
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

static bool containsViewType(const types::Type* t)
{
    if (!t)
        return false;
    switch (t->kind) {
    case types::TypeKind::View:
        return true;
    case types::TypeKind::Struct: {
        const auto& p = std::get<types::StructPayload>(t->payload);
        for (const auto& f : p.fields) {
            if (containsViewType(f.type))
                return true;
        }
        return false;
    }
    case types::TypeKind::Array:
        return containsViewType(std::get<types::ArrayPayload>(t->payload).base);
    case types::TypeKind::Vector:
        return containsViewType(std::get<types::VectorPayload>(t->payload).base);
    case types::TypeKind::Map: {
        const auto& p = std::get<types::MapPayload>(t->payload);
        return containsViewType(p.key) || containsViewType(p.value);
    }
    default:
        return false;
    }
}

void ControlFlowPass::visit(ast::Program& program)
{
    for (auto& decl : program.decls) {
        dispatch(decl);
    }
}

void ControlFlowPass::visit(ast::FnDecl& fn)
{
    // Rule: main function cannot be async
    if (ctx_.symbols.resolve(fn.name) == "main" && fn.isAsync) {
        ctx_.diagnostics.error(
            fn.pos, "the 'main' function cannot be async; you must manually spawn tasks");
    }

    const ast::FnDecl* prevFn = currentFn_;
    bool prevAsync = isAsyncContext_;

    currentFn_ = &fn;
    isAsyncContext_ = fn.isAsync;

    if (fn.body) {
        visit(*fn.body);
    }

    currentFn_ = prevFn;
    isAsyncContext_ = prevAsync;
}

void ControlFlowPass::visit(ast::BlockStmt& block)
{
    for (auto& stmt : block.statements) {
        dispatch(stmt);
    }
}

void ControlFlowPass::visit(ast::IfExpr& ifExpr)
{
    dispatch(ifExpr.condition);
    if (ifExpr.consequence) {
        visit(*ifExpr.consequence);
    }
    if (ifExpr.alternative) {
        visit(*ifExpr.alternative);
    }
}

void ControlFlowPass::visit(ast::ForStmt& forStmt)
{
    dispatch(forStmt.init);
    dispatch(forStmt.condition);
    dispatch(forStmt.post);
    if (forStmt.body) {
        visit(*forStmt.body);
    }
}

void ControlFlowPass::visit(ast::ReturnStmt& ret)
{
    if (!std::holds_alternative<std::monostate>(ret.value)) {
        dispatch(ret.value);

        const types::Type* retType
            = std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                             [this](auto&& e) -> const types::Type* {
                                 return e ? ctx_.semantic.typeOf(e) : nullptr;
                             } },
                ret.value);

        // Rule: Functions cannot return View types to prevent dangling references
        if (retType && containsViewType(retType)) {
            ctx_.diagnostics.error(ret.pos, "cannot return a View from a function");
        }
    }
}

void ControlFlowPass::visit(ast::AwaitExpr& await)
{
    // Rule: await can only be used inside async functions
    if (!isAsyncContext_) {
        ctx_.diagnostics.error(await.pos, "cannot use 'await' outside of an async function");
    }
    dispatch(await.value);
}

void ControlFlowPass::visit(ast::ExprStmt& exprStmt) { dispatch(exprStmt.value); }

void ControlFlowPass::visit(ast::MatchExpr& match)
{
    dispatch(match.subject);

    const types::Type* subjectType
        = std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                         [this](auto&& e) -> const types::Type* {
                             return e ? ctx_.semantic.typeOf(e) : nullptr;
                         } },
            match.subject);

    for (auto& arm : match.arms) {
        dispatch(arm.body);
    }

    checkExhaustiveness(match, subjectType);
}

void ControlFlowPass::checkExhaustiveness(ast::MatchExpr& match, const types::Type* subjectType)
{
    if (!subjectType || subjectType->kind == types::TypeKind::Unknown) {
        return;
    }

    if (subjectType->kind == types::TypeKind::Sum) {
        const auto& payload = std::get<types::SumPayload>(subjectType->payload);
        std::vector<bool> covered(payload.variants.size(), false);
        bool hasWildcard = false;

        for (const auto& arm : match.arms) {
            if (std::holds_alternative<ast::WildcardPattern*>(arm.pattern)) {
                hasWildcard = true;
                break;
            } else if (auto ip = std::get_if<ast::IdentifierPattern*>(&arm.pattern)) {
                bool isUnitVariant = false;
                for (size_t i = 0; i < payload.variants.size(); ++i) {
                    if (payload.variants[i].name == (*ip)->name) {
                        covered[i] = true;
                        isUnitVariant = true;
                        break;
                    }
                }
                if (!isUnitVariant) {
                    hasWildcard = true;
                    break;
                }
            } else if (auto cp = std::get_if<ast::CompositePattern*>(&arm.pattern)) {
                if (auto** namedType = std::get_if<ast::NamedTypeExpr*>(&(*cp)->typeExpr)) {
                    SymID varName = (*namedType)->name->name;
                    for (size_t i = 0; i < payload.variants.size(); ++i) {
                        if (payload.variants[i].name == varName) {
                            covered[i] = true;
                            break;
                        }
                    }
                }
            }
        }

        if (!hasWildcard) {
            for (size_t i = 0; i < covered.size(); ++i) {
                if (!covered[i]) {
                    ctx_.diagnostics.error(match.pos,
                        "non-exhaustive match: missing case for variant '{}'",
                        ctx_.symbols.resolve(payload.variants[i].name));
                }
            }
        }
    } else {
        // Non-sum types require a wildcard '_' pattern or catch-all variable
        bool hasWildcard = false;
        for (const auto& arm : match.arms) {
            if (std::holds_alternative<ast::WildcardPattern*>(arm.pattern)
                || std::holds_alternative<ast::IdentifierPattern*>(arm.pattern)) {
                hasWildcard = true;
                break;
            }
        }
        if (!hasWildcard) {
            ctx_.diagnostics.error(match.pos,
                "non-exhaustive match: matching on type '{}' requires a wildcard '_' pattern or "
                "catch-all variable",
                subjectType->toString(ctx_.symbols));
        }
    }
}

} // namespace maml::sema