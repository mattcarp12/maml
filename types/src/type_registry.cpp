#include "type_registry.h"
#include "arena.h"

#include "capability.h"
#include "sym.h"
#include "type_layout.h"
#include "types.h"
#include <algorithm>
#include <cassert>
#include <cstddef>
#include <cstdint>
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

// type_registry.cpp (isPayloadEqual updated snippet)
bool TypeRegistry::isPayloadEqual(const TypePayload& a, const TypePayload& b, TypeKind kind) const
{
    switch (kind) {
    case TypeKind::Array: {
        const auto& pa = std::get<ArrayPayload>(a);
        const auto& pb = std::get<ArrayPayload>(b);
        return pa.base == pb.base && pa.size == pb.size;
    }
    case TypeKind::Vector:
        return std::get<VectorPayload>(a).base == std::get<VectorPayload>(b).base;
    case TypeKind::Buffer:
        return std::get<BufferPayload>(a).base == std::get<BufferPayload>(b).base;
    case TypeKind::Future:
        return std::get<FuturePayload>(a).base == std::get<FuturePayload>(b).base;
    case TypeKind::View: {
        const auto& pa = std::get<ViewPayload>(a);
        const auto& pb = std::get<ViewPayload>(b);
        return pa.base == pb.base && pa.isMut == pb.isMut;
    }
    case TypeKind::Map: {
        const auto& pa = std::get<MapPayload>(a);
        const auto& pb = std::get<MapPayload>(b);
        return pa.key == pb.key && pa.value == pb.value;
    }
    case TypeKind::Struct:
        return std::get<StructPayload>(a).name == std::get<StructPayload>(b).name;
    case TypeKind::Sum: {
        const auto& pa = std::get<SumPayload>(a);
        const auto& pb = std::get<SumPayload>(b);

        if (pa.baseName != pb.baseName || pa.typeArgs.size() != pb.typeArgs.size()) {
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
        const auto& pa = std::get<FunctionPayload>(a);
        const auto& pb = std::get<FunctionPayload>(b);
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
    std::vector<const Type*> params, std::vector<Capability> caps, const Type* returnType)
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

// Internal helper to calculate size AND alignment
static TypeLayout getLayout(const Type* type, TypeRegistry& reg)
{
    if (!type)
        return { 0, 1 };

    switch (type->kind) {
    case TypeKind::Array: {
        const auto& p = std::get<ArrayPayload>(type->payload);
        TypeLayout base = getLayout(p.base, reg);
        return { .size = p.size * base.size, .alignment = base.alignment };
    }

    case TypeKind::Struct: {
        const auto& p = std::get<StructPayload>(type->payload);
        size_t offset = 0;
        size_t maxAlign = 1;

        for (const auto& field : p.fields) {
            TypeLayout fl = getLayout(field.type, reg);
            offset = TargetABI::alignTo(offset, fl.alignment);
            offset += fl.size;
            maxAlign = std::max(maxAlign, fl.alignment);
        }
        // Final struct size must be a multiple of its maximum field alignment
        return { .size = TargetABI::alignTo(offset, maxAlign), .alignment = maxAlign };
    }

    case TypeKind::String:
    case TypeKind::View:
    case TypeKind::Vector:
    case TypeKind::Map: {
        // Derive layout directly from the canonical container schema
        auto fields = TargetABI::getBuiltinContainerFields(type->kind);
        size_t offset = 0;
        size_t maxAlign = 1;

        for (TypeKind fk : fields) {
            TypeLayout fl = TargetABI::getScalarLayout(fk);
            offset = TargetABI::alignTo(offset, fl.alignment);
            offset += fl.size;
            maxAlign = std::max(maxAlign, fl.alignment);
        }
        return { .size = TargetABI::alignTo(offset, maxAlign), .alignment = maxAlign };
    }

    case TypeKind::Sum: {
        const auto& p = std::get<SumPayload>(type->payload);
        size_t maxPayloadSize = 0;
        size_t maxAlign = 4; // At least i32 for discriminant

        for (const auto& variant : p.variants) {
            size_t varSize = 0;
            for (const auto* tupleTy : variant.tupleTypes) {
                TypeLayout tl = getLayout(tupleTy, reg);
                varSize = TargetABI::alignTo(varSize, tl.alignment) + tl.size;
                maxAlign = std::max(maxAlign, tl.alignment);
            }
            for (const auto& field : variant.fields) {
                TypeLayout fl = getLayout(field.type, reg);
                varSize = TargetABI::alignTo(varSize, fl.alignment) + fl.size;
                maxAlign = std::max(maxAlign, fl.alignment);
            }
            maxPayloadSize = std::max(maxPayloadSize, varSize);
        }
        size_t total = TargetABI::alignTo(4, maxAlign) + maxPayloadSize;
        return { .size = TargetABI::alignTo(total, maxAlign), .alignment = maxAlign };
    }

    default:
        return TargetABI::getScalarLayout(type->kind);
    }
}

size_t TypeRegistry::getTypeSize(const Type* type) { return getLayout(type, *this).size; }

} // namespace maml::types
