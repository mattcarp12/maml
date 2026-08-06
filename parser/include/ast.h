// ast.h
#pragma once

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

namespace maml::types {
struct Type;
}

namespace maml::ast {

// =============================================================================
// 1. Enums & Utility Structs
// =============================================================================

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

// =============================================================================
// 2. Forward Declarations
// =============================================================================

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
struct BlockStmt; // Acts as both Stmt and Expr
struct TaggedUnionConstructExpr;
struct TaggedUnionAccessExpr;
struct IntrinsicCallExpr;
struct CastExpr;

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

// =============================================================================
// 3. Variant Definitions (AST Node Interfaces)
// =============================================================================

using Expr = std::variant<std::monostate, Identifier*, IntLiteral*, BoolLiteral*, StringLiteral*,
    InfixExpr*, PrefixExpr*, CallExpr*, IfExpr*, MatchExpr*, AwaitExpr*, SpawnExpr*,
    CompositeLiteral*, FieldAccess*, IndexExpr*, SliceExpr*, TypeExprWrapper*, BlockStmt*,
    TaggedUnionConstructExpr*, TaggedUnionAccessExpr*, IntrinsicCallExpr*, CastExpr*>;

using Stmt = std::variant<std::monostate, BlockStmt*, DeclareStmt*, AssignStmt*, ExprStmt*,
    ReturnStmt*, YieldStmt*, ForStmt*, BreakStmt*, ContinueStmt*, AliasDecl*, VecPushStmt*>;

using Decl = std::variant<std::monostate, FnDecl*, TypeDecl*, Program*>;

using TypeExpr = std::variant<std::monostate, NamedTypeExpr*, ArrayTypeExpr*, StructTypeExpr*,
    SumTypeExpr*, GenericTypeExpr*>;

using Pattern = std::variant<std::monostate, IdentifierPattern*, LiteralPattern*, CompositePattern*,
    WildcardPattern*>;

// =============================================================================
// 4. Helper Sub-structs
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

// =============================================================================
// 5. AST Node Definitions
// =============================================================================

// --- Expressions ---

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
    BlockStmt* alternative;
    const maml::types::Type* exprType = nullptr;

    bool alternativeIsUnreachable = false;
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
    Identifier* field;
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

struct TaggedUnionConstructExpr {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr; // The lowered Tagged Union layout struct
    int discriminant; // The tag value (0, 1, 2...)
    std::vector<Expr> payloadArgs; // Evaluated payload arguments
    const maml::types::Type* payloadStructType = nullptr; // Variant payload struct type
};

struct TaggedUnionAccessExpr {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr; // The extracted field's type
    Expr object; // The tagged union expression
    int fieldIndex; // Index inside the payload struct
    const maml::types::Type* payloadStructType = nullptr;
};

struct IntrinsicCallExpr {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr;
    SymID intrinsicSym; // Interned intrinsic name (e.g., "maml_vec_len")
    std::vector<Expr> arguments;
};

struct CastExpr {
    Position pos;
    Position end;
    const maml::types::Type* exprType = nullptr;
    Expr source;
    const maml::types::Type* targetType = nullptr;
};

// --- Statements ---

struct BlockStmt {
    Position pos;
    Position end;
    std::vector<Stmt> statements;
    const maml::types::Type* exprType = nullptr;
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
    TokenType op;
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
    Expr value;
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

// --- Type Expressions ---

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
    std::vector<VariantTypeExpr> variants;
};

struct GenericTypeExpr {
    Position pos;
    Position end;
    Identifier* name;
    std::vector<TypeExpr> args;
    const maml::types::Type* exprType = nullptr;
};

// --- Patterns ---

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

// --- Top-Level Declarations ---

struct FnDecl {
    Position pos;
    Position end;
    SymID name;
    std::vector<Param> params;
    TypeExpr returnType;
    BlockStmt* body;
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