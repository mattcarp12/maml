const std = @import("std");

// -----------------------------------------------------------------------------
// I/O Runtime
// -----------------------------------------------------------------------------

pub export fn maml_print(opaque_ptr: ?*anyopaque, len: i32) void {
    var threaded = std.Io.Threaded.init_single_threaded;
    const io = threaded.io();

    const stdout = std.Io.File.stdout();

    if (len <= 0 or opaque_ptr == null) {
        stdout.writeStreamingAll(io, "\n") catch {};
        return;
    }

    const byte_ptr: [*]const u8 = @ptrCast(opaque_ptr.?);
    const usize_len = @as(usize, @intCast(len));
    const slice = byte_ptr[0..usize_len];

    stdout.writeStreamingAll(io, slice) catch {};
    stdout.writeStreamingAll(io, "\n") catch {};
}
