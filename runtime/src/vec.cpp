#include "mamlrt_abi.h"
#include <cstring>
#include <cstdlib>
#include <cstdint>

// -----------------------------------------------------------------------------
// Vector Runtime
// -----------------------------------------------------------------------------

void maml_vec_push(Vector* header, void* item_ptr) {
    if (header->len == header->cap) {
        uint32_t new_cap = (header->cap == 0) ? 8 : header->cap * 2;
        size_t new_size = static_cast<size_t>(new_cap) * header->elem_size;
        
        void* new_buffer = nullptr;
        if (header->cap == 0) {
            new_buffer = maml_alloc(new_size);
        } else {
            new_buffer = maml_realloc(header->buffer, new_size);
        }

        if (new_buffer == nullptr) {
            std::abort(); // OOM
        }
        
        header->buffer = new_buffer;
        header->cap = new_cap;
    }

    // Calculate the destination offset in bytes
    auto* buffer_bytes = static_cast<unsigned char*>(header->buffer);
    size_t offset = static_cast<size_t>(header->len) * header->elem_size;
    
    // Copy item bytes into position
    std::memcpy(buffer_bytes + offset, item_ptr, header->elem_size);
    header->len += 1;
}

void maml_vec_set(Vector* header, uint32_t index, void* item_ptr) {
    if (index >= header->len) {
        std::abort(); // Vector index out of bounds
    }
    
    auto* buffer_bytes = static_cast<unsigned char*>(header->buffer);
    size_t offset = static_cast<size_t>(index) * header->elem_size;
    
    std::memcpy(buffer_bytes + offset, item_ptr, header->elem_size);
}

void* maml_vec_get(Vector* header, uint32_t index) {
    if (index >= header->len) {
        std::abort(); // Vector index out of bounds
    }
    
    auto* buffer_bytes = static_cast<unsigned char*>(header->buffer);
    size_t offset = static_cast<size_t>(index) * header->elem_size;
    
    // Return a void pointer to the start of the element
    return buffer_bytes + offset;
}

uint32_t maml_vec_len(Vector* header) {
    return header->len;
}

Vector maml_vec_clone(Vector* old_header) {
    Vector new_header{};
    new_header.cap = old_header->cap;
    new_header.len = old_header->len;
    new_header.elem_size = old_header->elem_size;
    new_header.buffer = nullptr;

    if (old_header->cap > 0) {
        size_t total_bytes = static_cast<size_t>(old_header->cap) * old_header->elem_size;
        void* new_buf = maml_alloc(total_bytes);
        if (new_buf == nullptr) {
            std::abort(); // OOM
        }
        
        std::memcpy(new_buf, old_header->buffer, total_bytes);
        new_header.buffer = new_buf;
    }

    // Return by value (RVO will optimize this out)
    return new_header; 
}

void maml_vec_free(Vector* header) {
    if (header->cap > 0 && header->buffer != nullptr) {
        maml_free(header->buffer);
    }
}