#include "ast.h"
#include "compiler_context.h"
#include "passes.h"
#include "scope.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"

#include <variant>

namespace maml::sema {

void DeclarationPass::visit(ast::Program& program)
{
    // 1. Hoist type shells (structs & sum types) into the global scope first.
    // This allows self-referential and cross-referential types to resolve correctly in Phase 3.
    for (auto& decl : program.decls) {
        if (auto** tdPtr = std::get_if<ast::TypeDecl*>(&decl); tdPtr && *tdPtr) {
            visit(**tdPtr);
        }
    }

    // 2. Hoist function symbols into scope after types are registered.
    for (auto& decl : program.decls) {
        if (auto** fnPtr = std::get_if<ast::FnDecl*>(&decl); fnPtr && *fnPtr) {
            visit(**fnPtr);
        }
    }
}

void DeclarationPass::visit(ast::TypeDecl& node)
{
    if (!node.name) {
        ctx_.diagnostics.error(node.pos, "type declaration missing identifier");
        return;
    }

    SymID typeName = node.name->name;

    // Duplicate type declaration check in global scope
    if (ctx_.globalScope && ctx_.globalScope->resolveType(typeName) != nullptr) {
        ctx_.diagnostics.error(
            node.pos, "type '{}' is already defined", ctx_.symbols.resolve(typeName));
        return;
    }

    // Allocate empty type shells in the registry and register them in scope.
    // Full field and variant definitions are populated in TypeResolutionPass (Phase 3).
    if (std::holds_alternative<ast::StructTypeExpr*>(node.rhs)) {
        const types::Type* shell = ctx_.types.registry.getStruct(typeName, {});
        if (ctx_.globalScope) {
            ctx_.globalScope->defineType(typeName, shell);
        }
    } else if (std::holds_alternative<ast::SumTypeExpr*>(node.rhs)) {
        const types::Type* shell = ctx_.types.registry.getSum(typeName, {});
        if (ctx_.globalScope) {
            ctx_.globalScope->defineType(typeName, shell);
        }
    }
}

void DeclarationPass::visit(ast::FnDecl& node)
{
    // Duplicate symbol declaration check in global scope
    if (ctx_.globalScope && ctx_.globalScope->resolveSymbolLocal(node.name) != nullptr) {
        ctx_.diagnostics.error(
            node.pos, "symbol '{}' is already defined", ctx_.symbols.resolve(node.name));
        return;
    }

    // Register function symbol with placeholder Unknown type.
    // TypeResolutionPass (Phase 3) will resolve parameter/return types and update sym.type.
    Symbol sym;
    sym.kind = SymbolKind::Func;
    sym.name = node.name;
    sym.type = ctx_.types.registry.getPrimitive(types::TypeKind::Unknown);

    if (ctx_.globalScope) {
        ctx_.globalScope->defineSymbol(node.name, sym);
    }
}

} // namespace maml::sema