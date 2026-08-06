// ast_visitor.h
#pragma once

#include "ast.h"

#include <type_traits>
#include <variant>

namespace maml::ast {

class ASTVisitor {
public:
    virtual ~ASTVisitor() = default;

    // --- Expressions ---
    virtual void visit(Identifier&) { }
    virtual void visit(IntLiteral&) { }
    virtual void visit(BoolLiteral&) { }
    virtual void visit(StringLiteral&) { }
    virtual void visit(InfixExpr&) { }
    virtual void visit(PrefixExpr&) { }
    virtual void visit(CallExpr&) { }
    virtual void visit(IfExpr&) { }
    virtual void visit(MatchExpr&) { }
    virtual void visit(AwaitExpr&) { }
    virtual void visit(SpawnExpr&) { }
    virtual void visit(CompositeLiteral&) { }
    virtual void visit(FieldAccess&) { }
    virtual void visit(IndexExpr&) { }
    virtual void visit(SliceExpr&) { }
    virtual void visit(TypeExprWrapper&) { }

    // --- Statements ---
    virtual void visit(BlockStmt&) { }
    virtual void visit(DeclareStmt&) { }
    virtual void visit(AssignStmt&) { }
    virtual void visit(ExprStmt&) { }
    virtual void visit(ReturnStmt&) { }
    virtual void visit(YieldStmt&) { }
    virtual void visit(ForStmt&) { }
    virtual void visit(BreakStmt&) { }
    virtual void visit(ContinueStmt&) { }
    virtual void visit(AliasDecl&) { }
    virtual void visit(VecPushStmt&) { }

    // --- Declarations ---
    virtual void visit(FnDecl&) { }
    virtual void visit(TypeDecl&) { }
    virtual void visit(Program&) { }

    // --- Type Expressions ---
    virtual void visit(NamedTypeExpr&) { }
    virtual void visit(ArrayTypeExpr&) { }
    virtual void visit(StructTypeExpr&) { }
    virtual void visit(SumTypeExpr&) { }
    virtual void visit(GenericTypeExpr&) { }

    // --- Patterns ---
    virtual void visit(IdentifierPattern&) { }
    virtual void visit(LiteralPattern&) { }
    virtual void visit(CompositePattern&) { }
    virtual void visit(WildcardPattern&) { }

    // --- Generic Variant Dispatcher ---
    // Unpacks `std::variant` AST handles (Expr, Stmt, Decl, TypeExpr, Pattern)
    // and dispatches to the corresponding `visit(*node)` overload.
    template <typename VariantHandle> void dispatch(const VariantHandle& handle)
    {
        std::visit(
            [this](auto&& ptr) {
                using T = std::decay_t<decltype(ptr)>;
                if constexpr (!std::is_same_v<T, std::monostate>) {
                    if (ptr) {
                        this->visit(*ptr);
                    }
                }
            },
            handle);
    }
};

} // namespace maml::ast