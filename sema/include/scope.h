#pragma once
#include "ast_nodes.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"
#include <unordered_map>

namespace maml::sema {

enum class SymbolKind : uint8_t { Var, Func, Param, Variant };

struct Symbol {
    SymbolKind kind;
    SymID name;
    const types::Type* type = nullptr;

    bool isMutable = false;
    ast::Capability cap = ast::Capability::Ro;

    // For variant symbols, we track the parent sum type and discriminant index.
    // In Go, you stored pointers to the variant struct itself, but since types
    // are interned in C++, storing the parent type and index is safer and leaner.
    const types::Type* sumType = nullptr;
    int variantDiscriminant = -1;
};

class Scope {
public:
    explicit Scope(Scope* parent = nullptr)
        : parent_(parent)
    {
    }

    // Symbol Management
    void defineSymbol(SymID name, Symbol sym);
    Symbol* resolveSymbol(SymID name);
    Symbol* resolveSymbolLocal(SymID name);

    // Custom Type Management (e.g., structs, sum types)
    void defineType(SymID name, const types::Type* type);
    const types::Type* resolveType(SymID name);

    Scope* getParent() const { return parent_; }

private:
    Scope* parent_;

    // We use SymID for O(1) hashing and ultra-fast lookups
    std::unordered_map<SymID, Symbol> symbols_;
    std::unordered_map<SymID, const types::Type*> types_;
};

// Factory to initialize the global scope with built-in polymorphic variants
Scope* createGlobalScope(types::TypeRegistry& registry, SymbolTable& sym);

} // namespace maml::sema