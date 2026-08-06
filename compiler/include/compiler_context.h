#pragma once

#include "arena.h"
#include "diagnostics.h"
#include "scope.h"
#include "semantic_tables.h"
#include "sym.h"
#include "type_lowering.h"
#include "type_registry.h"

namespace maml {

struct TypeSystem {
    TypeSystem(Arena& arena, SymbolTable& sym)
        : registry(arena)
        , lowering(registry, sym)
    {
    }
    types::TypeRegistry registry;
    types::TypeLowering lowering;
};

struct CompilerContext {
    CompilerContext(Arena& arena, SymbolTable& sym)
        : arena(arena)
        , symbols(sym)
        , types(arena, sym)
        , globalScope(sema::createGlobalScope(arena, types.registry, types.lowering, sym))
    {
    }

    Arena& arena;
    Diagnostics diagnostics;
    SymbolTable& symbols;
    TypeSystem types;
    SemanticTables semantic;
    sema::Scope* globalScope = nullptr;
};

} // namespace maml