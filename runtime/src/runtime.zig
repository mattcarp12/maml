const std = @import("std");

extern "C" fn mi_malloc(size: usize) ?*anyopaque;
extern "C" fn mi_realloc(p: ?*anyopaque, size: usize) ?*anyopaque;
extern "C" fn mi_free(p: ?*anyopaque) void;

// -----------------------------------------------------------------------------
// ARC Header
// -----------------------------------------------------------------------------

const ArcHeader = struct {
    count: usize,
    // Optional virtual destructor hook for complex reference types
    destructor: ?*const fn (?*anyopaque) void,
};

export fn maml_alloc(size: usize) ?*anyopaque {
    const total_size = @sizeOf(ArcHeader) + size;
    const raw_ptr = mi_malloc(total_size) orelse return null;

    var header = @as(*ArcHeader, @ptrCast(@alignCast(raw_ptr)));
    header.count = 1;
    header.destructor = null; // Default to no custom destructor

    const raw_bytes = @as([*]u8, @ptrCast(raw_ptr));
    return @ptrCast(raw_bytes + @sizeOf(ArcHeader));
}

export fn maml_retain(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const header = @as(*ArcHeader, @ptrCast(@alignCast(raw_bytes - @sizeOf(ArcHeader))));
    header.count += 1;
}

export fn maml_release(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const header_ptr = raw_bytes - @sizeOf(ArcHeader);
    const header = @as(*ArcHeader, @ptrCast(@alignCast(header_ptr)));
    header.count -= 1;
    if (header.count == 0) {
        // If a custom destructor is attached (like for maps), run it first!
        if (header.destructor) |dtor| {
            dtor(data_ptr);
        }
        mi_free(@ptrCast(header_ptr));
    }
}

export fn maml_free(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    mi_free(@ptrCast(raw_bytes - @sizeOf(ArcHeader)));
}

// -----------------------------------------------------------------------------
// Vec runtime
// -----------------------------------------------------------------------------

export fn maml_vec_grow(
    raw_ptr: ?*anyopaque,
    len: u32,
    cap_ptr: *u32,
    item_size: u32,
) ?*anyopaque {
    const old_cap = cap_ptr.*;
    const new_cap: u32 = if (old_cap == 0) 8 else old_cap * 2;
    const new_size = @as(usize, new_cap) * @as(usize, item_size);

    // FIX: Allocate the new buffer with an ARC header
    const new_raw = maml_alloc(new_size) orelse @panic("maml_vec_grow: out of memory");

    // FIX: Safely copy old data and release the old ARC pointer
    if (raw_ptr) |old_ptr| {
        const old_size = @as(usize, len) * @as(usize, item_size);
        const src = @as([*]const u8, @ptrCast(old_ptr));
        const dst = @as([*]u8, @ptrCast(new_raw));
        @memcpy(dst[0..old_size], src[0..old_size]);

        maml_release(old_ptr);
    }

    cap_ptr.* = new_cap;
    return new_raw;
}

const VecHeader = extern struct {
    raw_ptr: ?*anyopaque,
    data_ptr: ?*anyopaque,
    len: u32,
    cap: u32,
};

fn maml_vec_destructor(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const header = @as(*VecHeader, @ptrCast(@alignCast(data_ptr.?)));

    // If the vector contains reference types, we must release them all!
    // (You will need to add an `is_ref_type: u8` field to VecHeader just like MapHeader)
    if (header.raw_ptr) |buffer| {
        mi_free(buffer);
    }
}

// -----------------------------------------------------------------------------
// Map runtime
// -----------------------------------------------------------------------------

const MapHeader = extern struct {
    entries: ?*anyopaque,
    count: u32,
    cap: u32,
    val_size: u32,
    is_string_key: u8, // 🌟 Stored to drive internal structural layout branches
};

fn entrySize(val_size: u32, is_string_key: bool) usize {
    var raw_size = 8 + @as(usize, val_size) + 1;
    if (is_string_key) {
        raw_size += 16; // 8 bytes char pointer + 8 bytes length tracking
    }
    return (raw_size + 7) & ~@as(usize, 7);
}

fn entryAt(base: [*]u8, index: usize, val_size: u32, is_string_key: bool) [*]u8 {
    return base + index * entrySize(val_size, is_string_key);
}

fn entryKeyHash(entry: [*]u8) *u64 {
    return @as(*u64, @ptrCast(@alignCast(entry)));
}

fn entryKeyStringPtr(entry: [*]u8) *[*]const u8 {
    return @as(*[*]const u8, @ptrCast(@alignCast(entry + 8)));
}

