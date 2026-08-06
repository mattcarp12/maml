// ast_decl.h
#pragma once

#include "ast_common.h"
#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <vector>

namespace maml::ast {

struct FnDecl : NodeBase {
    Position pos;
    Position end;
    SymID name;
    std::vector<Param> params;
    TypeExpr returnType;
    BlockStmt* body;
    bool isAsync;
    bool isExtern;
};

struct TypeDecl : NodeBase {
    Position pos;
    Position end;
    Identifier* name;
    TypeExpr rhs;
};

struct Program : NodeBase {
    Position pos;
    Position end;
    std::vector<Decl> decls;
};

} // namespace maml::ast
