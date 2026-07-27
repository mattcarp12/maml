// arena.h
#pragma once
#include <cstdint>
#include <memory>
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

        // Construct the object in the allocated memory
        return new (result) T(std::forward<Args>(args)...);
    }

private:
    void allocateChunk()
    {
        chunks_.emplace_back(std::make_unique<char[]>(chunkSize_));
        ptr_ = chunks_.back().get();
        end_ = ptr_ + chunkSize_;
    }

    size_t chunkSize_;
    char* ptr_ = nullptr;
    char* end_ = nullptr;
    std::vector<std::unique_ptr<char[]>> chunks_;
};

} // namespace maml