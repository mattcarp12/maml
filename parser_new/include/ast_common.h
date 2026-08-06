// ast_common.h
#pragma once

#include "ast_fwd.h"
#include "node.h"
#include "sym.h"
#include "token.h"

#include <cassert>
#include <format>
#include <ostream>
#include <string>
#include <string_view>
#include <vector>

namespace maml::ast {

enum class Capability : uint8_t { Mut, Own, Ro };

inline Capability parseCapability(std::string_view literal)
{
    if (literal == "mut")
        return Capability::Mut;
    if (literal == "own")
        return Capability::Own;
    if (literal == "ro")
        return Capability::Ro;
    assert(false && "parseCapability called with non-capability literal");
    return Capability::Ro;
}

struct CompileError {
    std::string stage;
    Position pos;
    std::string msg;

    [[nodiscard]] std::string toString() const
    {
        return std::format(
            "[{} Error] in {} at {}:{}: {}", stage, pos.filename, pos.line, pos.col, msg);
    }
};

inline std::ostream& operator<<(std::ostream& os, const CompileError& err)
{
    os << err.toString();
    return os;
}

// --- Helper sub-structs shared across node kinds ---
// These describe *syntax shape*, not semantics, so they stay here rather
// than in a side table (there's no single NodeID they'd hang off of).

struct CallArg {
    Position pos;
    Position end;
    Capability cap = Capability::Ro;
    Expr argument;
};

struct CompositeElement {
    Position pos;
    Position end;
    Expr key; // std::monostate if purely positional
    Expr value;
};

struct CompositePatternElement {
    Position pos;
    Position end;
    Expr key;
    Pattern pattern;
};

struct MatchArm {
    Position pos;
    Position end;
    Pattern pattern;
    Expr body;
};

struct Param {
    Position pos;
    Position end;
    Capability cap = Capability::Ro;
    SymID name;
    TypeExpr type;
};

struct StructTypeField {
    Position pos;
    Position end;
    SymID name;
    TypeExpr type;
};

struct VariantTypeExpr {
    Position pos;
    Position end;
    SymID name;
    std::vector<StructTypeField> fields;
    std::vector<TypeExpr> tupleFields;
};

} // namespace maml::ast
