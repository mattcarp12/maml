#pragma once
#include "arena.h"
#include "ast.h"
#include "sym.h"
#include "types.h"
#include <cstddef>
#include <vector>

namespace maml::types {

class TypeRegistry {
public:
    explicit TypeRegistry(Arena& arena);

    // Primitives
    const Type* getPrimitive(TypeKind kind);

    // Single-type wrappers
    const Type* getVector(const Type* base);
    const Type* getBuffer(const Type* base);
    const Type* getFuture(const Type* base);
    const Type* getArray(const Type* base, int size);
    const Type* getView(const Type* base, bool isMut);

    // Multi-type wrappers
    const Type* getMap(const Type* key, const Type* value);

    // User defined
    const Type* getStruct(SymID name, std::vector<StructField> fields, bool isReprC = false);
    const Type* getSum(
        SymID baseName, std::vector<SumVariant> variants, std::vector<const Type*> typeArgs = {});
    void updateStruct(const Type* structType, std::vector<StructField> fields);
    void updateSum(const Type* sumType, std::vector<SumVariant> variants);
    const Type* getFunction(
        std::vector<const Type*> params, std::vector<ast::Capability> caps, const Type* returnType);

    // Helpers
    const Type* getOption(const Type* base, SymbolTable& sym);
    const Type* getResult(const Type* val, const Type* err, SymbolTable& sym);
    const Type* getTaggedUnionLayout(const Type* sumType, SymbolTable& sym);
    size_t getTypeSize(const Type* type);

private:
    Arena& arena_;

    // Pre-allocated primitives for O(1) access
    std::vector<const Type*> primitives_;

    // Interned composites. A linear search is used here for simplicity and small code size.
    // In a production compiler with millions of types, this would be backed by a hash map
    // with a custom std::hash<TypePayload>.
    std::vector<const Type*> composites_;

    bool isPayloadEqual(const TypePayload& a, const TypePayload& b, TypeKind kind) const;
};

} // namespace maml::types