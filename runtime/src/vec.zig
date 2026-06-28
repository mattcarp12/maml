const alloc = @import("alloc.zig");
const std = @import("std");

// -----------------------------------------------------------------------------
// Vec runtime
// -----------------------------------------------------------------------------

const VecHeader = extern struct {
    buffer: ?*anyopaque,
    cap: u32,
    len: u32,
    elem_size: u32,
    _pad: u32,
};

pub export fn maml_vec_grow(
    raw_ptr: ?*anyopaque,
    cap_ptr: *u32,
    item_size: u32,
) ?*anyopaque {
    const old_cap = cap_ptr.*;
    const new_cap: u32 = if (old_cap == 0) 8 else old_cap * 2;
    const new_size = @as(usize, new_cap) * @as(usize, item_size);
    const new_raw = alloc.mi_realloc(raw_ptr, new_size) orelse @panic("maml_vec_grow: out of memory");
    cap_ptr.* = new_cap;
    return new_raw;
}

pub export fn maml_vec_create(elem_size: u32) ?*anyopaque {
    // Just allocate the shell using standard malloc now
    const raw_ptr = alloc.mi_malloc(@sizeOf(VecHeader)) orelse @panic("OOM in maml_vec_create");
    var header = @as(*VecHeader, @ptrCast(@alignCast(raw_ptr)));

    header.buffer = null;
    header.cap = 0;
    header.len = 0;
    header.elem_size = elem_size;
    header._pad = 0;

    return raw_ptr;
}

pub export fn maml_vec_push(vec_ptr: ?*anyopaque, item_ptr: ?*anyopaque) void {
    if (vec_ptr == null or item_ptr == null) return;
    var header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));

    // Grow buffer if needed
    if (header.len == header.cap) {
        const new_cap = if (header.cap == 0) 8 else header.cap * 2;
        const new_size = new_cap * header.elem_size;
        const new_buffer = if (header.buffer) |buf|
            alloc.mi_realloc(buf, new_size)
        else
            alloc.mi_malloc(new_size);

        header.buffer = new_buffer orelse @panic("OOM in vec_push");
        header.cap = new_cap;
    }

    // Copy item bytes
    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = header.len * header.elem_size;
    @memcpy(buffer_bytes[offset .. offset + header.elem_size], @as([*]u8, @ptrCast(item_ptr.?))[0..header.elem_size]);

    header.len += 1;
}

pub export fn maml_vec_set(vec_ptr: ?*anyopaque, index: u32, item_ptr: ?*anyopaque) void {
    if (vec_ptr == null or item_ptr == null) return;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));
    if (index >= header.len) @panic("Vector index out of bounds");
    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = index * header.elem_size;
    const target_slice = buffer_bytes[offset .. offset + header.elem_size];

    // Copy new item bytes
    @memcpy(target_slice, @as([*]u8, @ptrCast(item_ptr.?))[0..header.elem_size]);
}

pub export fn maml_vec_get(vec_ptr: ?*anyopaque, index: u32) ?*anyopaque {
    if (vec_ptr == null) return null;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));

    if (index >= header.len) @panic("Vector index out of bounds");

    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = index * header.elem_size;
    return @ptrCast(buffer_bytes + offset);
}

pub export fn maml_vec_len(vec_ptr: ?*anyopaque) u32 {
    if (vec_ptr == null) return 0;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));
    return header.len;
}

pub export fn maml_vec_clone(vec_ptr: ?*anyopaque, elem_size: usize) ?*anyopaque {
    _ = elem_size; // Kept to match ABI schema, though we can read it from the header
    if (vec_ptr == null) return null;

    const old_header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));

    // 1. Allocate the new shell
    const raw_new_ptr = alloc.mi_malloc(@sizeOf(VecHeader)) orelse @panic("OOM in maml_vec_clone");
    var new_header = @as(*VecHeader, @ptrCast(@alignCast(raw_new_ptr)));

    new_header.cap = old_header.cap;
    new_header.len = old_header.len;
    new_header.elem_size = old_header.elem_size;
    new_header._pad = 0;
    new_header.buffer = null;

    // 2. Clone the backing buffer if it exists
    if (old_header.buffer) |old_buf| {
        // Allocate identical capacity
        const total_bytes = old_header.cap * old_header.elem_size;
        const new_buf = alloc.mi_malloc(total_bytes) orelse @panic("OOM in buffer clone");

        // We only need to memcpy the initialized items, not the whole capacity
        const active_bytes = old_header.len * old_header.elem_size;
        if (active_bytes > 0) {
            const src = @as([*]u8, @ptrCast(old_buf))[0..active_bytes];
            const dst = @as([*]u8, @ptrCast(new_buf))[0..active_bytes];
            @memcpy(dst, src);
        }

        new_header.buffer = new_buf;
    }

    return raw_new_ptr;
}
