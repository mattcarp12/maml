#pragma once

#include "ast.h"
#include "diagnostics.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"
#include <string_view>

namespace maml::types {

class TypeResolver {
public:
    TypeResolver(TypeRegistry& registry, SymbolTable& sym)
        : registry_(registry)
        , sym_(sym)
    {
    }
    const Type* resolve(const ast::TypeExpr& expr, Diagnostics& diags);

private:
    TypeRegistry& registry_;
    SymbolTable& sym_;
    const Type* resolvePrimitive(std::string_view name);
};

} // namespace maml::types