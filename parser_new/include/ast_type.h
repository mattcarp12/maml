// ast_type.h
#pragma once

#include "ast_common.h"
#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <vector>

namespace maml::ast {

struct NamedTypeExpr : NodeBase {
    Position pos;
    Position end;
    Identifier* name;
};

struct ArrayTypeExpr : NodeBase {
    Position pos;
    Position end;
    TypeExpr base;
    int size;
};

struct StructTypeExpr : NodeBase {
    Position pos;
    Position end;
    SymID name;
    std::vector<StructTypeField> fields;
};

struct SumTypeExpr : NodeBase {
    Position pos;
    Position end;
    SymID name;
    std::vector<VariantTypeExpr> variants;
};

struct GenericTypeExpr : NodeBase {
    Position pos;
    Position end;
    Identifier* name;
    std::vector<TypeExpr> args;
};

} // namespace maml::ast
