#include "mamlrt_abi.h"
#include <string_view>
#include <cstring>
#include <cstdlib>

// Helper to safely create a string_view from a raw pointer and length.
// Passing a nullptr with size > 0 to std::string_view is Undefined Behavior in C++.
static inline std::string_view make_view(const char* ptr, uint32_t len) {
    if (len == 0 || ptr == nullptr) {
        return {};
    }
    return {ptr, len};
}

// -----------------------------------------------------------------------------
// String Runtime
// -----------------------------------------------------------------------------

uint32_t maml_str_hash(const char* ptr, uint32_t len) {
    if (len == 0 || ptr == nullptr) return 0;
    
    uint32_t hash = 5381;
    
    // Using string_view allows for safe, idiomatic range-based for loops
    for (char c : make_view(ptr, len)) {
        // C++ handles standard unsigned integer wrapping natively
        hash = (hash * 33) + static_cast<uint32_t>(c);
    }
    return hash;
}

int32_t maml_str_eq(const char* a_ptr, uint32_t a_len, const char* b_ptr, uint32_t b_len) {
    // std::string_view's equality operator is highly optimized.
    // It automatically checks length first, then delegates to memcmp natively.
    bool is_eq = make_view(a_ptr, a_len) == make_view(b_ptr, b_len);
    return is_eq ? 1 : 0;
}

void* maml_str_clone(const char* ptr, uint32_t len) {
    if (len == 0 || ptr == nullptr) {
        return nullptr;
    }
    
    void* new_raw = maml_alloc(len);
    if (new_raw == nullptr) {
        // The closest equivalent to Zig's @panic for unrecoverable errors like OOM
        std::abort(); 
    }
    
    std::memcpy(new_raw, ptr, len);
    return new_raw;
}

uint32_t maml_str_len(String str) {
    return str.len;
}