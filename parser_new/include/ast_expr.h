// ast_expr.h
#pragma once

#include "ast_common.h"
#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <cstdint>
#include <string_view>
#include <vector>

namespace maml::ast {

struct Identifier : NodeBase {
    Position pos;
    Position end;
    SymID name;
};

struct IntLiteral : NodeBase {
    Position pos;
    Position end;
    int64_t value;
};

struct BoolLiteral : NodeBase {
    Position pos;
    Position end;
    bool value;
};

struct StringLiteral : NodeBase {
    Position pos;
    Position end;
    std::string_view value;
};

struct InfixExpr : NodeBase {
    Position pos;
    Position end;
    Expr left;
    TokenType op;
    Expr right;
};

struct PrefixExpr : NodeBase {
    Position pos;
    Position end;
    TokenType op;
    Expr right;
};

struct CallExpr : NodeBase {
    Position pos;
    Position end;
    Expr function;
    std::vector<CallArg> arguments;
};

struct IfExpr : NodeBase {
    Position pos;
    Position end;
    Expr condition;
    BlockStmt* consequence;
    BlockStmt* alternative;
    // Reachability, types, etc. live in CompilerContext side tables keyed by `id`.
};

struct MatchExpr : NodeBase {
    Position pos;
    Position end;
    Expr subject;
    std::vector<MatchArm> arms;
};

struct AwaitExpr : NodeBase {
    Position pos;
    Position end;
    Expr value;
};

struct SpawnExpr : NodeBase {
    Position pos;
    Position end;
    CallExpr* value;
};

struct CompositeLiteral : NodeBase {
    Position pos;
    Position end;
    TypeExpr typeExpr;
    std::vector<CompositeElement> elements;
};

struct FieldAccess : NodeBase {
    Position pos;
    Position end;
    Expr object;
    Identifier* field;
};

struct IndexExpr : NodeBase {
    Position pos;
    Position end;
    Expr left;
    Expr index;
};

struct SliceExpr : NodeBase {
    Position pos;
    Position end;
    Expr left;
    Expr low;
    Expr high;
};

struct TypeExprWrapper : NodeBase {
    Position pos;
    Position end;
    TypeExpr typeExpr;
};

} // namespace maml::ast
