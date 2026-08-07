#pragma once
#include <string_view>

namespace maml::intrinsics {

// Map operations
inline constexpr std::string_view MAP_PUT = "__builtin_map_put";
inline constexpr std::string_view MAP_GET = "__builtin_map_get";
inline constexpr std::string_view MAP_DELETE = "__builtin_map_delete";

// String operations
inline constexpr std::string_view STR_EQ = "__builtin_str_eq";

} // namespace maml::intrinsics