#include "mamlrt_abi.h"
#include <unordered_map>
#include <unordered_set>
#include <queue>
#include <thread>

// -----------------------------------------------------------------------------
// Coroutine Runtime State
// -----------------------------------------------------------------------------

// SOTA C++ uses standard containers rather than manual linked lists.
// Note: If you want mimalloc to back these standard containers, 
// linking mimalloc as a shared library or including <mimalloc-new-delete.h> 
// will automatically override the STL's default allocator globally.
static std::queue<void*> run_queue;

// Maps target handle -> waiting handle
static std::unordered_map<void*, void*> waker_registry;

// Tracks detached tasks that are not yet done
static std::unordered_set<void*> detached_registry;

// -----------------------------------------------------------------------------
// Executor Implementation
// -----------------------------------------------------------------------------

void maml_coro_runtime_init() {
    // In C++, static STL containers are zero-initialized automatically.
    // We provide this function simply to clear state if the runtime is restarted.
    run_queue = std::queue<void*>();
    waker_registry.clear();
    detached_registry.clear();
}

void maml_task_await(void* target_task, void* waiting_task) {
    if (maml_coro_done_helper(target_task)) {
        maml_spawn_task(waiting_task);
        return;
    }
    
    // std::unordered_map handles allocation and insertion automatically
    waker_registry[target_task] = waiting_task;
}

void maml_spawn_task(void* hdl) {
    run_queue.push(hdl);
}

void* maml_run_executor(void* root_task) {
    while (!maml_coro_done_helper(root_task)) {
        if (run_queue.empty()) {
            // Surrenders the CPU time slice to the OS, identical to std.Thread.yield()
            std::this_thread::yield();
            continue;
        }

        void* hdl = run_queue.front();
        run_queue.pop();

        // Only resume if still suspended
        if (!maml_coro_done_helper(hdl)) {
            maml_coro_resume_helper(hdl);
        }

        // After resumption, if it's done, wake waiters and handle detachment
        if (maml_coro_done_helper(hdl)) {
            auto waker_it = waker_registry.find(hdl);
            if (waker_it != waker_registry.end()) {
                maml_spawn_task(waker_it->second);
                waker_registry.erase(waker_it);
            }

            auto detached_it = detached_registry.find(hdl);
            if (detached_it != detached_registry.end()) {
                maml_coro_destroy_helper(hdl);
                detached_registry.erase(detached_it);
            }
        }
    }
    return root_task;
}

void maml_task_release(void* handle) {
    if (maml_coro_done_helper(handle)) {
        maml_coro_destroy_helper(handle);
    } else {
        // std::unordered_set acts exactly like a map with `void` values
        detached_registry.insert(handle);
    }
}

void maml_yield_now(void* current_coro) {
    maml_spawn_task(current_coro);
}