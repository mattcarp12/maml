// ast.h
#pragma once

#include "capability.h"
#include "sym.h"
#include "token.h"

#include <cassert>
#include <cstdint>
#include <format>
#include <ostream>
#include <string>
#include <string_view>
#include <variant>
#include <vector>

namespace maml::ast {

// --- Node Identification & Source Positions ---

using NodeID = uint32_t;
constexpr NodeID NoNode = 0;

struct NodeBase {
    NodeID id = NoNode;
    Position pos {};
    Position end {};
};

// --- Common Utilities & Error Handling ---

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

// --- Forward Declarations ---

// Expressions
struct Identifier;
struct IntLiteral;
struct BoolLiteral;
struct StringLiteral;
struct InfixExpr;
struct PrefixExpr;
struct CallExpr;
struct IfExpr;
struct MatchExpr;
struct AwaitExpr;
struct SpawnExpr;
struct CompositeLiteral;
struct FieldAccess;
struct IndexExpr;
struct SliceExpr;
struct TypeExprWrapper;
struct BlockStmt; // Serves as both Stmt and Expr

// Statements
struct DeclareStmt;
struct AssignStmt;
struct ExprStmt;
struct ReturnStmt;
struct YieldStmt;
struct ForStmt;
struct BreakStmt;
struct ContinueStmt;
struct AliasDecl;
struct VecPushStmt;

// Declarations
struct FnDecl;
struct TypeDecl;
struct Program;

// Type Expressions
struct NamedTypeExpr;
struct ArrayTypeExpr;
struct StructTypeExpr;
struct SumTypeExpr;
struct GenericTypeExpr;

// Patterns
struct IdentifierPattern;
struct LiteralPattern;
struct CompositePattern;
struct WildcardPattern;

// --- AST Variant Handles ---

using Expr = std::variant<std::monostate, Identifier*, IntLiteral*, BoolLiteral*, StringLiteral*,
    InfixExpr*, PrefixExpr*, CallExpr*, IfExpr*, MatchExpr*, AwaitExpr*, SpawnExpr*,
    CompositeLiteral*, FieldAccess*, IndexExpr*, SliceExpr*, TypeExprWrapper*, BlockStmt*>;

using Stmt = std::variant<std::monostate, BlockStmt*, DeclareStmt*, AssignStmt*, ExprStmt*,
    ReturnStmt*, YieldStmt*, ForStmt*, BreakStmt*, ContinueStmt*, AliasDecl*, VecPushStmt*>;

using Decl = std::variant<std::monostate, FnDecl*, TypeDecl*, Program*>;

using TypeExpr = std::variant<std::monostate, NamedTypeExpr*, ArrayTypeExpr*, StructTypeExpr*,
    SumTypeExpr*, GenericTypeExpr*>;

using Pattern = std::variant<std::monostate, IdentifierPattern*, LiteralPattern*, CompositePattern*,
    WildcardPattern*>;

// --- Helper Syntax Structures ---

struct CallArg {
    Position pos;
    Position end;
    Capability cap = Capability::Ro;
    Expr argument;
};

struct CompositeElement {
    Position pos;
    Position end;
    Expr key; // std::monostate if positional
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

// --- Concrete Node Definitions ---

// Expressions
struct Identifier : NodeBase {
    SymID name;
};

struct IntLiteral : NodeBase {
    int64_t value;
};

struct BoolLiteral : NodeBase {
    bool value;
};

struct StringLiteral : NodeBase {
    std::string value;
};

struct InfixExpr : NodeBase {
    Expr left;
    TokenType op;
    Expr right;
};

struct PrefixExpr : NodeBase {
    TokenType op;
    Expr right;
};

struct CallExpr : NodeBase {
    Expr function;
    std::vector<CallArg> arguments;
};

struct IfExpr : NodeBase {
    Expr condition;
    BlockStmt* consequence;
    BlockStmt* alternative;
};

struct MatchExpr : NodeBase {
    Expr subject;
    std::vector<MatchArm> arms;
};

struct AwaitExpr : NodeBase {
    Expr value;
};

struct SpawnExpr : NodeBase {
    CallExpr* value;
};

struct CompositeLiteral : NodeBase {
    TypeExpr typeExpr;
    std::vector<CompositeElement> elements;
};

struct FieldAccess : NodeBase {
    Expr object;
    Identifier* field;
};

struct IndexExpr : NodeBase {
    Expr left;
    Expr index;
};

struct SliceExpr : NodeBase {
    Expr left;
    Expr low;
    Expr high;
};

struct TypeExprWrapper : NodeBase {
    TypeExpr typeExpr;
};

// Statements
struct BlockStmt : NodeBase {
    std::vector<Stmt> statements;
};

struct DeclareStmt : NodeBase {
    bool isMutable;
    SymID name;
    Expr value;
};

struct AssignStmt : NodeBase {
    Expr lValue;
    TokenType op;
    Expr rValue;
};

struct ExprStmt : NodeBase {
    Expr value;
};

struct ReturnStmt : NodeBase {
    Expr value;
};

struct YieldStmt : NodeBase {
    Expr value;
};

struct ForStmt : NodeBase {
    Stmt init;
    Expr condition;
    Stmt post;
    BlockStmt* body;
};

struct BreakStmt : NodeBase {
    Token token;
};

struct ContinueStmt : NodeBase {
    Token token;
};

struct AliasDecl : NodeBase {
    Capability cap = Capability::Ro;
    SymID name;
    Expr value;
};

struct VecPushStmt : NodeBase {
    Expr lValue;
    Expr rValue;
};

// Declarations
struct FnDecl : NodeBase {
    SymID name;
    std::vector<Param> params;
    TypeExpr returnType;
    BlockStmt* body;
    bool isAsync;
    bool isExtern;
};

struct TypeDecl : NodeBase {
    Identifier* name;
    TypeExpr rhs;
};

struct Program : NodeBase {
    std::vector<Decl> decls;
};

// Patterns
struct IdentifierPattern : NodeBase {
    SymID name;
};

struct LiteralPattern : NodeBase {
    Expr value;
};

struct CompositePattern : NodeBase {
    TypeExpr typeExpr;
    std::vector<CompositePatternElement> elements;
};

struct WildcardPattern : NodeBase { };

// Type Expressions
struct NamedTypeExpr : NodeBase {
    Identifier* name;
};

struct ArrayTypeExpr : NodeBase {
    TypeExpr base;
    int size;
};

struct StructTypeExpr : NodeBase {
    SymID name;
    std::vector<StructTypeField> fields;
};

struct SumTypeExpr : NodeBase {
    SymID name;
    std::vector<VariantTypeExpr> variants;
};

struct GenericTypeExpr : NodeBase {
    Identifier* name;
    std::vector<TypeExpr> args;
};

} // namespace maml::ast