#pragma once

#include "arena.h"
#include "capability.h"
#include "sym.h"
#include "type_lowering.h"
#include "type_registry.h"
#include "types.h"
#include <cstdint>
#include <unordered_map>

namespace maml::sema {

enum class SymbolKind : uint8_t { Var, Func, Param, Variant };

struct Symbol {
    SymbolKind kind;
    SymID name;
    const types::Type* type = nullptr;
    bool isMutable = false;
    Capability cap = Capability::Ro;
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
    void defineSymbol(SymID name, Symbol sym) { symbols_[name] = sym; }

    Symbol* resolveSymbol(SymID name)
    {
        auto it = symbols_.find(name);
        if (it != symbols_.end()) {
            return &it->second;
        }
        if (parent_) {
            return parent_->resolveSymbol(name);
        }
        return nullptr;
    }

    Symbol* resolveSymbolLocal(SymID name)
    {
        auto it = symbols_.find(name);
        if (it != symbols_.end()) {
            return &it->second;
        }
        return nullptr;
    }

    // Custom Type Management
    void defineType(SymID name, const types::Type* type) { types_[name] = type; }

    const types::Type* resolveType(SymID name)
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

    [[nodiscard]] Scope* getParent() const { return parent_; }

private:
    Scope* parent_;
    std::unordered_map<SymID, Symbol> symbols_;
    std::unordered_map<SymID, const types::Type*> types_;
};

Scope* createGlobalScope(
    Arena& arena, types::TypeRegistry& registry, types::TypeLowering& lowering, SymbolTable& sym);

} // namespace maml::sema