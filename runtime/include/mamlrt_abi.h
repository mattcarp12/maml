#pragma once

#include <cstddef>
#include <cstdint>

extern "C" {

// ============================================================================
// Data Layouts (C-ABI Compatible)
// ============================================================================

struct String {
    void* ptr;
    uint32_t len;
    bool is_owned;
};

struct Vector {
    void* buffer;
    uint32_t cap;
    uint32_t len;
    uint32_t elem_size;
};

struct View {
    void* data_ptr;
    uint32_t len;
};

struct Map {
    void* entries;
    uint32_t count;
    uint32_t tombstone_count;
    uint32_t cap;
    uint32_t val_size;
    bool is_string_key;
};

// ============================================================================
// Compile-Time ABI Safety Assertions
// ============================================================================
// 1. Verify Struct Sizes and Alignments
static_assert(sizeof(String) == 16, "ABI Mismatch: String must be 16 bytes");
static_assert(alignof(String) == 8, "ABI Mismatch: String align must be 8");

// 2. Verify Offsets for String
static_assert(offsetof(String, ptr) == 0, "ABI Mismatch: String.ptr offset must be 0");
static_assert(offsetof(String, len) == 8, "ABI Mismatch: String.len offset must be 8");
static_assert(offsetof(String, is_owned) == 12, "ABI Mismatch: String.is_owned offset must be 12");

static_assert(sizeof(Vector) == 24, "ABI Mismatch: Vector must be 24 bytes");
static_assert(alignof(Vector) == 8, "ABI Mismatch: Vector align must be 8");

// 2. Verify Offsets for Vector
static_assert(offsetof(Vector, buffer) == 0, "ABI Mismatch: Vector.buffer offset must be 0");
static_assert(offsetof(Vector, cap) == 8, "ABI Mismatch: Vector.cap offset must be 8");
static_assert(offsetof(Vector, len) == 12, "ABI Mismatch: Vector.len offset must be 12");
static_assert(
    offsetof(Vector, elem_size) == 16, "ABI Mismatch: Vector.elem_size offset must be 16");

static_assert(sizeof(View) == 16, "ABI Mismatch: View must be 16 bytes");
static_assert(alignof(View) == 8, "ABI Mismatch: View align must be 8");

// 2. Verify Offsets for View
static_assert(offsetof(View, data_ptr) == 0, "ABI Mismatch: View.data_ptr offset must be 0");
static_assert(offsetof(View, len) == 8, "ABI Mismatch: View.len offset must be 8");

static_assert(sizeof(Map) == 32, "ABI Mismatch: Map must be 32 bytes");
static_assert(alignof(Map) == 8, "ABI Mismatch: Map align must be 8");

// 2. Verify Offsets for Map
static_assert(offsetof(Map, entries) == 0, "ABI Mismatch: Map.entries offset must be 0");
static_assert(offsetof(Map, count) == 8, "ABI Mismatch: Map.count offset must be 8");
static_assert(
    offsetof(Map, tombstone_count) == 12, "ABI Mismatch: Map.tombstone_count offset must be 12");
static_assert(offsetof(Map, cap) == 16, "ABI Mismatch: Map.cap offset must be 16");
static_assert(offsetof(Map, val_size) == 20, "ABI Mismatch: Map.val_size offset must be 20");
static_assert(
    offsetof(Map, is_string_key) == 24, "ABI Mismatch: Map.is_string_key offset must be 24");

// ============================================================================
// Runtime Export Functions
// ============================================================================

void* maml_alloc(size_t size);
void maml_free(void* ptr);
void* maml_realloc(void* ptr, size_t size);
void* maml_calloc(size_t nmemb, size_t size);
void maml_vec_push(Vector* vec, void* element);
void* maml_vec_get(Vector* vec, uint32_t index);
void maml_vec_set(Vector* vec, uint32_t index, void* element);
uint32_t maml_vec_len(Vector* vec);
Vector maml_vec_clone(Vector* vec);
void maml_vec_free(Vector* vec);
void maml_map_put(Map* m, uint32_t key_hash, void* key_ptr, uint32_t key_len, void* val_ptr);
void* maml_map_get(Map* m, uint32_t key_hash, void* key_ptr, uint32_t key_len);
void maml_map_delete(Map* m, uint32_t key_hash, void* key_ptr, uint32_t key_len);
uint32_t maml_map_len(Map* m);
Map maml_map_clone(Map* m);
void maml_map_free(Map* m);
uint32_t maml_str_hash(String* str);
int32_t maml_str_eq(String* a_str, String* b_str);
void* maml_str_clone(const char* str_ptr, uint32_t len);
uint32_t maml_str_len(String* str);
void maml_str_free(String* str);
String maml_str_concat(String* str_a, String* str_b);
void maml_coro_runtime_init();
void maml_coro_resume_helper(void* task);
bool maml_coro_done_helper(void* task);
void maml_coro_destroy_helper(void* task);
void maml_spawn_task(void* task);
void* maml_run_executor(void* task);
void maml_task_await(void* current_task, void* target_task);
void maml_task_release(void* task);
void maml_yield_now(void* task);
void maml_print(String* msg);
int32_t maml_file_open(String* filename);
void maml_file_close(int32_t fd);
String maml_file_read_str(int32_t fd);
void maml_file_write_str(int32_t fd, String* msg);
int32_t maml_socket(int32_t domain, int32_t typ, int32_t protocol);
int32_t maml_bind(int32_t fd, int32_t port);
int32_t maml_listen(int32_t fd, int32_t backlog);
int32_t maml_accept(int32_t fd);
String maml_socket_read(int32_t fd, uint32_t max_bytes);
int32_t maml_socket_write(int32_t fd, String* msg);
int32_t maml_connect(int32_t fd, String* host, int64_t port);

} // extern "C"