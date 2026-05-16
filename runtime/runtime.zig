const std = @import("std");

extern "C" fn mi_malloc(size: usize) ?*anyopaque;
extern "C" fn mi_free(p: ?*anyopaque) void;

// 1. Define our hidden header
const ArcHeader = struct {
    count: usize,
};

// 2. The upgraded Allocator
export fn maml_alloc(size: usize) ?*anyopaque {
    // Ask mimalloc for enough room for the header AND the data
    const total_size = @sizeOf(ArcHeader) + size;
    const raw_ptr = mi_malloc(total_size) orelse return null;

    // Cast the raw memory to our Header struct and initialize the count to 1
    var header = @as(*ArcHeader, @ptrCast(@alignCast(raw_ptr)));
    header.count = 1;

    // Calculate the pointer to the ACTUAL data (skip past the 8-byte header)
    const raw_bytes = @as([*]u8, @ptrCast(raw_ptr));
    const data_ptr = raw_bytes + @sizeOf(ArcHeader);

    return @ptrCast(data_ptr);
}

// 3. The Retain function (Called when a variable is copied/assigned)
export fn maml_retain(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;

    // Step BACKWARDS in memory to find the hidden header
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const header_ptr = raw_bytes - @sizeOf(ArcHeader);
    
    var header = @as(*ArcHeader, @ptrCast(@alignCast(header_ptr)));
    header.count += 1; // Increment the reference count!
}

// 4. The Release function (Called when a variable goes out of scope)
export fn maml_release(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;

    // Step BACKWARDS in memory to find the hidden header
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const header_ptr = raw_bytes - @sizeOf(ArcHeader);
    
    var header = @as(*ArcHeader, @ptrCast(@alignCast(header_ptr)));
    
    // Decrement the count
    header.count -= 1;
    
    // If nobody is looking at this memory anymore, nuke it!
    if (header.count == 0) {
        mi_free(@ptrCast(header_ptr));
    }
}