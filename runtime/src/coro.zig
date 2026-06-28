const alloc = @import("alloc.zig");
const std = @import("std");

// -----------------------------------------------------------------------------
// Async Executor Runtime
// -----------------------------------------------------------------------------

// These helpers will be generated dynamically by our LLVM Codegen backend!
pub extern "C" fn maml_coro_resume_helper(hdl: ?*anyopaque) void;
pub extern "C" fn maml_coro_done_helper(hdl: ?*anyopaque) bool;
pub extern "C" fn maml_coro_destroy_helper(hdl: ?*anyopaque) void;

const TaskNode = struct {
    hdl: ?*anyopaque,
    next: ?*TaskNode,
};

var run_queue_head: ?*TaskNode = null;
var run_queue_tail: ?*TaskNode = null;

// Registry for tasks waiting on other tasks
var waker_registry: std.AutoHashMap(?*anyopaque, ?*anyopaque) = undefined;
// Registry for tasks that have been dropped without being awaited
var detached_registry: std.AutoHashMap(?*anyopaque, void) = undefined;

pub export fn maml_coro_runtime_init() void {
    const page_alloc = std.heap.page_allocator; // Use your actual allocator here
    waker_registry = std.AutoHashMap(?*anyopaque, ?*anyopaque).init(page_alloc);
    detached_registry = std.AutoHashMap(?*anyopaque, void).init(page_alloc);
}

pub export fn maml_task_await(target_task: ?*anyopaque, waiting_task: ?*anyopaque) void {
    if (target_task == null or waiting_task == null) return;
    // 1. FAST PATH: The target task finished before we reached the await!
    // Do not suspend. Put ourselves right back in the ready queue.
    if (maml_coro_done_helper(target_task)) {
        maml_spawn_task(waiting_task);
        return;
    }

    // 2. SLOW PATH: Target is still running. Register for wakeup.
    waker_registry.put(target_task, waiting_task) catch @panic("OOM in waker registry");
}

pub export fn maml_spawn_task(hdl: ?*anyopaque) void {
    if (hdl == null) return;
    const node_ptr = alloc.mi_malloc(@sizeOf(TaskNode)) orelse @panic("OOM in task spawn");
    var node = @as(*TaskNode, @ptrCast(@alignCast(node_ptr)));
    node.hdl = hdl;
    node.next = null;
    if (run_queue_tail) |tail| {
        tail.next = node;
        run_queue_tail = node;
    } else {
        run_queue_head = node;
        run_queue_tail = node;
    }
}

pub export fn maml_run_executor(root_task: ?*anyopaque) ?*anyopaque {
    while (!maml_coro_done_helper(root_task)) {
        if (run_queue_head == null) {
            // FIX: If the queue is empty, the executor should sleep
            // briefly to allow the OS to schedule other threads/events.
            // In a real system, you'd use a conditional variable here.
            std.Thread.yield() catch {};

            // Re-check: Did any new tasks get spawned by an interrupt?
            if (run_queue_head == null) {
                continue;
            }
        }

        const node = run_queue_head.?;
        run_queue_head = node.next;
        if (run_queue_head == null) run_queue_tail = null;

        const hdl = node.hdl;
        alloc.mi_free(node);

        if (hdl) |handle| {

            // Only resume if the task is actually suspended.
            // If it's already done, just skip it.
            if (!maml_coro_done_helper(handle)) {
                maml_coro_resume_helper(handle);
            }

            // After resumption, if the task is now done,
            // check if anyone was waiting for it.
            if (maml_coro_done_helper(handle)) {
                if (waker_registry.fetchRemove(handle)) |kv| {
                    maml_spawn_task(kv.value);
                }
                if (detached_registry.fetchRemove(handle)) |_| {
                    maml_coro_destroy_helper(handle);
                }
            }
        }
    }
    return root_task;
}

// Emitted by the ARC injector when a Future<T> variable dies out of scope
pub export fn maml_task_release(handle: ?*anyopaque) void {
    if (handle == null) return;

    if (maml_coro_done_helper(handle)) {
        // Task is already finished. Safe to destroy immediately.
        maml_coro_destroy_helper(handle);
    } else {
        // Task is still running in the background. Mark it as detached so
        // the executor knows to clean it up when it finally finishes.
        detached_registry.put(handle, {}) catch @panic("OOM in detached registry");
    }
}

pub export fn maml_task_get_result(target_task: ?*anyopaque) void { // Or whatever return type
    maml_coro_destroy_helper(target_task);
}

pub export fn maml_yield_now(current_coro: ?*anyopaque) void {
    maml_spawn_task(current_coro);
}
