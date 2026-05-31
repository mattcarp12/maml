#ifndef MAML_RUNTIME_CONSTANTS_H
#define MAML_RUNTIME_CONSTANTS_H

namespace maml {
namespace rt {

// --- Memory & ARC Constants ---
constexpr const char *ALLOC = "maml_alloc";
constexpr const char *FREE = "maml_free"; 
constexpr const char *RETAIN = "maml_retain";
constexpr const char *RELEASE = "maml_release";

// --- Coroutine Constants ---
constexpr const char *CORO_ID = "__coro_id";
constexpr const char *CORO_HDL = "__coro_hdl";
constexpr const char *CORO_RESUME = "maml_coro_resume_helper";
constexpr const char *CORO_DESTROY = "maml_coro_destroy_helper";
constexpr const char *CORO_DONE = "maml_coro_done_helper";

// --- Async Executor Constants ---
constexpr const char *SPAWN_TASK = "maml_spawn_task";
constexpr const char *RUN_EXECUTOR = "maml_run_executor";

// --- Container Constants ---
constexpr const char *VEC_GROW = "maml_vec_grow";
constexpr const char *MAP_CREATE = "maml_map_create";
constexpr const char *MAP_PUT = "maml_map_put";
constexpr const char *MAP_GET = "maml_map_get";
constexpr const char *STR_HASH = "maml_str_hash";

// --- Standard Built-ins ---
constexpr const char *PUTS = "puts";

} // namespace rt
} // namespace maml

#endif // MAML_RUNTIME_CONSTANTS_H