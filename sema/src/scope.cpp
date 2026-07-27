#include "scope.h"
#include "type_registry.h"

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
    Scope* global = new Scope(nullptr);

    // 1. Inject polymorphic Option variants
    const types::Type* anyType = registry.getPrimitive(types::TypeKind::Any);
    const types::Type* optType = registry.getOption(anyType, sym);

    SymID someId = sym.intern("Some");
    global->defineSymbol(someId,
        Symbol { SymbolKind::Variant, someId, optType, false, ast::Capability::None, optType, 0 });

    SymID noneId = sym.intern("None");
    global->defineSymbol(noneId,
        Symbol { SymbolKind::Variant, noneId, optType, false, ast::Capability::None, optType, 1 });

    // 2. Inject polymorphic Result variants
    const types::Type* resType = registry.getResult(anyType, anyType, sym);

    SymID okId = sym.intern("Ok");
    global->defineSymbol(okId,
        Symbol { SymbolKind::Variant, okId, resType, false, ast::Capability::None, resType, 0 });

    SymID errId = sym.intern("Err");
    global->defineSymbol(errId,
        Symbol { SymbolKind::Variant, errId, resType, false, ast::Capability::None, resType, 1 });

    return global;
}

} // namespace maml::sema