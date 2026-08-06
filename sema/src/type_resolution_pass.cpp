#include "ast.h"
#include "capability.h"
#include "compiler_context.h"
#include "passes.h"
#include "scope.h"
#include "sym.h"
#include "type_resolver.h"
#include "types.h"

#include <cstddef>
#include <utility>
#include <variant>
#include <vector>

namespace maml::sema {

void TypeResolutionPass::visit(ast::Program& program)
{
    // 1. Resolve custom type bodies (struct fields & sum type variants)
    for (auto& decl : program.decls) {
        if (auto** tdPtr = std::get_if<ast::TypeDecl*>(&decl); tdPtr && *tdPtr) {
            visit(**tdPtr);
        }
    }

    // 2. Resolve function signatures (parameter types, capabilities, return types)
    for (auto& decl : program.decls) {
        if (auto** fnPtr = std::get_if<ast::FnDecl*>(&decl); fnPtr && *fnPtr) {
            visit(**fnPtr);
        }
    }
}

void TypeResolutionPass::visit(ast::TypeDecl& node)
{
    if (!node.name || !ctx_.globalScope) {
        return;
    }

    SymID typeName = node.name->name;
    const types::Type* shell = ctx_.globalScope->resolveType(typeName);
    if (!shell) {
        return; // Error already reported during DeclarationPass
    }

    types::TypeResolver resolver(ctx_.types.registry, ctx_.symbols);

    if (auto** structPtr = std::get_if<ast::StructTypeExpr*>(&node.rhs); structPtr && *structPtr) {
        ast::StructTypeExpr* rhs = *structPtr;
        std::vector<types::StructField> fields;
        fields.reserve(rhs->fields.size());

        for (const auto& f : rhs->fields) {
            const types::Type* fieldType = resolver.resolve(f.type, ctx_.diagnostics);
            fields.push_back({ f.name, fieldType });
        }

        // Update the pre-registered shell struct with resolved field types
        ctx_.types.registry.updateStruct(shell, std::move(fields));
        ctx_.semantic.setTypeOf(&node, shell);

    } else if (auto** sumPtr = std::get_if<ast::SumTypeExpr*>(&node.rhs); sumPtr && *sumPtr) {
        ast::SumTypeExpr* rhs = *sumPtr;
        std::vector<types::SumVariant> variants;
        variants.reserve(rhs->variants.size());

        for (size_t i = 0; i < rhs->variants.size(); ++i) {
            const auto& v = rhs->variants[i];
            types::SumVariant variant;
            variant.name = v.name;
            variant.discriminant = static_cast<int>(i);

            variant.fields.reserve(v.fields.size());
            for (const auto& f : v.fields) {
                variant.fields.push_back({ f.name, resolver.resolve(f.type, ctx_.diagnostics) });
            }

            variant.tupleTypes.reserve(v.tupleFields.size());
            for (const auto& tf : v.tupleFields) {
                variant.tupleTypes.push_back(resolver.resolve(tf, ctx_.diagnostics));
            }

            variants.push_back(std::move(variant));
        }

        // Update pre-registered sum type shell
        ctx_.types.registry.updateSum(shell, variants);

        // Register variant constructors as symbols in scope
        for (const auto& v : variants) {
            Symbol sym;
            sym.kind = SymbolKind::Variant;
            sym.name = v.name;
            sym.type = shell;
            sym.isMutable = false;
            sym.cap = Capability::Ro;
            sym.sumType = shell;
            sym.variantDiscriminant = v.discriminant;

            ctx_.globalScope->defineSymbol(v.name, sym);
        }

        ctx_.semantic.setTypeOf(&node, shell);
    }
}

void TypeResolutionPass::visit(ast::FnDecl& node)
{
    types::TypeResolver resolver(ctx_.types.registry, ctx_.symbols);

    std::vector<const types::Type*> paramTypes;
    std::vector<Capability> caps;
    paramTypes.reserve(node.params.size());
    caps.reserve(node.params.size());

    for (const auto& p : node.params) {
        paramTypes.push_back(resolver.resolve(p.type, ctx_.diagnostics));
        caps.push_back(p.cap);
    }

    const types::Type* returnType = ctx_.types.registry.getPrimitive(types::TypeKind::Unit);
    if (!std::holds_alternative<std::monostate>(node.returnType)) {
        returnType = resolver.resolve(node.returnType, ctx_.diagnostics);
    }

    if (node.isAsync) {
        returnType = ctx_.types.registry.getFuture(returnType);
    }

    const types::Type* fnType
        = ctx_.types.registry.getFunction(std::move(paramTypes), std::move(caps), returnType);

    // Update function symbol in global scope
    if (ctx_.globalScope) {
        if (Symbol* sym = ctx_.globalScope->resolveSymbolLocal(node.name)) {
            sym->type = fnType;
        }
    }

    ctx_.semantic.setTypeOf(&node, fnType);
}

} // namespace maml::sema