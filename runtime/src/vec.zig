const alloc = @import("alloc.zig");
const abi = @import("abi.zig");
const std = @import("std");

// -----------------------------------------------------------------------------
// Vec runtime implementation
// -----------------------------------------------------------------------------

/// Grows a raw backing array buffer.
pub export fn maml_vec_grow(
    raw_ptr: *anyopaque,
    cap_ptr: *u32,
    item_size: u32,
) *anyopaque {
    const old_cap = cap_ptr.*;
    const new_cap: u32 = if (old_cap == 0) 8 else old_cap * 2;
    const new_size = @as(usize, new_cap) * @as(usize, item_size);
    const new_raw = alloc.mi_realloc(raw_ptr, new_size) orelse @panic("maml_vec_grow: out of memory");
    cap_ptr.* = new_cap;
    return new_raw;
}

/// Allocates and initializes a new Vector descriptor shell and its initial buffer.
pub export fn maml_vec_create(elem_size: u32) *abi.Vector {
    const raw_ptr = alloc.mi_malloc(abi.VECTOR_SIZE) orelse @panic("OOM in maml_vec_create");
    var header = @as(*abi.Vector, @ptrCast(@alignCast(raw_ptr)));

    // Allocate an initial buffer with capacity 8 so buffer is never null
    const initial_cap: u32 = 8;
    const initial_bytes = @as(usize, initial_cap) * @as(usize, elem_size);
    const buffer = alloc.mi_malloc(initial_bytes) orelse @panic("OOM in initial vec buffer");

    header.buffer = buffer;
    header.cap = initial_cap;
    header.len = 0;
    header.elem_size = elem_size;

    return header;
}

/// Pushes an item into the vector, growing the backing buffer automatically if capacity is reached.
pub export fn maml_vec_push(header: *abi.Vector, item_ptr: *anyopaque) void {
    if (header.len == header.cap) {
        const new_cap = if (header.cap == 0) 8 else header.cap * 2;
        const new_size = new_cap * header.elem_size;
        const new_buffer = alloc.mi_realloc(header.buffer, new_size) orelse @panic("OOM in vec_push");
        header.buffer = new_buffer;
        header.cap = new_cap;
    }

    // Copy item bytes into position
    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer));
    const offset = header.len * header.elem_size;
    @memcpy(buffer_bytes[offset .. offset + header.elem_size], @as([*]u8, @ptrCast(item_ptr))[0..header.elem_size]);

    header.len += 1;
}

/// Sets an item at a specific index. Included for collection completeness.
pub export fn maml_vec_set(header: *abi.Vector, index: u32, item_ptr: *anyopaque) void {
    if (index >= header.len) @panic("Vector index out of bounds");

    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer));
    const offset = index * header.elem_size;
    @memcpy(buffer_bytes[offset .. offset + header.elem_size], @as([*]u8, @ptrCast(item_ptr))[0..header.elem_size]);
}

/// Returns a pointer to the element at the specified index.
pub export fn maml_vec_get(header: *abi.Vector, index: u32) *anyopaque {
    if (index >= header.len) @panic("Vector index out of bounds");

    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer));
    const offset = index * header.elem_size;
    return @ptrCast(&buffer_bytes[offset]);
}

/// Returns the current length of the vector.
pub export fn maml_vec_len(header: *abi.Vector) u32 {
    return header.len;
}

/// Clones an existing vector and deep-copies its underlying buffer contents.
pub export fn maml_vec_clone(old_header: *abi.Vector, elem_size: usize) *abi.Vector {
    _ = elem_size; // Handled dynamically via internal header tracking

    // 1. Allocate the new shell
    const raw_new_ptr = alloc.mi_malloc(abi.VECTOR_SIZE) orelse @panic("OOM in maml_vec_clone");
    var new_header = @as(*abi.Vector, @ptrCast(@alignCast(raw_new_ptr)));

    new_header.cap = old_header.cap;
    new_header.len = old_header.len;
    new_header.elem_size = old_header.elem_size;

    // 2. Clone the backing buffer
    const total_bytes = old_header.cap * old_header.elem_size;
    const new_buf = alloc.mi_malloc(total_bytes) orelse @panic("OOM in buffer clone");
    @memcpy(@as([*]u8, @ptrCast(new_buf))[0..total_bytes], @as([*]u8, @ptrCast(old_header.buffer))[0..total_bytes]);
    new_header.buffer = new_buf;

    return new_header;
}

pub export fn maml_vec_free(header: *abi.Vector) void {
    // 1. Free the dynamically allocated inner buffer.
    alloc.mi_free(header.buffer);

    // 2. Free the header itself
    alloc.mi_free(header);
}
