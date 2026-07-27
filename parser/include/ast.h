// ast.h
#pragma once
#include "token.h"
#include <format>
#include <string>
#include <variant>

// Forward declare your semantic type system's base class
namespace maml::types {
struct Type;
}

namespace maml::ast {

// =============================================================================
// 1. Forward Declarations
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

struct CompileError {
    std::string stage;
    Position pos;
    std::string msg;

    std::string toString() const
    {
        return std::format("[{} Error] at {}:{}: {}", stage, pos.line, pos.col, msg);
    }
};

inline std::ostream& operator<<(std::ostream& os, const CompileError& err)
{
    os << err.toString();
    return os;
}

// =============================================================================
// 2. Variant Definitions (AST Node Interfaces)
// =============================================================================

using Expr = std::variant<std::monostate, // Safe 'nil' state
    Identifier*, IntLiteral*, BoolLiteral*, StringLiteral*, InfixExpr*, PrefixExpr*, CallExpr*,
    IfExpr*, MatchExpr*, AwaitExpr*, SpawnExpr*, CompositeLiteral*, FieldAccess*, IndexExpr*,
    SliceExpr*, TypeExprWrapper*,
    BlockStmt* // MAML blocks can yield values
    >;

using Stmt = std::variant<std::monostate, // Safe 'nil' state
    BlockStmt*, DeclareStmt*, AssignStmt*, ExprStmt*, ReturnStmt*, YieldStmt*, ForStmt*, BreakStmt*,
    ContinueStmt*, AliasDecl*, VecPushStmt*>;

using Decl = std::variant<std::monostate, FnDecl*, TypeDecl*, Program*>;

using TypeExpr = std::variant<std::monostate, NamedTypeExpr*, ArrayTypeExpr*, StructTypeExpr*,
    SumTypeExpr*, GenericTypeExpr*>;

using Pattern = std::variant<std::monostate, IdentifierPattern*, LiteralPattern*, CompositePattern*,
    WildcardPattern*>;

} // namespace maml::ast