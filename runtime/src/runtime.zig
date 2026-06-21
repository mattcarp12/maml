const std = @import("std");

extern "C" fn mi_malloc(size: usize) ?*anyopaque;
extern "C" fn mi_realloc(p: ?*anyopaque, size: usize) ?*anyopaque;
extern "C" fn mi_free(p: ?*anyopaque) void;

// Force the compiler to evaluate the ABI assertions at comptime
comptime {
    _ = @import("abi_assertions.zig");
}

// -----------------------------------------------------------------------------
// ARC Header
// -----------------------------------------------------------------------------

const ArcHeader = extern struct {
    count: usize,
    // Optional virtual destructor hook for complex reference types
    destructor: ?*const fn (?*anyopaque) void,
};

pub export fn maml_alloc(size: usize) ?*anyopaque {
    const total_size = @sizeOf(ArcHeader) + size;
    const raw_ptr = mi_malloc(total_size) orelse return null;

    var header = @as(*ArcHeader, @ptrCast(@alignCast(raw_ptr)));
    header.count = 1;
    header.destructor = null; // Default to no custom destructor
    const user_ptr = @as([*]u8, @ptrCast(raw_ptr)) + @sizeOf(ArcHeader);

    return @ptrCast(user_ptr);
}

// !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
// TODO - Teach LLVM how to optimize arc calls - special llvm optimization pass
// !!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!

pub export fn maml_retain(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const header = @as(*ArcHeader, @ptrCast(@alignCast(raw_bytes - @sizeOf(ArcHeader))));
    header.count += 1;
}

pub export fn maml_release(data_ptr: ?*anyopaque) void {
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

pub export fn maml_free(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const raw_bytes = @as([*]u8, @ptrCast(data_ptr.?));
    const base_ptr = raw_bytes - @sizeOf(ArcHeader);

    mi_free(@ptrCast(base_ptr));
}

// -----------------------------------------------------------------------------
// Vec runtime
// -----------------------------------------------------------------------------

const VecHeader = extern struct {
    buffer: ?*anyopaque,
    cap: u32,
    len: u32,
    elem_size: u32,
    _pad: u32,

    // Type-Erased ARC Hooks
    elem_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    elem_release: ?*const fn (?*anyopaque) callconv(.c) void,
};

pub export fn maml_vec_grow(
    raw_ptr: ?*anyopaque,
    cap_ptr: *u32,
    item_size: u32,
) ?*anyopaque {
    const old_cap = cap_ptr.*;
    const new_cap: u32 = if (old_cap == 0) 8 else old_cap * 2;
    const new_size = @as(usize, new_cap) * @as(usize, item_size);

    const new_raw = mi_realloc(raw_ptr, new_size) orelse @panic("maml_vec_grow: out of memory");

    cap_ptr.* = new_cap;
    return new_raw;
}

pub export fn maml_vec_create(
    elem_size: u32,
    elem_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    elem_release: ?*const fn (?*anyopaque) callconv(.c) void,
) ?*anyopaque {
    // Allocate the shell container
    const raw_ptr = maml_alloc(@sizeOf(VecHeader)) orelse @panic("OOM in maml_vec_create");
    var header = @as(*VecHeader, @ptrCast(@alignCast(raw_ptr)));

    // Set up the virtual destructor to clean up the buffer and elements
    const arc_hdr_ptr = @as([*]u8, @ptrCast(raw_ptr)) - @sizeOf(ArcHeader);
    var arc_hdr = @as(*ArcHeader, @ptrCast(@alignCast(arc_hdr_ptr)));
    arc_hdr.destructor = maml_vec_destructor; // Assuming you have this implemented!

    // Initialize state
    header.buffer = null;
    header.cap = 0;
    header.len = 0;
    header.elem_size = elem_size;
    header._pad = 0;

    // Store the hooks passed by the LLVM frontend
    header.elem_retain = elem_retain;
    header.elem_release = elem_release;

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
            mi_realloc(buf, new_size)
        else
            mi_malloc(new_size);

        header.buffer = new_buffer orelse @panic("OOM in vec_push");
        header.cap = new_cap;
    }

    // Copy item bytes
    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = header.len * header.elem_size;
    @memcpy(buffer_bytes[offset .. offset + header.elem_size], @as([*]u8, @ptrCast(item_ptr.?))[0..header.elem_size]);

    header.len += 1;

    // 🌟 ARC: The Vector takes ownership of the newly pushed element
    if (header.elem_retain) |retain| {
        const heap_ptr = @as(*align(1) ?*anyopaque, @ptrCast(item_ptr.?)).*;
        retain(heap_ptr);
    }
}

