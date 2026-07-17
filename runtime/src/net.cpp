#include "mamlrt_abi.h"
#include <cstring>
#include <cstdlib>
#include <fcntl.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>

// -----------------------------------------------------------------------------
// Networking Runtime
// -----------------------------------------------------------------------------

int32_t maml_socket(int32_t domain, int32_t type, int32_t protocol) {
    int fd = socket(domain, type, protocol);
    return static_cast<int32_t>(fd);
}

int32_t maml_bind(int32_t fd, int32_t port) {
    if (fd < 0) return -1;

    // Prevent "Address already in use" when restarting quickly
    int opt = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    struct sockaddr_in addr;
    std::memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons(static_cast<uint16_t>(port));
    addr.sin_addr.s_addr = INADDR_ANY;  // 0.0.0.0

    int result = bind(fd, reinterpret_cast<struct sockaddr*>(&addr), sizeof(addr));
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
    int client_fd = accept(fd, reinterpret_cast<struct sockaddr*>(&client_addr), &addr_len);
    return static_cast<int32_t>(client_fd);
}

// Bounded read for sockets. Returns {nullptr, 0, false} on EOF or error.
String maml_socket_read(int32_t fd, uint32_t max_bytes) {
    if (fd < 0 || max_bytes == 0) {
        return {nullptr, 0, false};
    }

    // Cap to avoid accidental huge allocations
    if (max_bytes > 65536) max_bytes = 65536;

    void* buf = maml_alloc(max_bytes);
    if (buf == nullptr) std::abort();

    ssize_t n = read(fd, buf, max_bytes);
    if (n <= 0) {
        maml_free(buf);
        return {nullptr, 0, false};
    }

    // Shrink to fit if we got less than requested
    if (static_cast<uint32_t>(n) < max_bytes) {
        void* shrunk = maml_alloc(n);
        if (shrunk) {
            std::memcpy(shrunk, buf, n);
            maml_free(buf);
            buf = shrunk;
        }
    }

    return {buf, static_cast<uint32_t>(n), true};
}