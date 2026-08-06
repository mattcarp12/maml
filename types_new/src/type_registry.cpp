#include "type_registry.h"
#include "arena.h"

#include "ast.h"
#include "sym.h"
#include "types.h"
#include <algorithm>
#include <cassert>
#include <cstddef>
#include <cstdint>
#include <format>
#include <string>
#include <utility>
#include <variant>
#include <vector>

namespace maml::types {

TypeRegistry::TypeRegistry(Arena& arena)
    : arena_(arena)
{
    // Pre-allocate all primitive types so they are instantly available
    primitives_.resize(static_cast<size_t>(TypeKind::Unknown) + 1, nullptr);
    for (uint8_t i = 0; i <= static_cast<uint8_t>(TypeKind::Unknown); ++i) {
        auto* t = arena_.make<Type>();
        t->kind = static_cast<TypeKind>(i);
        t->payload = std::monostate {};
        primitives_[i] = t;
    }
}

const Type* TypeRegistry::getPrimitive(TypeKind kind)
{
    if (kind <= TypeKind::Unknown) {
        return primitives_[static_cast<uint8_t>(kind)];
    }
    return nullptr;
}

bool TypeRegistry::isPayloadEqual(const TypePayload& a, const TypePayload& b, TypeKind kind) const
{
    switch (kind) {
    case TypeKind::Array: {
        auto pa = std::get<ArrayPayload>(a);
        auto pb = std::get<ArrayPayload>(b);
        return pa.base == pb.base && pa.size == pb.size;
    }
    case TypeKind::Vector:
        return std::get<VectorPayload>(a).base == std::get<VectorPayload>(b).base;
    case TypeKind::Buffer:
        return std::get<BufferPayload>(a).base == std::get<BufferPayload>(b).base;
    case TypeKind::Future:
        return std::get<FuturePayload>(a).base == std::get<FuturePayload>(b).base;
    case TypeKind::View: {
        auto pa = std::get<ViewPayload>(a);
        auto pb = std::get<ViewPayload>(b);
        return pa.base == pb.base && pa.isMut == pb.isMut;
    }
    case TypeKind::Map: {
        auto pa = std::get<MapPayload>(a);
        auto pb = std::get<MapPayload>(b);
        return pa.key == pb.key && pa.value == pb.value;
    }
    case TypeKind::Struct:
        // Since names must be unique in scope, checking the SymID is sufficient for structs
        return std::get<StructPayload>(a).name == std::get<StructPayload>(b).name;
    case TypeKind::Sum: {
        auto pa = std::get<SumPayload>(a);
        auto pb = std::get<SumPayload>(b);

        // 1. Check the base name
        if (pa.baseName != pb.baseName) {
            return false;
        }

        // 2. Check that the generic arguments match exactly
        if (pa.typeArgs.size() != pb.typeArgs.size()) {
            return false;
        }
        for (size_t i = 0; i < pa.typeArgs.size(); ++i) {
            if (pa.typeArgs[i] != pb.typeArgs[i]) {
                return false;
            }
        }
        return true;
    }
    case TypeKind::Function: {
        auto pa = std::get<FunctionPayload>(a);
        auto pb = std::get<FunctionPayload>(b);
        return pa.returnType == pb.returnType && pa.params == pb.params && pa.caps == pb.caps;
    }
    default:
        return false;
    }
}

const Type* TypeRegistry::getVector(const Type* base)
{
    TypePayload p = VectorPayload { base };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Vector && isPayloadEqual(t->payload, p, TypeKind::Vector))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Vector;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getBuffer(const Type* base)
{
    TypePayload p = BufferPayload { base };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Buffer && isPayloadEqual(t->payload, p, TypeKind::Buffer))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Buffer;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getFuture(const Type* base)
{
    TypePayload p = FuturePayload { base };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Future && isPayloadEqual(t->payload, p, TypeKind::Future))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Future;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getMap(const Type* key, const Type* value)
{
    TypePayload p = MapPayload { key, value };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Map && isPayloadEqual(t->payload, p, TypeKind::Map))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Map;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getArray(const Type* base, int size)
{
    TypePayload p = ArrayPayload { base, size };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Array && isPayloadEqual(t->payload, p, TypeKind::Array))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Array;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getView(const Type* base, bool isMut)
{
    TypePayload p = ViewPayload { base, isMut };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::View && isPayloadEqual(t->payload, p, TypeKind::View))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::View;
    t->payload = p;
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getStruct(SymID name, std::vector<StructField> fields, bool isReprC)
{
    TypePayload p = StructPayload { name, std::move(fields), isReprC };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Struct && isPayloadEqual(t->payload, p, TypeKind::Struct))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Struct;
    t->payload = std::move(p);
    composites_.push_back(t);
    return t;
}

