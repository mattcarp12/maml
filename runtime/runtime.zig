const std = @import("std");

// 1. Declare the mimalloc functions we want to use from C.
// (We will link the actual mimalloc static library when we compile).
extern "C" fn mi_malloc(size: usize) ?*anyopaque;
extern "C" fn mi_free(p: ?*anyopaque) void;

// 2. Export our MAML allocation wrapper.
// The `export` keyword tells Zig to use the C ABI so LLVM can call it.
export fn maml_alloc(size: usize) ?*anyopaque {
    // For now, it just passes through to mimalloc.
    // Later, we will inject our ARC header allocation here!
    return mi_malloc(size);
}

// 3. Export our MAML free wrapper.
export fn maml_free(ptr: ?*anyopaque) void {
    if (ptr != null) {
        mi_free(ptr);
    }
}

// 4. A quick test function to print from the runtime to ensure linkage works!
export fn maml_runtime_init() void {
    std.debug.print("[MAML Runtime] Initialized with mimalloc.\n", .{});
}
