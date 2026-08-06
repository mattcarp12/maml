

#include "scope.h"
#include "arena.h"
#include "capability.h"
#include "sym.h"
#include "type_lowering.h"
#include "type_registry.h"
#include "types.h"

namespace maml::sema {
// Arena-allocated global scope initialization
Scope* createGlobalScope(
    Arena& arena, types::TypeRegistry& registry, types::TypeLowering& lowering, SymbolTable& sym)
{
    auto* global = arena.make<Scope>(nullptr);

    // 1. Inject polymorphic Option variants
    const types::Type* anyType = registry.getPrimitive(types::TypeKind::Any);
    const types::Type* optType = lowering.getOption(anyType);

    SymID someId = sym.intern("Some");
    global->defineSymbol(someId,
        Symbol { .kind = SymbolKind::Variant,
            .name = someId,
            .type = optType,
            .isMutable = false,
            .cap = Capability::Ro,
            .sumType = optType,
            .variantDiscriminant = 0 });

    SymID noneId = sym.intern("None");
    global->defineSymbol(noneId,
        Symbol { .kind = SymbolKind::Variant,
            .name = noneId,
            .type = optType,
            .isMutable = false,
            .cap = Capability::Ro,
            .sumType = optType,
            .variantDiscriminant = 1 });

    // 2. Inject polymorphic Result variants
    const types::Type* resType = lowering.getResult(anyType, anyType);

    SymID okId = sym.intern("Ok");
    global->defineSymbol(okId,
        Symbol { .kind = SymbolKind::Variant,
            .name = okId,
            .type = resType,
            .isMutable = false,
            .cap = Capability::Ro,
            .sumType = resType,
            .variantDiscriminant = 0 });

    SymID errId = sym.intern("Err");
    global->defineSymbol(errId,
        Symbol { .kind = SymbolKind::Variant,
            .name = errId,
            .type = resType,
            .isMutable = false,
            .cap = Capability::Ro,
            .sumType = resType,
            .variantDiscriminant = 1 });

    return global;
}
}