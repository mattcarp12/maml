// ir.h
//
// These nodes are never produced by the parser — they're introduced by
// LoweringPass. Keeping them out of ast.h keeps the parser AST a faithful
// representation of source syntax. IR nodes are semantic/codegen-facing,
// so (unlike ast_*.h) it's fine for them to carry resolved Type* directly
// rather than routing through a side table.
#pragma once

#include "node.h"
#include "sym.h"
#include "token.h"

#include <variant>
#include <vector>

namespace maml::types {
struct Type;
}

namespace maml::ast {
struct Identifier; // reused as-is from the parser AST where convenient
}

namespace maml::ir {

struct TaggedUnionConstructExpr;
struct TaggedUnionAccessExpr;
struct IntrinsicCallExpr;
struct CastExpr;

// IR exprs can embed a lowered subtree or fall back to an original ast::Expr
// leaf (e.g. Identifier*) that lowering left untouched.
using IRExpr = std::variant<std::monostate, TaggedUnionConstructExpr*, TaggedUnionAccessExpr*,
    IntrinsicCallExpr*, CastExpr*>;

struct TaggedUnionConstructExpr : ast::NodeBase {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr; // lowered tagged-union layout struct
    int discriminant = 0;
    std::vector<IRExpr> payloadArgs;
    const maml::types::Type* payloadStructType = nullptr;
};

struct TaggedUnionAccessExpr : ast::NodeBase {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr;
    IRExpr object;
    int fieldIndex = 0;
    const maml::types::Type* payloadStructType = nullptr;
};

struct IntrinsicCallExpr : ast::NodeBase {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr;
    SymID intrinsicSym;
    std::vector<IRExpr> arguments;
};

struct CastExpr : ast::NodeBase {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr;
    IRExpr source;
    const maml::types::Type* targetType = nullptr;
};

} // namespace maml::ir
