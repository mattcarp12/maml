#include "mamlrt_abi.h"
#include <string>
#include <cstring>
#include <cstdlib>

// POSIX headers for raw file descriptor I/O
#include <fcntl.h>
#include <unistd.h>

// -----------------------------------------------------------------------------
// I/O Runtime
// -----------------------------------------------------------------------------


void maml_print(String msg) {
    if (msg.len > 0 && msg.ptr != nullptr) {
        write(1, msg.ptr, msg.len);
    }
    write(1, "\n", 1);
}

int32_t maml_file_open(String filename) {
    if (filename.len == 0 || filename.ptr == nullptr) {
        return -1;
    }

    // POSIX open() requires a null-terminated C-string. 
    // MAML Strings are slices (ptr + len) and might not be null-terminated.
    // We construct a temporary std::string to safely guarantee null-termination.
    std::string path{static_cast<const char*>(filename.ptr), filename.len};

    // Open for read/write, and create if it doesn't exist (matching standard rw behavior)
    int fd = open(path.c_str(), O_RDWR | O_CREAT, 0666);
    return static_cast<int32_t>(fd);
}

void maml_file_close(int32_t fd) {
    if (fd < 0) return;
    close(fd);
}

String maml_file_read_str(int32_t fd) {
    // Use a fixed-size stack buffer + manual realloc instead of std::vector
    size_t cap = 4096;
    size_t len = 0;
    char* buf = (char*)maml_alloc(cap);
    if (!buf) std::abort();
    
    char chunk[4096];
    while (true) {
        ssize_t n = read(fd, chunk, sizeof(chunk));
        if (n <= 0) break;
        
        if (len + n > cap) {
            cap *= 2;
            buf = (char*)maml_realloc(buf, cap);
            if (!buf) std::abort();
        }
        std::memcpy(buf + len, chunk, n);
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

void maml_file_write_str(int32_t fd, String msg) {
    if (fd < 0 || msg.len == 0 || msg.ptr == nullptr) return;
    const char* buf = static_cast<const char*>(msg.ptr);
    size_t remaining = msg.len;
    while (remaining > 0) {
        ssize_t written = write(fd, buf, remaining);
        if (written < 0) {
            break;
        }
        buf += written;
        remaining -= written;
    }
}