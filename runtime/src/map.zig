const alloc = @import("alloc.zig");
const abi = @import("abi.zig");
const std = @import("std");

// -----------------------------------------------------------------------------
// Map runtime helpers
// -----------------------------------------------------------------------------

inline fn entryOccupied(entry: [*]u8) *u32 {
    return @as(*u32, @ptrCast(@alignCast(entry)));
}

inline fn entryAt(entries: *anyopaque, index: usize, val_size: u32, is_str: bool) [*]u8 {
    const stride = entrySize(val_size, is_str);
    const entries_bytes = @as([*]u8, @ptrCast(entries));
    return entries_bytes + (index * stride);
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
    return header_offset + val_size;
}

fn findSlot(
    entries: *anyopaque,
    cap: u32,
    hash: u32,
    key_str_ptr: ?*anyopaque,
    key_str_len: u32,
    val_size: u32,
    is_str: bool,
) ?usize {
    if (cap == 0) return null;

    var index = @as(usize, hash) % @as(usize, cap);
    var first_tombstone: ?usize = null;

    var i: usize = 0;
    while (i < cap) : (i += 1) {
        const entry = entryAt(entries, index, val_size, is_str);
        const status = entryOccupied(entry).*;

        if (status == 0) {
            return first_tombstone orelse index;
        } else if (status == 2) {
            if (first_tombstone == null) {
                first_tombstone = index;
            }
        } else if (status == 1) {
            if (is_str) {
                const entry_hash = entryKeyHash(entry).*;
                if (entry_hash == hash) {
                    const elen = entryKeyStringLen(entry).*;
                    if (elen == key_str_len) {
                        const eptr = entryKeyStringPtr(entry).*;
                        if (eptr != null) {
                            const key_slice = @as([*]const u8, @ptrCast(key_str_ptr))[0..key_str_len];
                            if (std.mem.eql(u8, eptr.?[0..elen], key_slice)) {
                                return index;
                            }
                        }
                    }
                }
            } else {
                if (entryKeyHash(entry).* == hash) {
                    return index;
                }
            }
        }

        index = (index + 1) % @as(usize, cap);
    }

    return first_tombstone;
}

fn resize(map: *abi.Map, new_cap: u32) void {
    const is_str = map.is_string_key;
    const esize = entrySize(map.val_size, is_str);
    const total_bytes = @as(usize, new_cap) * esize;

    const new_entries_raw = alloc.mi_malloc(total_bytes) orelse @panic("OOM in map resize");
    var idx: usize = 0;
    while (idx < new_cap) : (idx += 1) {
        const entries_bytes = @as([*]u8, @ptrCast(new_entries_raw));
        const entry = entries_bytes + (idx * esize);
        @as(*u32, @ptrCast(@alignCast(entry))).* = 0;
    }

    const old_entries_ptr = map.entries;
    const old_cap = map.cap;

    map.entries = new_entries_raw;
    map.cap = new_cap;
    map.count = 0;
    map.tombstone_count = 0;

    if (old_cap > 0) {
        const old_entries = @as([*]u8, @ptrCast(old_entries_ptr));
        var i: usize = 0;
        while (i < old_cap) : (i += 1) {
            const old_entry = entryAt(old_entries, i, map.val_size, is_str);
            if (entryOccupied(old_entry).* == 1) {
                const hash = entryKeyHash(old_entry).*;
                var kptr: ?[*]const u8 = null;
                var klen: u32 = 0;

                if (is_str) {
                    kptr = entryKeyStringPtr(old_entry).*;
                    klen = entryKeyStringLen(old_entry).*;
                }

                const slot = findSlot(new_entries_raw, new_cap, hash, @constCast(kptr), klen, map.val_size, is_str).?;
                const dest_entry = entryAt(new_entries_raw, slot, map.val_size, is_str);

                entryOccupied(dest_entry).* = 1;
                entryKeyHash(dest_entry).* = hash;

                if (is_str) {
                    entryKeyStringPtr(dest_entry).* = kptr;
                    entryKeyStringLen(dest_entry).* = klen;
                }

                const src_val = entryValuePtr(old_entry, is_str);
                const dest_val = entryValuePtr(dest_entry, is_str);
                @memcpy(dest_val[0..map.val_size], src_val[0..map.val_size]);

                map.count += 1;
            }
        }
        alloc.mi_free(old_entries_ptr);
    }
}

// -----------------------------------------------------------------------------
// Exported C Runtime API
// -----------------------------------------------------------------------------

pub export fn maml_map_create(val_size: u32, is_string_key: bool) *abi.Map {
    // 1. Allocate the header
    const raw_ptr = alloc.mi_malloc(abi.MAP_SIZE) orelse @panic("OOM in maml_map_create");
    var map = @as(*abi.Map, @ptrCast(@alignCast(raw_ptr)));

    // 2. Pre-allocate initial buffer (e.g., capacity 8)
    const initial_cap: u32 = 8;
    const esize = entrySize(val_size, is_string_key);
    const initial_entries = alloc.mi_malloc(@as(usize, initial_cap) * esize) orelse @panic("OOM in map initial allocation");

    // Clear initial buffer
    @memset(@as([*]u8, @ptrCast(initial_entries))[0..(initial_cap * esize)], 0);

    map.count = 0;
    map.tombstone_count = 0;
    map.cap = initial_cap;
    map.val_size = val_size;
    map.is_string_key = is_string_key;
    map.entries = initial_entries; // Now always valid non-null pointer

    return map;
}

