#include "builder.h"

namespace maml::mir {

// =============================================================================
// Runtime Function Emitters
// =============================================================================
// These helpers generate the CallInst nodes mapped to the external C ABI
// provided by the maml runtime library.

Value Builder::EmitMamlVecPush(Value vec, Value element, Position pos)
{
    return emitRuntimeCall(sym_.intern("maml_vec_push"), reg_.getPrimitive(types::TypeKind::Unit),
        { vec, element }, pos);
}

Value Builder::EmitMamlVecGet(Value vec, Value index, Position pos)
{
    return emitRuntimeCall(
        sym_.intern("maml_vec_get"), reg_.getPrimitive(types::TypeKind::Ptr), { vec, index }, pos);
}

Value Builder::EmitMamlVecSet(Value vec, Value index, Value element, Position pos)
{
    return emitRuntimeCall(sym_.intern("maml_vec_set"), reg_.getPrimitive(types::TypeKind::Unit),
        { vec, index, element }, pos);
}

Value Builder::EmitMamlMapPut(
    Value m, Value key_hash, Value key_ptr, Value key_len, Value val_ptr, Position pos)
{
    return emitRuntimeCall(sym_.intern("maml_map_put"), reg_.getPrimitive(types::TypeKind::Unit),
        { m, key_hash, key_ptr, key_len, val_ptr }, pos);
}

Value Builder::EmitMamlMapGet(Value m, Value key_hash, Value key_ptr, Value key_len, Position pos)
{
    return emitRuntimeCall(sym_.intern("maml_map_get"), reg_.getPrimitive(types::TypeKind::Ptr),
        { m, key_hash, key_ptr, key_len }, pos);
}

// =============================================================================
// Layout Helpers
// =============================================================================

// Simplified SizeOf estimator for the ABI structs.
// In a full implementation, this bridges to layout.go logic.
int sizeOfDynamic(const types::Type* t, const Target& target)
{
    if (!t)
        return 0;
    switch (t->kind) {
    case types::TypeKind::I8:
    case types::TypeKind::U8:
    case types::TypeKind::Bool:
        return 1;
    case types::TypeKind::I16:
    case types::TypeKind::U16:
        return 2;
    case types::TypeKind::I32:
    case types::TypeKind::U32:
    case types::TypeKind::F32:
    case types::TypeKind::Char:
        return 4;
    case types::TypeKind::I64:
    case types::TypeKind::U64:
    case types::TypeKind::F64:
        return 8;
    case types::TypeKind::I128:
    case types::TypeKind::U128:
        return 16;
    case types::TypeKind::String:
        return 16; // STRING_SIZE
    case types::TypeKind::Vector:
        return 24; // VECTOR_SIZE
    case types::TypeKind::View:
        return 16; // VIEW_SIZE
    case types::TypeKind::Map:
        return 32; // MAP_SIZE
    case types::TypeKind::Ptr:
        return target.pointerSize;
    default:
        return target.pointerSize; // Fallback for composites
    }
}

// =============================================================================
// Composite Literal Lowering (Vec and Map)
// =============================================================================

// This logic maps directly from frontend/mir/vec.go.
// Call this from the CompositeLiteral branch in builder_lower_expr.cpp when resolvedType is Vector.
Value Builder::lowerVecLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType)
{
    SymID tmp = emitTemp(resolvedType);
    Value obj = Register { tmp, resolvedType, e->pos };

    const types::Type* baseType = std::get<types::VectorPayload>(resolvedType->payload).base;

    // Inline construct the { buffer, cap, len, elem_size } struct directly
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos }, sym_.intern("buffer"),
        0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::U32), e->pos }, sym_.intern("cap"), 1,
        reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::U32), e->pos }, sym_.intern("len"), 2,
        reg_.getPrimitive(types::TypeKind::U32), e->pos);

    int elemSize = sizeOfDynamic(baseType, target_);
    storeField(obj, resolvedType,
        IntConstant { elemSize, reg_.getPrimitive(types::TypeKind::U32), e->pos },
        sym_.intern("elem_size"), 3, reg_.getPrimitive(types::TypeKind::U32), e->pos);

    if (!e->elements.empty()) {
        Value vecPtrReg = emitBorrow(tmp, true, e->pos);

        for (const auto& elem : e->elements) {
            Value flatElem = lowerExpr(elem.value);
            Value boxedElem = boxScalar(flatElem, baseType, elem.pos);
            EmitMamlVecPush(vecPtrReg, boxedElem, elem.pos);
        }
    }

    return obj;
}

// This logic maps directly from frontend/mir/map.go.
// Call this from the CompositeLiteral branch in builder_lower_expr.cpp when resolvedType is Map.
Value Builder::lowerMapLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType)
{
    SymID tmp = emitTemp(resolvedType);
    Value obj = Register { tmp, resolvedType, e->pos };

    const types::Type* keyType = std::get<types::MapPayload>(resolvedType->payload).key;
    const types::Type* valType = std::get<types::MapPayload>(resolvedType->payload).value;

    bool isStrKey = (keyType->kind == types::TypeKind::String);

    // Inline Initialization
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos }, sym_.intern("entries"),
        0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::U32), e->pos }, sym_.intern("count"), 1,
        reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::U32), e->pos },
        sym_.intern("tombstone_count"), 2, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::U32), e->pos }, sym_.intern("cap"), 3,
        reg_.getPrimitive(types::TypeKind::U32), e->pos);

    int valSize = sizeOfDynamic(valType, target_);
    storeField(obj, resolvedType,
        IntConstant { valSize, reg_.getPrimitive(types::TypeKind::U32), e->pos },
        sym_.intern("val_size"), 4, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        BoolConstant { isStrKey, reg_.getPrimitive(types::TypeKind::Bool), e->pos },
        sym_.intern("is_string_key"), 5, reg_.getPrimitive(types::TypeKind::Bool), e->pos);

    if (!e->elements.empty()) {
        Value mapPtrReg = emitBorrow(tmp, true, e->pos);

        for (const auto& kv : e->elements) {
            Value flatVal = lowerExpr(kv.value);
            auto [hashVal, ptrVal, lenVal] = lowerMapKey(kv.key, kv.pos);
            Value boxedVal = boxScalar(flatVal, valType, kv.pos);
            EmitMamlMapPut(mapPtrReg, hashVal, ptrVal, lenVal, boxedVal, kv.pos);
        }
    }

    return obj;
}

} // namespace maml::mir