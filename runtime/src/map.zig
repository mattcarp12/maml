const alloc = @import("alloc.zig");
const std = @import("std");

// -----------------------------------------------------------------------------
// Map runtime
// -----------------------------------------------------------------------------

const MapHeader = extern struct {
    entries: ?*anyopaque,
    count: u32,
    tombstone_count: u32,
    cap: u32,
    val_size: u32,
    is_string_key: bool,
    _pad: [7]u8,
};

inline fn entryOccupied(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry)));
}

// Helper to step to the Nth entry in the array
inline fn entryAt(entries: [*]u8, index: usize, val_size: u32, is_str: bool) [*]u8 {
    const stride = entrySize(val_size, is_str);
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

pub export fn maml_map_create(val_size: u32, is_string_key: bool) ?*anyopaque {
    // 1. Allocate a raw header without the ArcHeader prefix
    const raw_ptr = alloc.mi_malloc(@sizeOf(MapHeader)) orelse @panic("OOM in maml_map_create");
    var header = @as(*MapHeader, @ptrCast(@alignCast(raw_ptr)));

    header.entries = null;
    header.count = 0;
    header.tombstone_count = 0;
    header.cap = 16;
    header.val_size = val_size;
    header.is_string_key = is_string_key;
    header._pad = [7]u8{ 0, 0, 0, 0, 0, 0, 0 };

    return raw_ptr;
}

fn mapGrow(header: *MapHeader) void {
    const old_cap = header.cap;
    const new_cap = old_cap * 2;
    const val_size = header.val_size;
    const is_str = header.is_string_key;
    const esize = entrySize(val_size, is_str);

    const new_entries_raw = alloc.mi_malloc(@as(usize, new_cap) * esize) orelse
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
        alloc.mi_free(old_raw);
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
    const is_str = header.is_string_key;

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
                @memcpy(entryValuePtr(entry, is_str)[0..header.val_size], @as([*]u8, @ptrCast(val_ptr))[0..header.val_size]);
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
    const is_str = header.is_string_key;
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
    const is_str = header.is_string_key;

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

pub export fn maml_map_next_active(
    map_ptr: ?*anyopaque,
    iter_state_ptr: ?*anyopaque,
    out_str_ptr: ?*anyopaque,
) ?*anyopaque {
    if (map_ptr == null or iter_state_ptr == null or out_str_ptr == null) return null;

    const header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));
    if (header.entries == null) return null;

    // Cast the type-erased ABI pointers into typed pointers
    const iter_state = @as(*u32, @ptrCast(@alignCast(iter_state_ptr.?)));
    const out_str_key = @as(*?*anyopaque, @ptrCast(@alignCast(out_str_ptr.?)));

    const entries = @as([*]u8, @ptrCast(header.entries.?));
    const is_str = header.is_string_key;

    // Scan forward starting from the current iter_state index
    while (iter_state.* < header.cap) {
        const i = iter_state.*;
        iter_state.* += 1; // Advance state for the next call

        const entry = entryAt(entries, i, header.val_size, is_str);

        // 1 means occupied/active
        if (entryOccupied(entry).* == 1) {
            // If it's a string map, populate the out-parameter
            if (is_str) {
                out_str_key.* = @constCast(entryKeyStringPtr(entry).*);
            } else {
                out_str_key.* = null;
            }

            // Return the pointer to the value slot
            return @ptrCast(entryValuePtr(entry, is_str));
        }
    }

    // Iteration complete
    return null;
}

pub export fn maml_map_clone(map_ptr: ?*anyopaque) ?*anyopaque {
    if (map_ptr == null) return null;
    const old_header = @as(*MapHeader, @ptrCast(@alignCast(map_ptr.?)));

    // 1. Allocate the new shell
    const raw_new_ptr = alloc.mi_malloc(@sizeOf(MapHeader)) orelse @panic("OOM in map clone");
    var new_header = @as(*MapHeader, @ptrCast(@alignCast(raw_new_ptr)));

    new_header.count = old_header.count;
    new_header.tombstone_count = old_header.tombstone_count;
    new_header.cap = old_header.cap;
    new_header.val_size = old_header.val_size;
    new_header.is_string_key = old_header.is_string_key;
    new_header._pad = [7]u8{ 0, 0, 0, 0, 0, 0, 0 };
    new_header.entries = null;

    // 2. Clone the hash array if it exists
    if (old_header.entries) |old_entries_raw| {
        const is_str = old_header.is_string_key;
        const esize = entrySize(old_header.val_size, is_str);
        const total_bytes = @as(usize, old_header.cap) * esize;

        const new_entries_raw = alloc.mi_malloc(total_bytes) orelse @panic("OOM in map entries clone");

        // Because it's open addressing, we must copy the exact layout including tombstones
        const src = @as([*]u8, @ptrCast(old_entries_raw))[0..total_bytes];
        const dst = @as([*]u8, @ptrCast(new_entries_raw))[0..total_bytes];
        @memcpy(dst, src);

        new_header.entries = new_entries_raw;
    }

    return raw_new_ptr;
}
