// core/include/capability.h
#pragma once
#include <cstdint>
#include <string_view>

namespace maml {

enum class Capability : uint8_t { Mut, Own, Ro };

inline Capability parseCapability(std::string_view literal) {
    if (literal == "mut") return Capability::Mut;
    if (literal == "own") return Capability::Own;
    if (literal == "ro")  return Capability::Ro;
    return Capability::Ro;
}

} // namespace maml