// ast_fwd.h
#pragma once
#include <variant>

namespace maml::ast {

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

// NOTE: TaggedUnionConstructExpr, TaggedUnionAccessExpr, IntrinsicCallExpr,
// and CastExpr moved out of the parser AST entirely — see ir.h. They are
// never produced by the parser; they're introduced during lowering, so they
// don't belong in a "faithful representation of source code."

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

} // namespace maml::ast