pub export fn maml_vec_set(vec_ptr: ?*anyopaque, index: u32, item_ptr: ?*anyopaque) void {
    if (vec_ptr == null or item_ptr == null) return;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));

    if (index >= header.len) @panic("Vector index out of bounds");

    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = index * header.elem_size;
    const target_slice = buffer_bytes[offset .. offset + header.elem_size];

    // 🌟 ARC: Release the old element before overwriting it
    if (header.elem_release) |release| {
        const old_heap_ptr = @as(*align(1) ?*anyopaque, @ptrCast(target_slice.ptr)).*;
        release(old_heap_ptr);
    }

    // Copy new item bytes
    @memcpy(target_slice, @as([*]u8, @ptrCast(item_ptr.?))[0..header.elem_size]);

    // 🌟 ARC: Retain the new element
    if (header.elem_retain) |retain| {
        const new_heap_ptr = @as(*align(1) ?*anyopaque, @ptrCast(item_ptr.?)).*;
        retain(new_heap_ptr);
    }
}

pub export fn maml_vec_get(vec_ptr: ?*anyopaque, index: u32) ?*anyopaque {
    if (vec_ptr == null) return null;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));

    if (index >= header.len) @panic("Vector index out of bounds");

    const buffer_bytes = @as([*]u8, @ptrCast(header.buffer.?));
    const offset = index * header.elem_size;
    return @ptrCast(buffer_bytes + offset);
}

fn maml_vec_destructor(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const header = @as(*VecHeader, @ptrCast(@alignCast(data_ptr.?)));

    if (header.buffer) |buf| {
        const buffer_bytes = @as([*]u8, @ptrCast(buf));

        // 🌟 ARC: Release all surviving elements before the vector dies
        if (header.elem_release) |release| {
            for (0..header.len) |i| {
                const offset = i * header.elem_size;
                const heap_ptr = @as(*align(1) ?*anyopaque, @ptrCast(buffer_bytes + offset)).*;
                release(heap_ptr);
            }
        }

        // Finally, free the backing buffer
        mi_free(buf);
    }
}

pub export fn maml_vec_len(vec_ptr: ?*anyopaque) u32 {
    if (vec_ptr == null) return 0;
    const header = @as(*VecHeader, @ptrCast(@alignCast(vec_ptr.?)));
    return header.len;
}

// -----------------------------------------------------------------------------
// Map runtime
// -----------------------------------------------------------------------------

const MapHeader = extern struct {
    entries: ?*anyopaque,
    count: u32,
    tombstone_count: u32,
    cap: u32,
    val_size: u32,
    is_string_key: u8,
    _pad: [7]u8,

    // Type-Erased ARC Hooks
    key_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    key_release: ?*const fn (?*anyopaque) callconv(.c) void,
    val_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    val_release: ?*const fn (?*anyopaque) callconv(.c) void,
};

// --- Dynamic Map Entry Layout Helpers ---
// Layout (Int Key):    [occupied: u32][hash: u32][value: val_size] (8 bytes header)
// Layout (String Key): [occupied: u32][hash: u32][str_ptr: 8][str_len: u32][_pad: u32][value: val_size] (24 bytes header)

inline fn entryOccupied(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry)));
}

// Helper to step to the Nth entry in the array
inline fn entryAt(entries: [*]u8, index: usize, val_size: u32, is_str: bool) [*]u8 {
    const stride = entrySize(val_size, is_str); // 🌟 Use the aligned size here!
    return entries + (index * stride);
}

