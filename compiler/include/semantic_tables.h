#pragma once
#include "sym.h"
#include "types.h"
#include <cstdint>
#include <unordered_map>
#include <variant>

namespace maml {

enum class ValueCategory : uint8_t { LValue, RValue };

using ConstantValue = std::variant<std::monostate, int64_t, double, bool>;

class SemanticTables {
public:
    void setTypeOf(const void* node, const types::Type* type) { exprTypes_[node] = type; }
    [[nodiscard]] const types::Type* typeOf(const void* node) const
    {
        auto it = exprTypes_.find(node);
        return it == exprTypes_.end() ? nullptr : it->second;
    }

    void setResolvedSymbol(const void* node, SymID sym) { resolvedSymbols_[node] = sym; }
    [[nodiscard]] const SymID* resolvedSymbolOf(const void* node) const
    {
        auto it = resolvedSymbols_.find(node);
        return it == resolvedSymbols_.end() ? nullptr : &it->second;
    }

    void setConstantValue(const void* node, ConstantValue value) { constants_[node] = value; }
    [[nodiscard]] const ConstantValue* constantValueOf(const void* node) const
    {
        auto it = constants_.find(node);
        return it == constants_.end() ? nullptr : &it->second;
    }

    void setValueCategory(const void* node, ValueCategory category)
    {
        valueCategories_[node] = category;
    }
    [[nodiscard]] const ValueCategory* valueCategoryOf(const void* node) const
    {
        auto it = valueCategories_.find(node);
        return it == valueCategories_.end() ? nullptr : &it->second;
    }

private:
    std::unordered_map<const void*, const types::Type*> exprTypes_;
    std::unordered_map<const void*, SymID> resolvedSymbols_;
    std::unordered_map<const void*, ConstantValue> constants_;
    std::unordered_map<const void*, ValueCategory> valueCategories_;
};

} // namespace maml
