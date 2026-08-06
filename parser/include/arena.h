#pragma once
#include <cstddef>
#include <cstdint>
#include <memory>
#include <ranges>
#include <type_traits>
#include <utility>
#include <vector>

namespace maml {

class Arena {
public:
    Arena(size_t chunkSize = 64 * 1024)
        : chunkSize_(chunkSize)
    {
        allocateChunk();
    }

    ~Arena()
    {
        // Call destructors for non-trivially destructible objects in reverse order
        for (auto& cleanup : std::ranges::reverse_view(cleanups_)) {
            cleanup.destroy(cleanup.ptr);
        }
    }

    // Disable copying and moving
    Arena(const Arena&) = delete;
    Arena& operator=(const Arena&) = delete;

    template <typename T, typename... Args> T* make(Args&&... args)
    {
        size_t size = sizeof(T);
        size_t alignment = alignof(T);

        // Align the current pointer
        size_t padding = (alignment - (reinterpret_cast<uintptr_t>(ptr_) % alignment)) % alignment;

        if (ptr_ + padding + size > end_) {
            allocateChunk();
            padding = (alignment - (reinterpret_cast<uintptr_t>(ptr_) % alignment)) % alignment;
        }

        ptr_ += padding;
        T* result = reinterpret_cast<T*>(ptr_);
        ptr_ += size;

        // Construct the object in allocated memory
        T* object = new (result) T(std::forward<Args>(args)...);

        // Register destructor cleanup ONLY if T has a non-trivial destructor
        if constexpr (!std::is_trivially_destructible_v<T>) {
            cleanups_.push_back(
                { .ptr = object, .destroy = [](void* p) { static_cast<T*>(p)->~T(); } });
        }

        return object;
    }

private:
    void allocateChunk()
    {
        chunks_.emplace_back(std::make_unique<char[]>(chunkSize_));
        ptr_ = chunks_.back().get();
        end_ = ptr_ + chunkSize_;
    }

    struct Cleanup {
        void* ptr;
        void (*destroy)(void*);
    };

    size_t chunkSize_;
    char* ptr_ = nullptr;
    char* end_ = nullptr;
    std::vector<Cleanup> cleanups_;
    std::vector<std::unique_ptr<char[]>> chunks_;
};

} // namespace maml