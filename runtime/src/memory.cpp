#include "mamlrt_abi.h" 
#include <malloc.h>
#include <cstdlib>

[[nodiscard]] void* maml_alloc(size_t size) {
    return malloc(size);
}

void maml_free(void* ptr) {
    free(ptr);
}

[[nodiscard]] void* maml_realloc(void* ptr, size_t size) {
    return realloc(ptr, size);
}

[[nodiscard]] void* maml_calloc(size_t nmemb, size_t size) {
    return calloc(nmemb, size);
}