#include "scope.h"
#include "ast_nodes.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"

namespace maml::sema {

void Scope::defineSymbol(SymID name, Symbol sym) { symbols_[name] = sym; }

Symbol* Scope::resolveSymbol(SymID name)
{
    auto it = symbols_.find(name);
    if (it != symbols_.end()) {
        return &it->second; // Return pointer to the stable map value
    }
    if (parent_) {
        return parent_->resolveSymbol(name);
    }
    return nullptr;
}

Symbol* Scope::resolveSymbolLocal(SymID name)
{
    auto it = symbols_.find(name);
    if (it != symbols_.end()) {
        return &it->second;
    }
    return nullptr;
}

void Scope::defineType(SymID name, const types::Type* type) { types_[name] = type; }

const types::Type* Scope::resolveType(SymID name)
{
    auto it = types_.find(name);
    if (it != types_.end()) {
        return it->second;
    }
    if (parent_) {
        return parent_->resolveType(name);
    }
    return nullptr;
}

Scope* createGlobalScope(types::TypeRegistry& registry, SymbolTable& sym)
{
    // Note: The Analyzer will take ownership of this pointer and clean it up.
    auto* global = new Scope(nullptr);

    // 1. Inject polymorphic Option variants
    const types::Type* anyType = registry.getPrimitive(types::TypeKind::Any);
    const types::Type* optType = registry.getOption(anyType, sym);

    SymID someId = sym.intern("Some");
    global->defineSymbol(someId,
        Symbol { .kind=SymbolKind::Variant, .name=someId, .type=optType, .isMutable=false, .cap=ast::Capability::Ro, .sumType=optType, .variantDiscriminant=0 });

    SymID noneId = sym.intern("None");
    global->defineSymbol(noneId,
        Symbol { .kind=SymbolKind::Variant, .name=noneId, .type=optType, .isMutable=false, .cap=ast::Capability::Ro, .sumType=optType, .variantDiscriminant=1 });

    // 2. Inject polymorphic Result variants
    const types::Type* resType = registry.getResult(anyType, anyType, sym);

    SymID okId = sym.intern("Ok");
    global->defineSymbol(okId,
        Symbol { .kind=SymbolKind::Variant, .name=okId, .type=resType, .isMutable=false, .cap=ast::Capability::Ro, .sumType=resType, .variantDiscriminant=0 });

    SymID errId = sym.intern("Err");
    global->defineSymbol(errId,
        Symbol { .kind=SymbolKind::Variant, .name=errId, .type=resType, .isMutable=false, .cap=ast::Capability::Ro, .sumType=resType, .variantDiscriminant=1 });

    return global;
}

} // namespace maml::sema