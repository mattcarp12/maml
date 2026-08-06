// ast_pattern.h
#pragma once

#include "ast_common.h"
#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <vector>

namespace maml::ast {

struct IdentifierPattern : NodeBase {
    Position pos;
    Position end;
    SymID name;
};

struct LiteralPattern : NodeBase {
    Position pos;
    Position end;
    Expr value;
};

struct CompositePattern : NodeBase {
    Position pos;
    Position end;
    TypeExpr typeExpr;
    std::vector<CompositePatternElement> elements;
};

struct WildcardPattern : NodeBase {
    Position pos;
    Position end;
};

} // namespace maml::ast
