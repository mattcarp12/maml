#pragma once
#include "arena.h"
#include "diagnostics.h"
#include "semantic_tables.h"
#include "sym.h"
#include "type_lowering.h"
#include "type_registry.h"

namespace maml {

// Wraps symbol-table-related state. Currently just the one SymbolTable
// from sym.h, but gives passes a stable `ctx.symbols.*` surface to grow
// into later (e.g. scope stacks) without CompilerContext itself changing.
struct SymbolTables {
    explicit SymbolTables(types::SymbolTable& table)
        : table(table)
    {
    }
    types::SymbolTable& table;
};

// Everything type-related: the canonical TypeRegistry plus TypeLowering
// (Option/Result/tagged-union layout). TypeResolver (AST type-expr ->
// Type*) belongs here too once ast.h is wired up — see type_resolver.h.
struct TypeSystem {
    TypeSystem(Arena& arena, types::SymbolTable& sym)
        : registry(arena)
        , lowering(registry, sym)
    {
    }
    types::TypeRegistry registry;
    types::TypeLowering lowering;
    // types::TypeResolver resolver; // TODO(you): needs ast.h, see type_resolver.h
};

// CompilerContext is a thin composition root: it owns the independent
// subsystems each pass needs, but holds no pass logic of its own. Per the
// design notes, this is deliberately NOT a god object — passes should only
// reach into the piece(s) they actually need:
//
//   void TypeCheckPass::visit(ast::BinaryExpr& node) {
//       const Type* lhs = ctx_.semantic.typeOf(&node.lhs);
//       ...
//       if (mismatch) ctx_.diagnostics.error("type mismatch in binary expr");
//   }
struct CompilerContext {
    CompilerContext(Arena& arena, types::SymbolTable& sym)
        : symbols(sym)
        , types(arena, sym)
    {
    }

    Diagnostics diagnostics;
    SymbolTables symbols;
    TypeSystem types;
    SemanticTables semantic;
};

} // namespace maml