const Type* TypeRegistry::getSum(
    SymID baseName, std::vector<SumVariant> variants, std::vector<const Type*> typeArgs)
{
    TypePayload p = SumPayload { baseName, std::move(variants), std::move(typeArgs) };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Sum && isPayloadEqual(t->payload, p, TypeKind::Sum))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Sum;
    t->payload = std::move(p);
    composites_.push_back(t);
    return t;
}

void TypeRegistry::updateStruct(const Type* structType, std::vector<StructField> fields)
{
    // Safely cast away constness since the registry owns the Arena allocation
    Type* t = const_cast<Type*>(structType);
    if (t && t->kind == TypeKind::Struct) {
        auto& p = std::get<StructPayload>(t->payload);
        p.fields = std::move(fields);
    }
}

void TypeRegistry::updateSum(const Type* sumType, std::vector<SumVariant> variants)
{
    Type* t = const_cast<Type*>(sumType);
    if (t && t->kind == TypeKind::Sum) {
        auto& p = std::get<SumPayload>(t->payload);
        p.variants = std::move(variants);
    }
}

const Type* TypeRegistry::getFunction(
    std::vector<const Type*> params, std::vector<ast::Capability> caps, const Type* returnType)
{
    TypePayload p = FunctionPayload { std::move(params), std::move(caps), returnType };
    for (const auto* t : composites_) {
        if (t->kind == TypeKind::Function && isPayloadEqual(t->payload, p, TypeKind::Function))
            return t;
    }
    auto* t = arena_.make<Type>();
    t->kind = TypeKind::Function;
    t->payload = std::move(p);
    composites_.push_back(t);
    return t;
}

size_t TypeRegistry::getTypeSize(const Type* type)
{
    if (!type)
        return 0;

    switch (type->kind) {
    case TypeKind::I8:
    case TypeKind::U8:
    case TypeKind::Bool:
    case TypeKind::Char:
        return 1;

    case TypeKind::I16:
    case TypeKind::U16:
        return 2;

    case TypeKind::I32:
    case TypeKind::U32:
    case TypeKind::F32:
        return 4;

    case TypeKind::I64:
    case TypeKind::U64:
    case TypeKind::F64:
    case TypeKind::Ptr:
    case TypeKind::View:
    case TypeKind::Buffer:
        return 8; // Assuming a 64-bit target architecture

    case TypeKind::I128:
    case TypeKind::U128:
        return 16;

    case TypeKind::Unit:
    case TypeKind::Any:
    case TypeKind::Unknown:
        return 0;

    case TypeKind::Array: {
        const auto& payload = std::get<ArrayPayload>(type->payload);
        return payload.size * getTypeSize(payload.base);
    }

    case TypeKind::Struct: {
        const auto& payload = std::get<StructPayload>(type->payload);
        size_t totalSize = 0;
        for (const auto& field : payload.fields) {
            // Note: For simplicity, this sums field sizes directly.
            // If your ABI requires strict struct field alignment padding,
            // align 'totalSize' to field alignment before adding.
            totalSize += getTypeSize(field.type);
        }
        return totalSize;
    }

    case TypeKind::Sum: {
        // The size of a sum type is sizeof(i32) + max payload size
        const auto& payload = std::get<SumPayload>(type->payload);
        size_t maxPayloadSize = 0;

        for (const auto& variant : payload.variants) {
            size_t variantSize = 0;
            for (const auto* tupleTy : variant.tupleTypes) {
                variantSize += getTypeSize(tupleTy);
            }
            for (const auto& field : variant.fields) {
                variantSize += getTypeSize(field.type);
            }
            maxPayloadSize = std::max(maxPayloadSize, variantSize);
        }
        return 4 + maxPayloadSize; // 4 bytes for discriminant + payload
    }

    default:
        return 8; // Fallback default for handles/pointers
    }
}

} // namespace maml::types
