#include "mamlrt_abi.h"
#include <string_view>
#include <cstring>
#include <cstdlib>

enum class EntryStatus : uint32_t {
    Empty = 0,
    Occupied = 1,
    Tombstone = 2
};

struct StrKeyHeader {
    EntryStatus status;      // 0: empty, 1: occupied, 2: tombstone
    uint32_t hash;
    const char* key_ptr;
    uint32_t key_len;
};
static_assert(sizeof(StrKeyHeader) == 24, "StrKeyHeader size mismatch");

struct IntKeyHeader {
    EntryStatus status;
    uint32_t hash;
};
static_assert(sizeof(IntKeyHeader) == 8, "IntKeyHeader size mismatch");

// -----------------------------------------------------------------------------
// Map runtime helpers
// -----------------------------------------------------------------------------

static inline size_t entry_size(uint32_t val_size, bool is_str) {
    return (is_str ? sizeof(StrKeyHeader) : sizeof(IntKeyHeader)) + val_size;
}

static inline unsigned char* entry_at(void* entries, size_t index, uint32_t val_size, bool is_str) {
    auto* bytes = static_cast<unsigned char*>(entries);
    return bytes + (index * entry_size(val_size, is_str));
}

static inline EntryStatus& entry_status(unsigned char* entry) {
    // Reinterpret cast is safe here because mi_malloc guarantees alignment
    return *reinterpret_cast<EntryStatus*>(entry);
}

static inline unsigned char* entry_value_ptr(unsigned char* entry, bool is_str) {
    return entry + (is_str ? sizeof(StrKeyHeader) : sizeof(IntKeyHeader));
}

// -----------------------------------------------------------------------------
// Core Internal Logic
// -----------------------------------------------------------------------------

static size_t find_slot(void* entries, uint32_t cap, uint32_t hash, 
                        void* key_str_ptr, uint32_t key_str_len, 
                        uint32_t val_size, bool is_str) {
    if (cap == 0) return static_cast<size_t>(-1); // Represents null

    size_t index = static_cast<size_t>(hash) % cap;
    size_t first_tombstone = static_cast<size_t>(-1);

    for (size_t i = 0; i < cap; ++i) {
        unsigned char* entry = entry_at(entries, index, val_size, is_str);
        EntryStatus status = entry_status(entry);

        if (status == EntryStatus::Empty) { // Empty
            return (first_tombstone != static_cast<size_t>(-1)) ? first_tombstone : index;
        } 
        else if (status == EntryStatus::Tombstone) { // Tombstone
            if (first_tombstone == static_cast<size_t>(-1)) {
                first_tombstone = index;
            }
        } 
        else if (status == EntryStatus::Occupied) { // Occupied
            if (is_str) {
                auto* header = reinterpret_cast<StrKeyHeader*>(entry);
                if (header->hash == hash && header->key_len == key_str_len) {
                    if (header->key_ptr != nullptr) {
                        std::string_view entry_key{header->key_ptr, header->key_len};
                        std::string_view target_key{static_cast<const char*>(key_str_ptr), key_str_len};
                        if (entry_key == target_key) {
                            return index;
                        }
                    }
                }
            } else {
                auto* header = reinterpret_cast<IntKeyHeader*>(entry);
                if (header->hash == hash) {
                    return index;
                }
            }
        }

        index = (index + 1) % cap;
    }

    return first_tombstone;
}

static void resize(Map* map, uint32_t new_cap) {
    bool is_str = map->is_string_key;
    size_t esize = entry_size(map->val_size, is_str);
    size_t total_bytes = static_cast<size_t>(new_cap) * esize;

    // Use mi_zalloc to automatically zero out the memory.
    // This perfectly handles setting all `status` fields to 0 (empty).
    void* new_entries = maml_calloc(1,total_bytes);
    if (new_entries == nullptr) {
        std::abort(); // OOM in map resize
    }

    void* old_entries = map->entries;
    uint32_t old_cap = map->cap;

    map->entries = new_entries;
    map->cap = new_cap;
    map->count = 0;
    map->tombstone_count = 0;

    if (old_cap > 0 && old_entries != nullptr) {
        for (size_t i = 0; i < old_cap; ++i) {
            unsigned char* old_entry = entry_at(old_entries, i, map->val_size, is_str);
            
            if (entry_status(old_entry) == EntryStatus::Occupied) { // Occupied
                uint32_t hash;
                void* kptr = nullptr;
                uint32_t klen = 0;

                if (is_str) {
                    auto* header = reinterpret_cast<StrKeyHeader*>(old_entry);
                    hash = header->hash;
                    kptr = const_cast<char*>(header->key_ptr);
                    klen = header->key_len;
                } else {
                    auto* header = reinterpret_cast<IntKeyHeader*>(old_entry);
                    hash = header->hash;
                }

                size_t slot = find_slot(new_entries, new_cap, hash, kptr, klen, map->val_size, is_str);
                unsigned char* dest_entry = entry_at(new_entries, slot, map->val_size, is_str);

                entry_status(dest_entry) = EntryStatus::Occupied;

                if (is_str) {
                    auto* dest_header = reinterpret_cast<StrKeyHeader*>(dest_entry);
                    dest_header->hash = hash;
                    dest_header->key_ptr = static_cast<const char*>(kptr);
                    dest_header->key_len = klen;
                } else {
                    auto* dest_header = reinterpret_cast<IntKeyHeader*>(dest_entry);
                    dest_header->hash = hash;
                }

                unsigned char* src_val = entry_value_ptr(old_entry, is_str);
                unsigned char* dest_val = entry_value_ptr(dest_entry, is_str);
                std::memcpy(dest_val, src_val, map->val_size);

                map->count += 1;
            }
        }
        maml_free(old_entries);
    }
}

