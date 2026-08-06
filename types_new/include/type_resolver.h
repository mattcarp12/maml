#pragma once
#include "sym.h"
#include "type_registry.h"
#include "types.h"

// TODO(you): this file needs your real ast.h to finish. TypeRegistry only
// knows how to build canonical types from a *structural* description
// (base type, size, key/value, ...). TypeResolver is the thing that reads
// *syntax* — an ast::TypeExpr written by the programmer, e.g. `[]mut i32`
// or `Map<string, User>` — and turns it into calls against TypeRegistry.
//
// Once ast.h is available, `resolve()` below would look roughly like:
//
//   const Type* TypeResolver::resolve(const ast::TypeExpr& expr) {
//       switch (expr.kind) {
//       case ast::TypeExprKind::Named:
//           return resolveNamed(static_cast<const ast::NamedTypeExpr&>(expr));
//       case ast::TypeExprKind::Array:
//           return resolveArray(static_cast<const ast::ArrayTypeExpr&>(expr));
//       // ...
//       }
//   }
//
// with lookups against `sym_` for named types, diagnostics reported through
// a Diagnostics& for unresolvable names, and everything else forwarded to
// registry_.getX(...).

namespace maml::types {

class TypeResolver {
public:
    TypeResolver(TypeRegistry& registry, SymbolTable& sym)
        : registry_(registry)
        , sym_(sym)
    {
    }

    // const Type* resolve(const ast::TypeExpr& expr);

private:
    TypeRegistry& registry_;
    SymbolTable& sym_;
};

} // namespace maml::types
