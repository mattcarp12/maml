const std = @import("std");
const alloc = @import("alloc.zig");

// -----------------------------------------------------------------------------
// String Runtime
// -----------------------------------------------------------------------------

pub export fn maml_str_hash(ptr: ?*anyopaque, len: u32) u32 {
    if (ptr == null or len == 0) return 0;
    const bytes = @as([*]const u8, @ptrCast(ptr.?));

    var hash: u32 = 5381;
    var i: u32 = 0;
    while (i < len) : (i += 1) {
        hash = (hash *% 33) +% @as(u32, bytes[i]);
    }
    return hash;
}

pub export fn maml_str_eq(a_ptr: ?*anyopaque, a_len: u32, b_ptr: ?*anyopaque, b_len: u32) i32 {
    if (a_len != b_len) return 0;
    if (a_len == 0) return 1;
    if (a_ptr == null or b_ptr == null) return 0;

    const a_bytes = @as([*]const u8, @ptrCast(a_ptr.?))[0..a_len];
    const b_bytes = @as([*]const u8, @ptrCast(b_ptr.?))[0..b_len];
    return if (std.mem.eql(u8, a_bytes, b_bytes)) 1 else 0;
}

pub export fn maml_str_clone(ptr: ?*anyopaque, len: u32) ?*anyopaque {
    if (ptr == null or len == 0) return null;

    // Allocate exact capacity for the string
    const new_raw = alloc.mi_malloc(len) orelse @panic("OOM in maml_str_clone");

    // Slice and copy
    const src = @as([*]const u8, @ptrCast(ptr.?))[0..len];
    const dst = @as([*]u8, @ptrCast(new_raw))[0..len];
    @memcpy(dst, src);

    return new_raw;
}