inline fn entryKeyHash(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry + 4)));
}

inline fn entryKeyStringPtr(entry: [*]u8) *?[*]const u8 {
    return @as(*?[*]const u8, @ptrCast(@alignCast(entry + 8)));
}

inline fn entryKeyStringLen(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry + 16)));
}

inline fn entryValuePtr(entry: [*]u8, is_str: bool) [*]u8 {
    const header_offset: usize = if (is_str) 24 else 8;
    return entry + header_offset;
}

inline fn entrySize(val_size: u32, is_str: bool) usize {
    const header_offset: usize = if (is_str) 24 else 8;
    const raw_size = header_offset + @as(usize, val_size);
    return (raw_size + 7) & ~@as(usize, 7); // Maintain 8-byte alignment
}

const INITIAL_MAP_CAP: u32 = 16;

fn maml_map_destructor(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    const header = @as(*MapHeader, @ptrCast(@alignCast(data_ptr.?)));

    if (header.entries) |entries_raw| {
        const entries = @as([*]u8, @ptrCast(entries_raw));
        const is_str = header.is_string_key == 1;

        // Iterate through all slots and release only active ones
        for (0..header.cap) |i| {
            const entry = entryAt(entries, i, header.val_size, is_str);
            if (entryOccupied(entry).* == 1) {

                // 🌟 ARC: Cleanup surviving elements as the Map dies
                if (header.key_release) |release| {
                    if (is_str) release(@constCast(entryKeyStringPtr(entry).*));
                }
                if (header.val_release) |release| {
                    release(@ptrCast(entryValuePtr(entry, is_str)));
                }
            }
        }

        // Finally, free the contiguous hash array itself
        mi_free(entries_raw);
    }
}

pub export fn maml_map_create(
    val_size: u32,
    is_string_key: u8,
    key_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    key_release: ?*const fn (?*anyopaque) callconv(.c) void,
    val_retain: ?*const fn (?*anyopaque) callconv(.c) void,
    val_release: ?*const fn (?*anyopaque) callconv(.c) void,
) ?*anyopaque {
    const raw_ptr = maml_alloc(@sizeOf(MapHeader)) orelse @panic("OOM in maml_map_create");
    var header = @as(*MapHeader, @ptrCast(@alignCast(raw_ptr)));

    const arc_hdr_ptr = @as([*]u8, @ptrCast(raw_ptr)) - @sizeOf(ArcHeader);
    var arc_hdr = @as(*ArcHeader, @ptrCast(@alignCast(arc_hdr_ptr)));
    arc_hdr.destructor = maml_map_destructor;

    header.entries = null;
    header.count = 0;
    header.tombstone_count = 0; // Initialize tombstone tracking
    header.cap = 16; // Default initial capacity
    header.val_size = val_size;
    header.is_string_key = is_string_key;
    header._pad = [7]u8{ 0, 0, 0, 0, 0, 0, 0 };

    header.key_retain = key_retain;
    header.key_release = key_release;
    header.val_retain = val_retain;
    header.val_release = val_release;

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
            if (entryOccupied(old_entry).* != 1) continue;

            const hash = entryKeyHash(old_entry).*;
            var slot = @as(usize, @truncate(hash % @as(u64, new_cap)));
            while (entryOccupied(entryAt(new_entries, slot, val_size, is_str)).* != 0) {
                slot = (slot + 1) % @as(usize, new_cap);
            }
            const dest = entryAt(new_entries, slot, val_size, is_str);
            @memcpy(dest[0..esize], old_entry[0..esize]);
        }
        mi_free(old_raw);
    }

    header.entries = new_entries_raw;
    header.cap = new_cap;
    header.tombstone_count = 0;
}

