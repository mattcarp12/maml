#pragma once
#include "sym.h"
#include "types.h"
#include <cstdint>
#include <unordered_map>
#include <variant>

namespace maml {

enum class ValueCategory : uint8_t { LValue, RValue };

// Minimal constant-value representation for constant-folding results.
// TODO(you): extend this (e.g. string/array constants) once you have a
// real constant-value representation to reuse.
using ConstantValue = std::variant<std::monostate, int64_t, double, bool>;

// SemanticTables holds every per-node semantic fact computed by passes —
// expression types, resolved symbols, constant values, lvalue/rvalue-ness —
// keyed by AST node identity, instead of storing them as fields on the AST
// nodes themselves. This is exactly the "side table" split from the design
// notes; it deliberately does NOT try to hold things that are cheaper to
// just mutate directly on the AST (implicit casts, desugaring, lowering).
//
// NOTE: keyed on `const void*` (raw node address), which assumes AST nodes
// are stable, arena-allocated addresses — the same assumption TypeRegistry
// already makes for interning `const Type*`. If your AST instead uses a
// NodeID/handle scheme, swap the key type for that; the rest of this API
// is unaffected.
class SemanticTables {
public:
    void setTypeOf(const void* node, const types::Type* type) { exprTypes_[node] = type; }
    [[nodiscard]] const types::Type* typeOf(const void* node) const
    {
        auto it = exprTypes_.find(node);
        return it == exprTypes_.end() ? nullptr : it->second;
    }

    void setResolvedSymbol(const void* node, types::SymID sym) { resolvedSymbols_[node] = sym; }
    [[nodiscard]] const types::SymID* resolvedSymbolOf(const void* node) const
    {
        auto it = resolvedSymbols_.find(node);
        return it == resolvedSymbols_.end() ? nullptr : &it->second;
    }

    void setConstantValue(const void* node, ConstantValue value) { constants_[node] = std::move(value); }
    [[nodiscard]] const ConstantValue* constantValueOf(const void* node) const
    {
        auto it = constants_.find(node);
        return it == constants_.end() ? nullptr : &it->second;
    }

    void setValueCategory(const void* node, ValueCategory category) { valueCategories_[node] = category; }
    [[nodiscard]] const ValueCategory* valueCategoryOf(const void* node) const
    {
        auto it = valueCategories_.find(node);
        return it == valueCategories_.end() ? nullptr : &it->second;
    }

private:
    std::unordered_map<const void*, const types::Type*> exprTypes_;
    std::unordered_map<const void*, types::SymID> resolvedSymbols_;
    std::unordered_map<const void*, ConstantValue> constants_;
    std::unordered_map<const void*, ValueCategory> valueCategories_;
};

} // namespace maml
