#include "mamlrt_abi.h"
#include <string_view>
#include <cstring>
#include <cstdlib>

// Helper to safely create a string_view from a raw pointer and length.
// Passing a nullptr with size > 0 to std::string_view is Undefined Behavior in C++.
static inline std::string_view make_view(void* ptr, uint32_t len) {
    if (len == 0 || ptr == nullptr) {
        return {};
    }
    return {static_cast<char*>(ptr), len};
}

// -----------------------------------------------------------------------------
// String Runtime
// -----------------------------------------------------------------------------

uint32_t maml_str_hash(String* str) {
    if (str->len == 0 || str->ptr == nullptr) return 0;
    
    uint32_t hash = 5381;
    for (char c : make_view(str->ptr, str->len)) {
        hash = (hash * 33) + static_cast<uint32_t>(c);
    }
    return hash;
}

int32_t maml_str_eq(String* a_str, String* b_str) {
    bool is_eq = make_view(a_str->ptr, a_str->len) == make_view(b_str->ptr, b_str->len);
    return is_eq ? 1 : 0;
}

void* maml_str_clone(const char* ptr, uint32_t len) {
    if (len == 0 || ptr == nullptr) {
        return nullptr;
    }
    
    void* new_raw = maml_alloc(len);
    if (new_raw == nullptr) {
        std::abort(); 
    }
    
    std::memcpy(new_raw, ptr, len);
    return new_raw;
}

uint32_t maml_str_len(String* str) {
    return str->len;
}

String maml_str_concat(String* a, String* b) {
    uint32_t total_len = a->len + b->len;
    if (total_len == 0) {
        return {nullptr, 0, false};
    }
    void* buffer = maml_alloc(total_len);
    if (buffer == nullptr) {
        std::abort(); // Handle OOM
    }
    char* dest = static_cast<char*>(buffer);
    if (a->ptr != nullptr && a->len > 0) {
        std::memcpy(dest, a->ptr, a->len);
    }
    if (b->ptr != nullptr && b->len > 0) {
        std::memcpy(dest + a->len, b->ptr, b->len);
    }
    return {buffer, total_len, true};
}

void maml_str_free(String* s) {
    if (s->is_owned) maml_free(s->ptr);
}