pub export fn maml_map_put(
    map_ptr: ?*anyopaque,
    key_hash: u32,
    str_key_ptr: ?*anyopaque,
    str_key_len: u32,
    int_key: i32,
    val_ptr: ?*anyopaque,
) void {
    if (map_ptr == null or val_ptr == null) return;
    var header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));

    // Check physical load factor (active elements + tombstones)
    const physical_load = header.count + header.tombstone_count;
    if ((physical_load + 1) * 4 >= header.cap * 3) {
        mapGrow(header); // Note: Make sure mapGrow resets tombstone_count to 0!
    }

    // Allocate initial entries if needed
    if (header.entries == null) {
        mapGrow(header);
    }

    const entries = @as([*]u8, @ptrCast(header.entries.?));
    const is_str = header.is_string_key == 1;

    var slot = key_hash & (header.cap - 1);
    var first_tombstone: ?u32 = null;
    var probes: u32 = 0;

    while (probes < header.cap) : (probes += 1) {
        const entry = entryAt(entries, slot, header.val_size, is_str);
        const occupied = entryOccupied(entry);

        if (occupied.* == 0) {
            // Case 1: Insert into Empty Slot (or recycled Tombstone)
            const target_slot = if (first_tombstone) |t| t else slot;
            const target_entry = entryAt(entries, target_slot, header.val_size, is_str);

            entryOccupied(target_entry).* = 1;
            entryKeyHash(target_entry).* = key_hash;

            if (is_str) {
                entryKeyStringPtr(target_entry).* = @as(?[*]const u8, @ptrCast(str_key_ptr));
                entryKeyStringLen(target_entry).* = str_key_len;
            }
            // For integer keys, key_hash == int_key (set above), no separate storage needed.

            // Copy value bytes
            @memcpy(entryValuePtr(target_entry, is_str)[0..header.val_size], @as([*]u8, @ptrCast(val_ptr))[0..header.val_size]);

            header.count += 1;
            if (first_tombstone != null) {
                header.tombstone_count -= 1; // Recycled a tombstone
            }

            // 🌟 ARC: The Map takes ownership of both the key and the value!
            if (header.key_retain) |retain| {
                if (is_str) retain(@ptrCast(str_key_ptr));
            }
            if (header.val_retain) |retain| retain(@ptrCast(val_ptr));

            return;
        } else if (occupied.* == 2) {
            if (first_tombstone == null) first_tombstone = slot;
        } else {
            // Case 2: Slot Occupied, Check for Match
            var match = false;
            if (entryKeyHash(entry).* == key_hash) {
                if (is_str) {
                    const e_ptr = entryKeyStringPtr(entry).*;
                    const e_len = entryKeyStringLen(entry).*;
                    if (e_len == str_key_len) {
                        const e_slice = @as([*]const u8, @ptrCast(e_ptr.?))[0..e_len];
                        const s_slice = @as([*]const u8, @ptrCast(str_key_ptr.?))[0..str_key_len];
                        match = std.mem.eql(u8, e_slice, s_slice);
                    }
                } else {
                    match = (entryKeyHash(entry).* == @as(u32, @bitCast(int_key)));
                }
            }

            if (match) {
                // 🌟 ARC: Overwriting an existing value. Release the old, retain the new.
                if (header.val_release) |release| {
                    release(@ptrCast(entryValuePtr(entry, is_str)));
                }

                @memcpy(entryValuePtr(entry, is_str)[0..header.val_size], @as([*]u8, @ptrCast(val_ptr))[0..header.val_size]);

                if (header.val_retain) |retain| retain(@ptrCast(val_ptr));
                return;
            }
        }
        slot = (slot + 1) & (header.cap - 1);
    }
}