fn entryKeyStringLen(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry + 16)));
}

fn entryValue(entry: [*]u8, is_string_key: bool) [*]u8 {
    return entry + (if (is_string_key) @as(usize, 24) else @as(usize, 8));
}

fn entryOccupied(entry: [*]u8, val_size: u32, is_string_key: bool) *u8 {
    return &entry[if (is_string_key) @as(usize, 24) + val_size else @as(usize, 8) + val_size];
}

const INITIAL_MAP_CAP: u32 = 16;

fn maml_map_destructor(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const header = @as(*MapHeader, @ptrCast(@alignCast(data_ptr.?)));
    if (header.entries) |entries| {
        mi_free(entries);
    }
}

export fn maml_map_create(value_size: u32, is_string_key: u8) ?*anyopaque {
    const raw_ptr = maml_alloc(@sizeOf(MapHeader)) orelse return null;
    const header = @as(*MapHeader, @ptrCast(@alignCast(raw_ptr)));

    const raw_bytes = @as([*]u8, @ptrCast(raw_ptr));
    const arc_header = @as(*ArcHeader, @ptrCast(@alignCast(raw_bytes - @sizeOf(ArcHeader))));
    arc_header.destructor = maml_map_destructor;

    header.entries = null;
    header.count = 0;
    header.cap = INITIAL_MAP_CAP;
    header.val_size = value_size;
    header.is_string_key = is_string_key;

    const is_str = is_string_key == 1;
    const entries_bytes = mi_malloc(@as(usize, INITIAL_MAP_CAP) * entrySize(value_size, is_str)) orelse {
        maml_release(raw_ptr);
        return null;
    };

    const bytes = @as([*]u8, @ptrCast(entries_bytes));
    @memset(bytes[0 .. @as(usize, INITIAL_MAP_CAP) * entrySize(value_size, is_str)], 0);
    header.entries = entries_bytes;

    return raw_ptr;
}

fn mapGrow(header: *MapHeader) void {
    const old_cap = header.cap;
    const new_cap = old_cap * 2;
    const val_size = header.val_size;
    const is_str = header.is_string_key == 1;
    const esize = entrySize(val_size, is_str);

    const new_entries_raw = mi_malloc(@as(usize, new_cap) * esize) orelse
        @panic("maml_map: out of memory during grow");

    const new_entries = @as([*]u8, @ptrCast(new_entries_raw));
    @memset(new_entries[0 .. @as(usize, new_cap) * esize], 0);

    if (header.entries) |old_raw| {
        const old_entries = @as([*]u8, @ptrCast(old_raw));
        for (0..@as(usize, old_cap)) |i| {
            const old_entry = entryAt(old_entries, i, val_size, is_str);
            if (entryOccupied(old_entry, val_size, is_str).* == 0) continue;

            const hash = entryKeyHash(old_entry).*;
            var slot = @as(usize, @truncate(hash % @as(u64, new_cap)));
            while (entryOccupied(entryAt(new_entries, slot, val_size, is_str), val_size, is_str).* != 0) {
                slot = (slot + 1) % @as(usize, new_cap);
            }
            const dest = entryAt(new_entries, slot, val_size, is_str);
            @memcpy(dest[0..esize], old_entry[0..esize]);
        }
        mi_free(old_raw);
    }

    header.entries = new_entries_raw;
    header.cap = new_cap;
}

export fn maml_map_put(
    map_ptr: ?*anyopaque,
    key_hash: u64,
    value_ptr: ?*anyopaque,
    str_key_ptr: ?[*]const u8,
    str_key_len: u32,
) void {
    if (map_ptr == null) return;
    const header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    const is_str = header.is_string_key == 1;

    if (header.count * 4 >= header.cap * 3) {
        mapGrow(header);
    }

    const val_size = header.val_size;
    const entries = @as([*]u8, @ptrCast(header.entries.?));
    var slot = @as(usize, @truncate(key_hash % @as(u64, header.cap)));

    while (true) {
        const entry = entryAt(entries, slot, val_size, is_str);
        const occupied = entryOccupied(entry, val_size, is_str);

        if (occupied.* == 0) {
            entryKeyHash(entry).* = key_hash;
            if (is_str) {
                entryKeyStringPtr(entry).* = str_key_ptr.?;
                entryKeyStringLen(entry).* = str_key_len;
            }
            if (value_ptr) |vp| {
                const src = @as([*]u8, @ptrCast(vp));
                @memcpy(entryValue(entry, is_str)[0..@as(usize, val_size)], src[0..@as(usize, val_size)]);
            }
            occupied.* = 1;
            header.count += 1;
            return;
        }

        if (entryKeyHash(entry).* == key_hash) {
            var match = true;
            if (is_str) {
                const len = entryKeyStringLen(entry).*;
                if (len != str_key_len) {
                    match = false;
                } else if (len > 0) {
                    // CRITICAL FIX: Only unwrap and compare memory if length > 0
                    if (!std.mem.eql(u8, entryKeyStringPtr(entry).*[0..len], str_key_ptr.?[0..len])) {
                        match = false;
                    }
                }
            }
            if (match) {
                if (value_ptr) |vp| {
                    const src = @as([*]u8, @ptrCast(vp));
                    @memcpy(entryValue(entry, is_str)[0..@as(usize, val_size)], src[0..@as(usize, val_size)]);
                }
                return;
            }
        }

        slot = (slot + 1) % @as(usize, header.cap);
    }
}

