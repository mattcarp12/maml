// Import sub-modules
pub const alloc = @import("alloc.zig");
pub const vec = @import("vec.zig");
pub const map = @import("map.zig");
pub const coro = @import("coro.zig");
pub const string = @import("string.zig");
pub const io = @import("io.zig");

// Keep global runtime init here
pub export fn maml_runtime_init() void {
    coro.maml_coro_runtime_init();
}

// Force the compiler to evaluate the ABI assertions at comptime
comptime {
    _ = @import("abi_assertions.zig");
}
