#pragma once
#include "types.h"
#include <cstddef>
#include <vector>

namespace maml::types {

struct TypeLayout {
    size_t size;
    size_t alignment;
};

class TargetABI {
public:
    // 1. Primitive Scalar Size & Alignment (64-bit target)
    static constexpr TypeLayout getScalarLayout(TypeKind kind)
    {
        switch (kind) {
        case TypeKind::I8:
        case TypeKind::U8:
        case TypeKind::Bool:
            return { .size = 1, .alignment = 1 };

        case TypeKind::I16:
        case TypeKind::U16:
            return { .size = 2, .alignment = 2 };

        case TypeKind::I32:
        case TypeKind::U32:
        case TypeKind::F32:
        case TypeKind::Char: // 4 bytes: 32-bit Unicode Scalar Value
            return { .size = 4, .alignment = 4 };

        case TypeKind::I64:
        case TypeKind::U64:
        case TypeKind::F64:
        case TypeKind::I128:
        case TypeKind::U128:
        case TypeKind::Ptr:
        case TypeKind::Buffer:
        case TypeKind::Any:
        case TypeKind::Function:
        case TypeKind::Future:
            return { .size = 8, .alignment = 8 };

        default:
            return { .size = 0, .alignment = 1 };
        }
    }

    // Helper: Align an offset to a field boundary
    static constexpr size_t alignTo(size_t offset, size_t alignment)
    {
        return (offset + alignment - 1) & ~(alignment - 1);
    }

    // 2. Single Source of Truth for Builtin Container Field Types
    //    Returns ordered list of TypeKinds representing the underlying struct fields.
    static std::vector<TypeKind> getBuiltinContainerFields(TypeKind containerKind)
    {
        switch (containerKind) {
        case TypeKind::View:
            // { ptr, len }
            return { TypeKind::Ptr, TypeKind::I64 };

        case TypeKind::String:
            // { ptr, len, is_owned }
            return { TypeKind::Ptr, TypeKind::I64, TypeKind::Bool };

        case TypeKind::Vector:
            // { buffer_ptr, cap, len, elem_size }
            return { TypeKind::Ptr, TypeKind::U32, TypeKind::U32, TypeKind::U32 };

        case TypeKind::Map:
            // { entries_ptr, count, tombstone_count, cap, val_size, is_string_key }
            return { TypeKind::Ptr, TypeKind::U32, TypeKind::U32, TypeKind::U32, TypeKind::U32,
                TypeKind::Bool };

        default:
            return {};
        }
    }
};

} // namespace maml::types