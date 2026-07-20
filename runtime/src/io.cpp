#include "mamlrt_abi.h"
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>

// -----------------------------------------------------------------------------
// I/O Runtime
// -----------------------------------------------------------------------------

void maml_print(String* msg) {
    if (msg->len > 0 && msg->ptr != nullptr) {
        write(1, msg->ptr, msg->len);
    }
    write(1, "\n", 1);
}

int32_t maml_file_open(String* filename) {
    if (filename->len == 0 || filename->ptr == nullptr) {
        return -1;
    }

    // POSIX open() requires a null-terminated C-string. 
    // MAML Strings are slices (ptr + len) and might not be null-terminated.
    // We manually allocate a buffer to safely guarantee null-termination.
    char* path = (char*)maml_alloc(filename->len + 1);
    if (path == nullptr) {
        abort(); // OOM
    }
    
    memcpy(path, filename->ptr, filename->len);
    path[filename->len] = '\0';
    int fd = open(path, O_RDWR | O_CREAT, 0666);
    maml_free(path);
    return static_cast<int32_t>(fd);
}

void maml_file_close(int32_t fd) {
    if (fd < 0) return;
    close(fd);
}

String maml_file_read_str(int32_t fd) {
    size_t cap = 4096;
    size_t len = 0;
    char* buf = (char*)maml_alloc(cap);
    if (!buf) abort();
    char chunk[4096];
    while (true) {
        ssize_t n = read(fd, chunk, sizeof(chunk));
        if (n <= 0) break;
        
        if (len + n > cap) {
            cap *= 2;
            buf = (char*)maml_realloc(buf, cap);
            if (!buf) abort();
        }
        memcpy(buf + len, chunk, n);
        len += n;
    }
    
    if (len == 0) {
        maml_free(buf);
        return {nullptr, 0, false};
    }
    
    // shrink to fit
    if (len < cap) {
        buf = (char*)maml_realloc(buf, len);
    }
    return {buf, static_cast<uint32_t>(len), true};
}

void maml_file_write_str(int32_t fd, String* msg) {
    if (fd < 0 || msg->len == 0 || msg->ptr == nullptr) return;
    const char* buf = static_cast<const char*>(msg->ptr);
    size_t remaining = msg->len;
    while (remaining > 0) {
        ssize_t written = write(fd, buf, remaining);
        if (written < 0) {
            break;
        }
        buf += written;
        remaining -= written;
    }
}