#ifndef MAML_RUNTIME_CONSTANTS_H
#define MAML_RUNTIME_CONSTANTS_H

namespace maml {
namespace rt {

// --- Memory & ARC Constants ---
constexpr const char* ALLOC = "maml_alloc";
constexpr const char* FREE = "maml_free";
constexpr const char* RELEASE = "maml_release";
constexpr const char* RETAIN = "maml_retain";

// --- Container & String Constants ---
constexpr const char* MAP_CREATE = "maml_map_create";
constexpr const char* MAP_DELETE = "maml_map_delete";
constexpr const char* MAP_GET = "maml_map_get";
constexpr const char* MAP_LEN = "maml_map_len";
constexpr const char* MAP_PUT = "maml_map_put";
constexpr const char* STR_EQ = "maml_str_eq";
constexpr const char* STR_HASH = "maml_str_hash";
constexpr const char* VEC_CREATE = "maml_vec_create";
constexpr const char* VEC_GET = "maml_vec_get";
constexpr const char* VEC_GROW = "maml_vec_grow";
constexpr const char* VEC_LEN = "maml_vec_len";
constexpr const char* VEC_PUSH = "maml_vec_push";
constexpr const char* VEC_SET = "maml_vec_set";

// --- Coroutine Constants ---
constexpr const char* CORO_DESTROY_HELPER = "maml_coro_destroy_helper";
constexpr const char* CORO_DONE_HELPER = "maml_coro_done_helper";
constexpr const char* CORO_RESUME_HELPER = "maml_coro_resume_helper";

// --- Miscellaneous Constants ---
constexpr const char* RUN_EXECUTOR = "maml_run_executor";
constexpr const char* SPAWN_TASK = "maml_spawn_task";
constexpr const char* PUTS = "puts";

} // namespace rt
} // namespace maml

#endif // MAML_RUNTIME_CONSTANTS_H
