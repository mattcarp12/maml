const std = @import("std");
const alloc = @import("alloc.zig");

// -----------------------------------------------------------------------------
// String Runtime
// -----------------------------------------------------------------------------

pub export fn maml_str_hash(ptr: [*]const u8, len: u32) u32 {
    if (len == 0) return 0;
    var hash: u32 = 5381;
    var i: u32 = 0;
    while (i < len) : (i += 1) {
        hash = (hash *% 33) +% @as(u32, ptr[i]);
    }
    return hash;
}

pub export fn maml_str_eq(a_ptr: [*]const u8, a_len: u32, b_ptr: [*]const u8, b_len: u32) i32 {
    if (a_len != b_len) return 0;
    if (a_len == 0) return 1;
    const a_bytes = a_ptr[0..a_len];
    const b_bytes = b_ptr[0..b_len];
    return if (std.mem.eql(u8, a_bytes, b_bytes)) 1 else 0;
}

// maml_str_clone will also need a similar update if you use it in the MIR!
pub export fn maml_str_clone(ptr: [*]const u8, len: u32) *anyopaque {
    const new_raw = alloc.mi_malloc(len) orelse @panic("OOM in maml_str_clone");
    const src = ptr[0..len];
    const dst = @as([*]u8, @ptrCast(new_raw))[0..len];
    @memcpy(dst, src);
    return new_raw;
}
