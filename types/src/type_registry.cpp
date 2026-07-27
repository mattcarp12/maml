#include "type_registry.h"

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
    case TypeKind::Sum:
        return std::get<SumPayload>(a).baseName == std::get<SumPayload>(b).baseName;
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

const Type* TypeRegistry::getOption(const Type* base, SymbolTable& sym)
{
    // Emulates the NewOptionType logic
    SymID someId = sym.intern("Some");
    SymID noneId = sym.intern("None");
    SymID optId = sym.intern("Option");

    std::vector<SumVariant> variants = { { someId, 0, { base }, {} }, { noneId, 1, {}, {} } };
    return getSum(optId, std::move(variants), { base });
}

const Type* TypeRegistry::getResult(const Type* val, const Type* err, SymbolTable& sym)
{
    // Emulates the NewResultType logic
    SymID okId = sym.intern("Ok");
    SymID errId = sym.intern("Err");
    SymID resId = sym.intern("Result");

    std::vector<SumVariant> variants = { { okId, 0, { val }, {} }, { errId, 1, { err }, {} } };
    return getSum(resId, std::move(variants), { val, err });
}

} // namespace maml::types