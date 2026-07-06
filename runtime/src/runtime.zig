// Import sub-modules
pub const alloc = @import("alloc.zig");
pub const vec = @import("vec.zig");
pub const map = @import("map.zig");
pub const coro = @import("coro.zig");
pub const string = @import("string.zig");
pub const io = @import("io.zig");

// Force the compiler to evaluate the ABI assertions at comptime
comptime {
    _ = @import("abi.zig");
}
