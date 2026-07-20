#include "mamlrt_abi.h"
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <cstdio>

// -----------------------------------------------------------------------------
// Networking Runtime 
// -----------------------------------------------------------------------------

int32_t maml_socket(int32_t domain, int32_t type, int32_t protocol) {
    int fd = socket(domain, type, protocol);
    return (int32_t)fd;
}

int32_t maml_bind(int32_t fd, int32_t port) {
    if (fd < 0) return -1;

    // Prevent "Address already in use" when restarting quickly
    int opt = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    addr.sin_addr.s_addr = INADDR_ANY;  // 0.0.0.0

    int result = bind(fd, (struct sockaddr*)&addr, sizeof(addr));
    return result;
}

int32_t maml_listen(int32_t fd, int32_t backlog) {
    if (fd < 0) return -1;
    return listen(fd, backlog);
}

int32_t maml_accept(int32_t fd) {
    if (fd < 0) return -1;
    struct sockaddr_in client_addr;
    socklen_t addr_len = sizeof(client_addr);
    
    int client_fd = accept(fd, (struct sockaddr*)&client_addr, &addr_len);
    return (int32_t)client_fd;
}

// Bounded read for sockets. Returns {nullptr, 0, false} on EOF or error.
String maml_socket_read(int32_t fd, uint32_t max_bytes) {
    if (fd < 0 || max_bytes == 0) {
        return {nullptr, 0, false};
    }

    // Cap to avoid accidental huge allocations
    if (max_bytes > 65536) max_bytes = 65536;

    void* buf = maml_alloc(max_bytes);
    if (buf == nullptr) abort();

    ssize_t n = read(fd, buf, max_bytes);
    if (n <= 0) {
        maml_free(buf);
        return {nullptr, 0, false};
    }

    // Shrink to fit if we got less than requested
    if ((uint32_t)n < max_bytes) {
        void* shrunk = maml_alloc(n);
        if (shrunk) {
            memcpy(shrunk, buf, n);
            maml_free(buf);
            buf = shrunk;
        }
    }

    return {buf, (uint32_t)n, true};
}

int32_t maml_socket_write(int32_t fd, String* msg) {
    if (fd < 0 || msg == nullptr || msg->len == 0 || msg->ptr == nullptr) {
        return 0;
    }

    const char* buf = static_cast<const char*>(msg->ptr);
    size_t remaining = msg->len;
    size_t total_written = 0;

    while (remaining > 0) {
        ssize_t written = write(fd, buf, remaining);
        if (written < 0) {
            // Return whatever we managed to write so far, or -1 on hard failure
            return total_written > 0 ? static_cast<int32_t>(total_written) : -1;
        }
        buf += written;
        remaining -= written;
        total_written += written;
    }
    
    return static_cast<int32_t>(total_written);
}

int32_t maml_connect(int32_t fd, String* host, int64_t port) {
    if (fd < 0 || host == nullptr || host->len == 0 || host->ptr == nullptr) {
        return -1;
    }

    char* host_cstr = (char*)maml_alloc(host->len + 1);
    if (host_cstr == nullptr) abort(); 
    
    memcpy(host_cstr, host->ptr, host->len);
    host_cstr[host->len] = '\0';

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons(static_cast<uint16_t>(port));
    if (inet_pton(AF_INET, host_cstr, &addr.sin_addr) <= 0) {
        maml_free(host_cstr);
        return -1;
    }
    maml_free(host_cstr); // Now it is safe to free

    int result = connect(fd, (struct sockaddr*)&addr, sizeof(addr));
    return static_cast<int32_t>(result);
}