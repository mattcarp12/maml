#include "type_lowering.h"
#include "types.h"
#include <algorithm>
#include <cassert>
#include <utility>

namespace maml::types {

const Type* TypeLowering::getOption(const Type* base)
{
    // Emulates the NewOptionType logic
    SymID someId = sym_.intern("Some");
    SymID noneId = sym_.intern("None");
    SymID optId = sym_.intern("Option");

    std::vector<SumVariant> variants = { { someId, 0, { base }, {} }, { noneId, 1, {}, {} } };
    return registry_.getSum(optId, std::move(variants), { base });
}

const Type* TypeLowering::getResult(const Type* val, const Type* err)
{
    // Emulates the NewResultType logic
    SymID okId = sym_.intern("Ok");
    SymID errId = sym_.intern("Err");
    SymID resId = sym_.intern("Result");

    std::vector<SumVariant> variants = { { okId, 0, { val }, {} }, { errId, 1, { err }, {} } };
    return registry_.getSum(resId, std::move(variants), { val, err });
}

const Type* TypeLowering::getTaggedUnionLayout(const Type* sumType)
{
    assert(sumType && sumType->kind == TypeKind::Sum && "Expected a Sum Type");
    const auto& sumPayload = std::get<SumPayload>(sumType->payload);

    // 1. Calculate the maximum payload size across all variants
    size_t maxPayloadSize = 0;
    for (const auto& variant : sumPayload.variants) {
        size_t variantSize = 0;
        for (const auto* tupleTy : variant.tupleTypes) {
            variantSize += registry_.getTypeSize(tupleTy);
        }
        for (const auto& field : variant.fields) {
            variantSize += registry_.getTypeSize(field.type);
        }
        maxPayloadSize = std::max(maxPayloadSize, variantSize);
    }

    // 2. Build the structural fields: { discriminant: i32, payload: [maxPayloadSize x u8] }
    std::vector<StructField> fields;

    // Field 0: discriminant
    fields.push_back(
        { .name = sym_.intern("discriminant"), .type = registry_.getPrimitive(TypeKind::I32) });

    // Field 1: payload array (only add if maxPayloadSize > 0)
    if (maxPayloadSize > 0) {
        const Type* u8Type = registry_.getPrimitive(TypeKind::U8);
        const Type* payloadArrayType = registry_.getArray(u8Type, static_cast<int>(maxPayloadSize));

        fields.push_back({ .name = sym_.intern("payload"), .type = payloadArrayType });
    }

    // 3. Generate a descriptive interned name for the layout struct
    SymID layoutName = sym_.intern(sumType->toString(sym_));

    // 4. Create and return the interned structural struct
    return registry_.getStruct(layoutName, std::move(fields), /*isReprC=*/true);
}

} // namespace maml::types
