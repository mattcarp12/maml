// ast_nodes.h
#pragma once
#include "ast.h"
#include "sym.h"
#include "token.h"

#include <cassert>
#include <cstdint>
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
    return Capability::Ro; // suppress warnings
}

// =============================================================================
// Helper Structs (Stored by value inside vectors on the Arena)
// =============================================================================

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
    Expr body; // Typically a BlockStmt* or YieldStmt*
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

// =============================================================================
// Expressions (Decorated with exprType for Semantic Analysis)
// =============================================================================

struct Identifier {
    Position pos;
    Position end;
    SymID name;
    const maml::types::Type* exprType = nullptr;
};

struct IntLiteral {
    Position pos;
    Position end;
    int64_t value;
    const maml::types::Type* exprType = nullptr;
};

struct BoolLiteral {
    Position pos;
    Position end;
    bool value;
    const maml::types::Type* exprType = nullptr;
};

struct StringLiteral {
    Position pos;
    Position end;
    std::string_view value;
    const maml::types::Type* exprType = nullptr;
};

struct InfixExpr {
    Position pos;
    Position end;
    Expr left;
    TokenType op;
    Expr right;
    const maml::types::Type* exprType = nullptr;
};

struct PrefixExpr {
    Position pos;
    Position end;
    TokenType op;
    Expr right;
    const maml::types::Type* exprType = nullptr;
};

struct CallExpr {
    Position pos;
    Position end;
    Expr function;
    std::vector<CallArg> arguments;
    const maml::types::Type* exprType = nullptr;
};

struct IfExpr {
    Position pos;
    Position end;
    Expr condition;
    BlockStmt* consequence;
    BlockStmt* alternative; // std::monostate (nullptr equiv) if no else
    const maml::types::Type* exprType = nullptr;
};

struct MatchExpr {
    Position pos;
    Position end;
    Expr subject;
    std::vector<MatchArm> arms;
    const maml::types::Type* exprType = nullptr;
};

struct AwaitExpr {
    Position pos;
    Position end;
    Expr value;
    const maml::types::Type* exprType = nullptr;
};

struct SpawnExpr {
    Position pos;
    Position end;
    CallExpr* value;
    const maml::types::Type* exprType = nullptr;
};

struct CompositeLiteral {
    Position pos;
    Position end;
    TypeExpr typeExpr;
    std::vector<CompositeElement> elements;
    const maml::types::Type* exprType = nullptr;
};

struct FieldAccess {
    Position pos;
    Position end;
    Expr object;
    Identifier* field; // Keeping as Identifier* for precise field position tracking
    const maml::types::Type* exprType = nullptr;
};

struct IndexExpr {
    Position pos;
    Position end;
    Expr left;
    Expr index;
    const maml::types::Type* exprType = nullptr;
};

struct SliceExpr {
    Position pos;
    Position end;
    Expr left;
    Expr low;
    Expr high;
    const maml::types::Type* exprType = nullptr;
};

struct TypeExprWrapper {
    Position pos;
    Position end;
    TypeExpr typeExpr;
    const maml::types::Type* exprType = nullptr;
};

// =============================================================================
// Statements
// =============================================================================

struct BlockStmt {
    Position pos;
    Position end;
    std::vector<Stmt> statements;
    const maml::types::Type* exprType = nullptr; // Since blocks can yield expressions
};

struct DeclareStmt {
    Position pos;
    Position end;
    bool isMutable;
    SymID name;
    Expr value;
};

struct AssignStmt {
    Position pos;
    Position end;
    Expr lValue;
    TokenType op; // ASSIGN, PLUS_EQ, MINUS_EQ, etc.
    Expr rValue;
};

struct ExprStmt {
    Position pos;
    Position end;
    Expr value;
};

struct ReturnStmt {
    Position pos;
    Position end;
    Expr value; // std::monostate if void return
};

struct YieldStmt {
    Position pos;
    Position end;
    Expr value;
};

struct ForStmt {
    Position pos;
    Position end;
    Stmt init;
    Expr condition;
    Stmt post;
    BlockStmt* body;
};

struct BreakStmt {
    Position pos;
    Position end;
    Token token;
};

struct ContinueStmt {
    Position pos;
    Position end;
    Token token;
};

struct AliasDecl {
    Position pos;
    Position end;
    Capability cap = Capability::Ro;
    SymID name;
    Expr value;
};

struct VecPushStmt {
    Position pos;
    Position end;
    Expr lValue;
    Expr rValue;
};

// =============================================================================
// Type Expressions
// =============================================================================

struct NamedTypeExpr {
    Position pos;
    Position end;
    Identifier* name;
    const maml::types::Type* exprType = nullptr;
};

struct ArrayTypeExpr {
    Position pos;
    Position end;
    TypeExpr base;
    int size;
    const maml::types::Type* exprType = nullptr;
};

struct StructTypeExpr {
    Position pos;
    Position end;
    SymID name;
    std::vector<StructTypeField> fields;
};

struct SumTypeExpr {
    Position pos;
    Position end;
    SymID name;
    std::vector<TypeExpr> typeArgs;
    std::vector<VariantTypeExpr> variants;
};

struct GenericTypeExpr {
    Position pos;
    Position end;
    Identifier* name;
    std::vector<TypeExpr> args;
    const maml::types::Type* exprType = nullptr;
};

// =============================================================================
// Patterns (For Match Arms)
// =============================================================================

struct IdentifierPattern {
    Position pos;
    Position end;
    SymID name;
};

struct LiteralPattern {
    Position pos;
    Position end;
    Expr value;
};

struct CompositePattern {
    Position pos;
    Position end;
    TypeExpr typeExpr;
    std::vector<CompositePatternElement> elements;
};

struct WildcardPattern {
    Position pos;
    Position end;
};

// =============================================================================
// Top-Level Declarations
// =============================================================================

struct FnDecl {
    Position pos;
    Position end;
    SymID name;
    std::vector<Param> params;
    TypeExpr returnType; // std::monostate if omitted/void
    BlockStmt* body; // nullptr/std::monostate if isExtern == true
    bool isAsync;
    bool isExtern;
};

struct TypeDecl {
    Position pos;
    Position end;
    Identifier* name;
    TypeExpr rhs;
};

struct Program {
    Position pos;
    Position end;
    std::vector<Decl> decls;
};

} // namespace maml::ast