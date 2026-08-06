#pragma once
#include <cstdint>
#include <deque>
#include <string>
#include <string_view>
#include <unordered_map>

namespace maml {

using SymID = int32_t;
constexpr SymID NoSymbol = -1;

class SymbolTable {
public:
    SymbolTable() = default;

    SymID intern(std::string_view s)
    {
        if (s.empty())
            return NoSymbol;

        if (auto it = idx.find(s); it != idx.end()) {
            return it->second;
        }

        SymID id = static_cast<SymID>(syms.size());

        // Emplace back into deque ensures string_view pointers remain valid
        syms.emplace_back(s);
        idx[syms.back()] = id;

        return id;
    }

    std::string_view resolve(SymID id) const
    {
        if (id == NoSymbol || id < 0 || id >= static_cast<SymID>(syms.size())) {
            return "";
        }
        return syms[id];
    }

private:
    std::deque<std::string> syms;
    std::unordered_map<std::string_view, SymID> idx;
};

} // namespace maml