// -----------------------------------------------------------------------------
// Exported C Runtime API
// -----------------------------------------------------------------------------

void maml_map_put(Map* map, uint32_t hash, void* key_str_ptr, uint32_t key_str_len, void* val_ptr) {
    if (map->cap == 0 || (map->count + map->tombstone_count + 1) * 4 > map->cap * 3) {
        uint32_t new_cap = (map->cap == 0) ? 8 : map->cap * 2;
        resize(map, new_cap);
    }

    bool is_str = map->is_string_key;
    size_t slot = find_slot(map->entries, map->cap, hash, key_str_ptr, key_str_len, map->val_size, is_str);
    unsigned char* entry = entry_at(map->entries, slot, map->val_size, is_str);

    EntryStatus old_status = entry_status(entry);
    if (old_status != EntryStatus::Occupied) {
        if (old_status == EntryStatus::Tombstone) {
            map->tombstone_count -= 1;
        }
        map->count += 1;
    }

    entry_status(entry) = EntryStatus::Occupied;

    if (is_str) {
        auto* header = reinterpret_cast<StrKeyHeader*>(entry);
        header->hash = hash;
        header->key_ptr = static_cast<const char*>(key_str_ptr);
        header->key_len = key_str_len;
    } else {
        auto* header = reinterpret_cast<IntKeyHeader*>(entry);
        header->hash = hash;
    }

    unsigned char* dest_val = entry_value_ptr(entry, is_str);
    std::memcpy(dest_val, val_ptr, map->val_size);
}

void* maml_map_get(Map* map, uint32_t hash, void* key_str_ptr, uint32_t key_str_len) {
    if (map->cap == 0 || map->count == 0) return nullptr;

    bool is_str = map->is_string_key;
    size_t slot = find_slot(map->entries, map->cap, hash, key_str_ptr, key_str_len, map->val_size, is_str);
    
    if (slot == static_cast<size_t>(-1)) return nullptr;

    unsigned char* entry = entry_at(map->entries, slot, map->val_size, is_str);
    if (entry_status(entry) == EntryStatus::Occupied) {
        return entry_value_ptr(entry, is_str);
    }

    return nullptr;
}

void maml_map_delete(Map* map, uint32_t hash, void* key_str_ptr, uint32_t key_str_len) {
    if (map->cap == 0 || map->count == 0) return;

    bool is_str = map->is_string_key;
    size_t slot = find_slot(map->entries, map->cap, hash, key_str_ptr, key_str_len, map->val_size, is_str);
    
    if (slot == static_cast<size_t>(-1)) return;

    unsigned char* entry = entry_at(map->entries, slot, map->val_size, is_str);
    if (entry_status(entry) == EntryStatus::Occupied) {
        entry_status(entry) = EntryStatus::Tombstone; // Write tombstone status
        map->count -= 1;
        map->tombstone_count += 1;
    }
}

uint32_t maml_map_len(Map* map) {
    return map->count;
}

// void* maml_map_next_active(Map* map, uint32_t* index_ptr, void** out_str_key) {
//     if (map->cap == 0 || map->count == 0) return nullptr;
    
//     bool is_str = map->is_string_key;
//     while (*index_ptr < map->cap) {
//         uint32_t current_idx = *index_ptr;
//         *index_ptr += 1;

//         unsigned char* entry = entry_at(map->entries, current_idx, map->val_size, is_str);

//         if (entry_status(entry) == EntryStatus::Occupied) {
//             if (is_str) {
//                 auto* header = reinterpret_cast<StrKeyHeader*>(entry);
//                 *out_str_key = const_cast<void*>(static_cast<const void*>(header->key_ptr));
//             } else {
//                 *out_str_key = nullptr;
//             }
//             return entry_value_ptr(entry, is_str);
//         }
//     }

//     return nullptr;
// }

Map maml_map_clone(Map* old_map) {
    Map new_header{};
    new_header.count = old_map->count;
    new_header.tombstone_count = old_map->tombstone_count;
    new_header.cap = old_map->cap;
    new_header.val_size = old_map->val_size;
    new_header.is_string_key = old_map->is_string_key;
    new_header.entries = nullptr;

    if (old_map->cap > 0 && old_map->entries != nullptr) {
        bool is_str = old_map->is_string_key;
        size_t esize = entry_size(old_map->val_size, is_str);
        size_t total_bytes = static_cast<size_t>(old_map->cap) * esize;
        
        void* new_entries = maml_alloc(total_bytes);
        if (new_entries == nullptr) {
            std::abort(); // OOM in map clone elements
        }
        
        std::memcpy(new_entries, old_map->entries, total_bytes);
        new_header.entries = new_entries;
    }

    return new_header;
}

void maml_map_free(Map* map) {
    if (map->cap > 0 && map->entries != nullptr) {
        maml_free(map->entries);
    }
}