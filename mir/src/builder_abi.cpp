#include "ast.h"
#include "builder.h"
#include "mir.h"
#include "sym.h"
#include "token.h"
#include "types.h"
#include <variant>

namespace maml::mir {

// =============================================================================
// Runtime Function Emitters
// =============================================================================

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
// Composite Literal Lowering (Vec and Map)
// =============================================================================

Value Builder::lowerVecLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType)
{
    SymID tmp = emitTemp(resolvedType);
    push(AllocaInst { .dst = tmp, .type = resolvedType, .pos = e->pos });
    Value obj = Register { .name = tmp, .type = resolvedType, .pos = e->pos };

    const types::Type* baseType = std::get<types::VectorPayload>(resolvedType->payload).base;

    // Inline construct the { buffer, cap, len, elem_size } struct directly
    storeField(obj, resolvedType,
        Register { .name = sym_.intern("null"),
            .type = reg_.getPrimitive(types::TypeKind::Ptr),
            .pos = e->pos },
        sym_.intern("buffer"), 0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
    storeField(obj, resolvedType,
        IntConstant { .value = 0, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("cap"), 1, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { .value = 0, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("len"), 2, reg_.getPrimitive(types::TypeKind::U32), e->pos);

    // Phase 5: Query type size directly from TypeRegistry
    int elemSize = static_cast<int>(reg_.getTypeSize(baseType));
    storeField(obj, resolvedType,
        IntConstant {
            .value = elemSize, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("elem_size"), 3, reg_.getPrimitive(types::TypeKind::U32), e->pos);

    if (!e->elements.empty()) {
        Value vecPtrReg = emitBorrow(tmp, true, e->pos);

        for (const auto& elem : e->elements) {
            Value flatElem = lowerExpr(elem.value);
            Value boxedElem;
            if (baseType && baseType->isAggregate()) {
                if (auto* reg = std::get_if<Register>(&flatElem)) {
                    boxedElem = emitBorrow(reg->name, true, elem.pos);
                } else {
                    boxedElem = boxScalar(flatElem, baseType, elem.pos);
                }
            } else {
                boxedElem = boxScalar(flatElem, baseType, elem.pos);
            }
            EmitMamlVecPush(vecPtrReg, boxedElem, elem.pos);
        }
    }

    return obj;
}

Value Builder::lowerMapLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType)
{
    SymID tmp = emitTemp(resolvedType);
    push(AllocaInst { .dst = tmp, .type = resolvedType, .pos = e->pos });
    Value obj = Register { .name = tmp, .type = resolvedType, .pos = e->pos };

    const types::Type* keyType = std::get<types::MapPayload>(resolvedType->payload).key;
    const types::Type* valType = std::get<types::MapPayload>(resolvedType->payload).value;

    bool isStrKey = (keyType->kind == types::TypeKind::String);

    // Inline Initialization
    storeField(obj, resolvedType,
        Register { .name = sym_.intern("null"),
            .type = reg_.getPrimitive(types::TypeKind::Ptr),
            .pos = e->pos },
        sym_.intern("entries"), 0, reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
    storeField(obj, resolvedType,
        IntConstant { .value = 0, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("count"), 1, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { .value = 0, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("tombstone_count"), 2, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        IntConstant { .value = 0, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("cap"), 3, reg_.getPrimitive(types::TypeKind::U32), e->pos);

    // Phase 5: Query type size directly from TypeRegistry
    int valSize = static_cast<int>(reg_.getTypeSize(valType));
    storeField(obj, resolvedType,
        IntConstant {
            .value = valSize, .type = reg_.getPrimitive(types::TypeKind::U32), .pos = e->pos },
        sym_.intern("val_size"), 4, reg_.getPrimitive(types::TypeKind::U32), e->pos);
    storeField(obj, resolvedType,
        BoolConstant {
            .value = isStrKey, .type = reg_.getPrimitive(types::TypeKind::Bool), .pos = e->pos },
        sym_.intern("is_string_key"), 5, reg_.getPrimitive(types::TypeKind::Bool), e->pos);

    if (!e->elements.empty()) {
        Value mapPtrReg = emitBorrow(tmp, true, e->pos);

        for (const auto& kv : e->elements) {
            Value flatVal = lowerExpr(kv.value);
            auto [hashVal, ptrVal, lenVal] = lowerMapKey(kv.key, kv.pos);
            Value boxedVal;
            if (valType && valType->isAggregate()) {
                if (auto* reg = std::get_if<Register>(&flatVal)) {
                    boxedVal = emitBorrow(reg->name, true, kv.pos);
                } else {
                    boxedVal = boxScalar(flatVal, valType, kv.pos);
                }
            } else {
                boxedVal = boxScalar(flatVal, valType, kv.pos);
            }
            EmitMamlMapPut(mapPtrReg, hashVal, ptrVal, lenVal, boxedVal, kv.pos);
        }
    }

    return obj;
}

Value Builder::lowerSumTypeLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType)
{
    SymID variantName = NoSymbol;
    if (auto* namedTy = std::get_if<ast::NamedTypeExpr*>(&e->typeExpr)) {
        if (namedTy && *namedTy && (*namedTy)->name) {
            variantName = (*namedTy)->name->name;
        }
    }
    const auto& sumPayload = std::get<types::SumPayload>(resolvedType->payload);
    for (const auto& variant : sumPayload.variants) {
        if (variant.name == variantName) {
            SymID tmp = emitTemp(resolvedType);
            push(AllocaInst { .dst = tmp, .type = resolvedType, .pos = e->pos });
            Value basePtr = emitBorrow(tmp, true, e->pos);

            const types::Type* i32Type = reg_.getPrimitive(types::TypeKind::I32);
            Value discVal
                = IntConstant { .value = variant.discriminant, .type = i32Type, .pos = e->pos };
            storeField(
                basePtr, resolvedType, discVal, sym_.intern("discriminant"), 0, i32Type, e->pos);

            if (!e->elements.empty()) {
                Value rawPayloadAddr = emitFieldAddr(basePtr, resolvedType, sym_.intern("payload"),
                    1, reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                SymID castTmp = newPtrTemp();
                push(BitcastPtrInst { .dst = castTmp,
                    .src = rawPayloadAddr,
                    .type = reg_.getPrimitive(types::TypeKind::Ptr),
                    .pos = e->pos });
                Value typedPayloadPtr = Register {
                    .name = castTmp, .type = reg_.getPrimitive(types::TypeKind::Ptr), .pos = e->pos
                };

                const types::Type* payloadStructType = reg_.getStruct(variant.name, variant.fields);

                for (size_t i = 0; i < e->elements.size(); ++i) {
                    const auto& elem = e->elements[i];
                    int fieldIdx = -1;
                    SymID fieldName = NoSymbol;
                    const types::Type* fieldType = nullptr;

                    if (auto idPtr = std::get_if<ast::Identifier*>(&elem.key)) {
                        fieldName = (*idPtr)->name;
                        for (size_t j = 0; j < variant.fields.size(); ++j) {
                            if (variant.fields[j].name == fieldName) {
                                fieldIdx = static_cast<int>(j);
                                fieldType = variant.fields[j].type;
                                break;
                            }
                        }
                    }

                    if (fieldIdx >= 0 && fieldType) {
                        Value flatVal = lowerExpr(elem.value);
                        storeField(typedPayloadPtr, payloadStructType, flatVal, fieldName, fieldIdx,
                            fieldType, elem.pos);
                    }
                }
            }
            return Register { .name = tmp, .type = resolvedType, .pos = e->pos };
        }
    }

    return std::monostate {}; // <-- ADD THIS: Fallback return value when no variant matches
}

} // namespace maml::mir