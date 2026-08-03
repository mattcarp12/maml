#pragma once
#include "ast_nodes.h"
#include "sym.h"
#include <cstdint>
#include <string>
#include <variant>
#include <vector>

namespace maml::types {

enum class TypeKind : uint8_t {
    // Primitives
    I8,
    I16,
    I32,
    I64,
    I128,
    U8,
    U16,
    U32,
    U64,
    U128,
    F32,
    F64,
    Bool,
    Char,
    Unit,
    String,
    Ptr,
    Any,
    Unknown,

    // Composites & Containers
    Buffer,
    Array,
    View,
    Vector,
    Map,
    Future,
    Struct,
    Sum,
    Function
};

// Forward declare the main Type wrapper
struct Type;

// --- Payloads for Complex Types ---
struct ArrayPayload {
    const Type* base;
    int size;
};
struct ViewPayload {
    const Type* base;
    bool isMut;
};
struct VectorPayload {
    const Type* base;
};
struct MapPayload {
    const Type* key;
    const Type* value;
};
struct FuturePayload {
    const Type* base;
};
struct BufferPayload {
    const Type* base;
}; // e.g., Buffer<u8>

struct StructField {
    SymID name;
    const Type* type;
};
struct StructPayload {
    SymID name;
    std::vector<StructField> fields;
    bool isReprC;
};

struct SumVariant {
    SymID name;
    int discriminant;
    std::vector<const Type*> tupleTypes;
    std::vector<StructField> fields;
};
struct SumPayload {
    SymID baseName;
    std::vector<SumVariant> variants;
    std::vector<const Type*> typeArgs;
};

struct FunctionPayload {
    std::vector<const Type*> params;
    std::vector<ast::Capability> caps;
    const Type* returnType;
};

using TypePayload = std::variant<std::monostate, // Used for all primitives
    ArrayPayload, ViewPayload, VectorPayload, MapPayload, FuturePayload, BufferPayload,
    StructPayload, SumPayload, FunctionPayload>;

// --- The Unified Type Struct ---
struct Type {
    TypeKind kind;
    TypePayload payload;

    // Helper methods
    [[nodiscard]] bool isInteger() const;
    [[nodiscard]] bool canRepresentInt(int64_t value) const;
    [[nodiscard]] bool isCopyable() const;

    [[nodiscard]] std::string toString(const SymbolTable& sym) const;
    [[nodiscard]] std::string mangledName(const SymbolTable& sym) const;
};

} // namespace maml::types