pub export fn maml_map_get(
    map_ptr: ?*anyopaque,
    key_hash: u32,
    str_key_ptr: ?*anyopaque,
    str_key_len: u32,
) ?*anyopaque {
    if (map_ptr == null) return null;
    const header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    if (header.entries == null or header.count == 0) return null;

    const val_size = header.val_size;
    const is_str = header.is_string_key == 1;
    const entries = @as([*]u8, @ptrCast(header.entries.?));
    var slot = @as(usize, key_hash % header.cap);

    var probes: usize = 0;
    while (probes < header.cap) : (probes += 1) {
        const entry = entryAt(entries, slot, val_size, is_str);
        const occupied = entryOccupied(entry);

        if (occupied.* == 0) return null; // Hit an empty slot, key isn't here

        if (occupied.* == 1 and entryKeyHash(entry).* == key_hash) {
            var match = true;
            if (is_str) {
                const len = entryKeyStringLen(entry).*;
                if (len != str_key_len) {
                    match = false;
                } else if (len > 0) {
                    // 1. Unpack the entry pointer (it is already typed as ?[*]const u8 via the helper)
                    const e_slice = entryKeyStringPtr(entry).*.?[0..len];

                    // 2. Explicitly cast the anyopaque parameter to a many-item pointer before slicing
                    const s_slice = @as([*]const u8, @ptrCast(str_key_ptr.?))[0..len];

                    if (!std.mem.eql(u8, e_slice, s_slice)) {
                        match = false;
                    }
                }
            }

            if (match) {
                const ret_ptr = entryValuePtr(entry, is_str);
                return @as(?*anyopaque, @ptrCast(ret_ptr));
            }
        }

        slot = (slot + 1) % @as(usize, header.cap);
    }
    return null;
}

pub export fn maml_map_delete(
    map_ptr: ?*anyopaque,
    key_hash: u32,
    str_key_ptr: ?*anyopaque,
    str_key_len: u32,
    int_key: i32,
) void {
    if (map_ptr == null) return;
    var header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    if (header.entries == null) return;

    const entries = @as([*]u8, @ptrCast(header.entries.?));
    const is_str = header.is_string_key == 1;

    var slot = key_hash & (header.cap - 1);
    var probes: u32 = 0;

    while (probes < header.cap) : (probes += 1) {
        const entry = entryAt(entries, slot, header.val_size, is_str);
        const occupied = entryOccupied(entry);

        if (occupied.* == 0) return; // Not found

        if (occupied.* == 1 and entryKeyHash(entry).* == key_hash) {
            var match = false;
            if (is_str) {
                const e_len = entryKeyStringLen(entry).*;
                if (e_len == str_key_len) {
                    const e_slice = entryKeyStringPtr(entry).*.?[0..e_len];
                    const s_slice = @as([*]const u8, @ptrCast(str_key_ptr.?))[0..str_key_len];
                    match = std.mem.eql(u8, e_slice, s_slice);
                }
            } else {
                match = (entryKeyHash(entry).* == @as(u32, @bitCast(int_key)));
            }

            if (match) {
                // Update layout flags
                occupied.* = 2; // Mark as Tombstone
                header.count -= 1;
                header.tombstone_count += 1; // Track dead space

                // 🌟 ARC: The entry is gone, Map drops ownership of keys and values!
                if (header.key_release) |release| {
                    if (is_str) release(@constCast(entryKeyStringPtr(entry).*));
                }
                if (header.val_release) |release| {
                    release(@ptrCast(entryValuePtr(entry, is_str)));
                }
                return;
            }
        }
        slot = (slot + 1) & (header.cap - 1);
    }
}

pub export fn maml_map_len(map_ptr: ?*anyopaque) u32 {
    if (map_ptr == null) return 0;
    const header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    return header.count;
}

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

// -----------------------------------------------------------------------------
// Async Executor Runtime
// -----------------------------------------------------------------------------

// These helpers will be generated dynamically by our LLVM Codegen backend!
pub extern "C" fn maml_coro_resume_helper(hdl: ?*anyopaque) void;
pub extern "C" fn maml_coro_done_helper(hdl: ?*anyopaque) bool;
pub extern "C" fn maml_coro_destroy_helper(hdl: ?*anyopaque) void;

const TaskNode = struct {
    hdl: ?*anyopaque,
    next: ?*TaskNode,
};

var run_queue_head: ?*TaskNode = null;
var run_queue_tail: ?*TaskNode = null;

pub export fn maml_spawn_task(hdl: ?*anyopaque) void {
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

pub export fn maml_run_executor() void {
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

// -----------------------------------------------------------------------------
// I/O Runtime
// -----------------------------------------------------------------------------

// Bind directly to libc's puts.
// The 'pub' keyword ensures abi_assertions.zig can see it.
// pub extern "C" fn puts(s: ?*anyopaque) i32;

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