pub export fn maml_map_put(
    map: *abi.Map,
    hash: u32,
    key_str_ptr: *anyopaque,
    key_str_len: u32,
    val_ptr: *anyopaque,
) void {
    if (map.cap == 0 or (map.count + map.tombstone_count + 1) * 4 > map.cap * 3) {
        const new_cap = if (map.cap == 0) 8 else map.cap * 2;
        resize(map, new_cap);
    }

    const entries = @as([*]u8, @ptrCast(map.entries));
    const is_str = map.is_string_key;
    const slot = findSlot(entries, map.cap, hash, key_str_ptr, key_str_len, map.val_size, is_str).?;
    const entry = entryAt(entries, slot, map.val_size, is_str);

    const old_status = entryOccupied(entry).*;
    if (old_status != 1) {
        if (old_status == 2) {
            map.tombstone_count -= 1;
        }
        map.count += 1;
    }

    entryOccupied(entry).* = 1;
    entryKeyHash(entry).* = hash;

    if (is_str) {
        entryKeyStringPtr(entry).* = @as(?[*]const u8, @ptrCast(key_str_ptr));
        entryKeyStringLen(entry).* = key_str_len;
    }

    const dest_val = entryValuePtr(entry, is_str);
    @memcpy(dest_val[0..map.val_size], @as([*]const u8, @ptrCast(val_ptr))[0..map.val_size]);
}

pub export fn maml_map_get(
    map: *abi.Map,
    hash: u32,
    key_str_ptr: *anyopaque,
    key_str_len: u32,
) ?*anyopaque {
    if (map.cap == 0 or map.count == 0) return null;

    const entries = @as([*]u8, @ptrCast(map.entries));
    const is_str = map.is_string_key;
    const slot = findSlot(entries, map.cap, hash, key_str_ptr, key_str_len, map.val_size, is_str).?;
    const entry = entryAt(entries, slot, map.val_size, is_str);

    if (entryOccupied(entry).* == 1) {
        return @ptrCast(entryValuePtr(entry, is_str));
    }

    return null;
}

pub export fn maml_map_delete(
    map: *abi.Map,
    hash: u32,
    key_str_ptr: *anyopaque,
    key_str_len: u32,
) void {
    if (map.cap == 0 or map.count == 0) return;

    const entries = @as([*]u8, @ptrCast(map.entries));
    const is_str = map.is_string_key;
    const slot = findSlot(entries, map.cap, hash, key_str_ptr, key_str_len, map.val_size, is_str).?;
    const entry = entryAt(entries, slot, map.val_size, is_str);

    if (entryOccupied(entry).* == 1) {
        entryOccupied(entry).* = 2; // Write tombstone status
        map.count -= 1;
        map.tombstone_count += 1;
    }
}

pub export fn maml_map_len(map: *abi.Map) u32 {
    return map.count;
}

pub export fn maml_map_next_active(
    map: *abi.Map,
    index_ptr: *u32,
    out_str_key: *?*anyopaque,
) ?*anyopaque {
    if (map.cap == 0 or map.count == 0) return null;
    const entries = @as([*]u8, @ptrCast(map.entries));
    const is_str = map.is_string_key;
    while (index_ptr.* < map.cap) {
        const current_idx = index_ptr.*;
        index_ptr.* += 1;

        const entry = entryAt(entries, current_idx, map.val_size, is_str);

        if (entryOccupied(entry).* == 1) {
            if (is_str) {
                out_str_key.* = @as(?*anyopaque, @constCast(entryKeyStringPtr(entry).*));
            } else {
                out_str_key.* = null;
            }
            return @ptrCast(entryValuePtr(entry, is_str));
        }
    }

    return null;
}

pub export fn maml_map_clone(map: *abi.Map) *abi.Map {
    const raw_new_ptr = alloc.mi_malloc(abi.MAP_SIZE) orelse @panic("OOM in map clone");
    var new_header = @as(*abi.Map, @ptrCast(@alignCast(raw_new_ptr)));

    new_header.count = map.count;
    new_header.tombstone_count = map.tombstone_count;
    new_header.cap = map.cap;
    new_header.val_size = map.val_size;
    new_header.is_string_key = map.is_string_key;

    const old_entries_raw = map.entries;
    const is_str = map.is_string_key;
    const esize = entrySize(map.val_size, is_str);
    const total_bytes = @as(usize, map.cap) * esize;
    const new_entries = alloc.mi_malloc(total_bytes) orelse @panic("OOM in map clone elements");
    const dest_bytes = @as([*]u8, @ptrCast(new_entries))[0..total_bytes];
    const src_bytes = @as([*]const u8, @ptrCast(old_entries_raw))[0..total_bytes];
    @memcpy(dest_bytes, src_bytes);
    new_header.entries = new_entries;

    return new_header;
}

/// Frees the map's backing entries buffer and its heap-allocated header.
pub export fn maml_map_free(map: *abi.Map) void {
    alloc.mi_free(map.entries);
    alloc.mi_free(map);
}
