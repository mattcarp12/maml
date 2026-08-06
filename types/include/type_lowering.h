#pragma once
#include "sym.h"
#include "type_registry.h"
#include "types.h"

namespace maml::types {

// TypeLowering builds language-level / derived types on top of a plain
// TypeRegistry: Option<T>, Result<T, E>, and the concrete memory layout of
// a Sum type. TypeRegistry itself stays ignorant of what "Option" means —
// it only knows how to canonicalize structural descriptions. This is where
// that language-specific knowledge lives instead.
class TypeLowering {
public:
    TypeLowering(TypeRegistry& registry, SymbolTable& sym)
        : registry_(registry)
        , sym_(sym)
    {
    }

    const Type* getOption(const Type* base);
    const Type* getResult(const Type* val, const Type* err);

    // Computes the { discriminant: i32, payload: [N x u8] } struct layout
    // for a Sum type — used when lowering sums down to a concrete ABI.
    const Type* getTaggedUnionLayout(const Type* sumType);

private:
    TypeRegistry& registry_;
    SymbolTable& sym_;
};

} // namespace maml::types