export fn maml_map_get(
    map_ptr: ?*anyopaque,
    key_hash: u64,
    str_key_ptr: ?[*]const u8,
    str_key_len: u32,
) ?*anyopaque {
    if (map_ptr == null) return null;
    const header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    if (header.entries == null) return null;

    const val_size = header.val_size;
    const is_str = header.is_string_key == 1;
    const entries = @as([*]u8, @ptrCast(header.entries.?));
    var slot = @as(usize, @truncate(key_hash % @as(u64, header.cap)));

    for (0..@as(usize, header.cap)) |_| {
        const entry = entryAt(entries, slot, val_size, is_str);
        const occupied = entryOccupied(entry, val_size, is_str);

        if (occupied.* == 0) return null;

        if (entryKeyHash(entry).* == key_hash) {
            var match = true;
            if (is_str) {
                const len = entryKeyStringLen(entry).*;
                if (len != str_key_len) {
                    match = false;
                } else if (len > 0) {
                    // CRITICAL FIX: Only unwrap and compare memory if length > 0
                    if (!std.mem.eql(u8, entryKeyStringPtr(entry).*[0..len], str_key_ptr.?[0..len])) {
                        match = false;
                    }
                }
            }
            if (match) {
                return @ptrCast(entryValue(entry, is_str));
            }
        }

        slot = (slot + 1) % @as(usize, header.cap);
    }

    return null;
}

export fn maml_str_hash(ptr: [*]const u8, len: u32) u64 {
    var hash: u64 = 5381; // 🌟 Upgraded to standard, collision-resistant djb2 algorithm!
    var i: u32 = 0;
    while (i < len) : (i += 1) {
        hash = (hash *% 33) +% @as(u64, ptr[i]);
    }
    return hash;
}

// -----------------------------------------------------------------------------
// Async Executor Runtime
// -----------------------------------------------------------------------------

// These helpers will be generated dynamically by our LLVM Codegen backend!
extern "C" fn maml_coro_resume_helper(hdl: ?*anyopaque) void;
extern "C" fn maml_coro_done_helper(hdl: ?*anyopaque) bool;
extern "C" fn maml_coro_destroy_helper(hdl: ?*anyopaque) void;

const TaskNode = struct {
    hdl: ?*anyopaque,
    next: ?*TaskNode,
};

var run_queue_head: ?*TaskNode = null;
var run_queue_tail: ?*TaskNode = null;

export fn maml_spawn_task(hdl: ?*anyopaque) void {
    if (hdl == null) return;

    const node_ptr = mi_malloc(@sizeOf(TaskNode)) orelse @panic("OOM in task spawn");
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

export fn maml_run_executor() void {
    // Round-robin cooperative polling loop
    while (run_queue_head != null) {
        // 1. Pop the next task off the queue
        const node = run_queue_head.?;
        run_queue_head = node.next;
        if (run_queue_head == null) run_queue_tail = null;

        const hdl = node.hdl;
        mi_free(node);

        if (hdl) |handle| {
            // 2. If it's not done, resume it!
            if (!maml_coro_done_helper(handle)) {
                maml_coro_resume_helper(handle);

                // 3. After yielding, check if it finished
                if (!maml_coro_done_helper(handle)) {
                    // Still pending (hit another await). Put it back in the queue.
                    maml_spawn_task(handle);
                } else {
                    // Task is completely finished. Destroy the LLVM frame.
                    maml_coro_destroy_helper(handle);
                }
            } else {
                // Was already done before we ran it, just clean it up
                maml_coro_destroy_helper(handle);
            }
        }
    }
}
