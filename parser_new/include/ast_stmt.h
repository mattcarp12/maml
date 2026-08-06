// ast_stmt.h
#pragma once

#include "ast_common.h" // for Capability (AliasDecl)
#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <vector>

namespace maml::ast {

struct BlockStmt : NodeBase {
    Position pos;
    Position end;
    std::vector<Stmt> statements;
    // Note: BlockStmt doubles as an Expr (last-statement-is-value). Its
    // resulting type belongs in a side table, not here.
};

struct DeclareStmt : NodeBase {
    Position pos;
    Position end;
    bool isMutable;
    SymID name;
    Expr value;
};

struct AssignStmt : NodeBase {
    Position pos;
    Position end;
    Expr lValue;
    TokenType op;
    Expr rValue;
};

struct ExprStmt : NodeBase {
    Position pos;
    Position end;
    Expr value;
};

struct ReturnStmt : NodeBase {
    Position pos;
    Position end;
    Expr value;
};

struct YieldStmt : NodeBase {
    Position pos;
    Position end;
    Expr value;
};

struct ForStmt : NodeBase {
    Position pos;
    Position end;
    Stmt init;
    Expr condition;
    Stmt post;
    BlockStmt* body;
};

struct BreakStmt : NodeBase {
    Position pos;
    Position end;
    Token token;
};

struct ContinueStmt : NodeBase {
    Position pos;
    Position end;
    Token token;
};

struct AliasDecl : NodeBase {
    Position pos;
    Position end;
    Capability cap = Capability::Ro;
    SymID name;
    Expr value;
};

struct VecPushStmt : NodeBase {
    Position pos;
    Position end;
    Expr lValue;
    Expr rValue;
};

} // namespace maml::ast
