#pragma once

#include "ast.h"
#include "ast_visitor.h"
#include "compiler_context.h"
#include "scope.h"
#include "semantic_tables.h"
#include "token.h"
#include "types.h"

namespace maml {
struct CompilerContext; // Forward declaration breaks header coupling
}

namespace maml::sema {

// Base class for all compiler semantic pipeline passes
class SemanticPass : public ast::ASTVisitor {
public:
    explicit SemanticPass(CompilerContext& ctx)
        : ctx_(ctx)
    {
    }

    virtual void run(ast::Program* program)
    {
        if (program) {
            visit(*program);
        }
    }

protected:
    CompilerContext& ctx_;
};

// Pass 1: Declaration Hoisting & Scope Setup
class DeclarationPass : public SemanticPass {
public:
    using SemanticPass::SemanticPass;
    void visit(ast::Program& node) override;
    void visit(ast::FnDecl& node) override;
    void visit(ast::TypeDecl& node) override;
};

// Pass 2: Type Expression Resolution & Body Layout
class TypeResolutionPass : public SemanticPass {
public:
    using SemanticPass::SemanticPass;
    void visit(ast::Program& node) override;
    void visit(ast::TypeDecl& node) override;
    void visit(ast::FnDecl& node) override;
};

// Pass 3: Type Checking & Side-Table Population
class TypeCheckPass : public SemanticPass {
public:
    explicit TypeCheckPass(CompilerContext& ctx)
        : SemanticPass(ctx)
        , currentScope_(ctx.globalScope) // Seed local chain with global scope
    {
    }

    // Local scope helpers — transient to THIS pass run
    void pushScope() { currentScope_ = ctx_.arena.make<Scope>(currentScope_); }

    void popScope()
    {
        if (currentScope_ && currentScope_->getParent()) {
            currentScope_ = currentScope_->getParent();
        }
    }
    using SemanticPass::SemanticPass;

    void visit(ast::Program& node) override;
    void visit(ast::FnDecl& node) override;

    // Statements
    void visit(ast::BlockStmt& node) override;
    void visit(ast::DeclareStmt& node) override;
    void visit(ast::AssignStmt& node) override;
    void visit(ast::AliasDecl& node) override;
    void visit(ast::ReturnStmt& node) override;
    void visit(ast::ExprStmt& node) override;
    void visit(ast::YieldStmt& node) override;
    void visit(ast::ForStmt& node) override;

    // Expressions
    void visit(ast::Identifier& node) override;
    void visit(ast::IntLiteral& node) override;
    void visit(ast::BoolLiteral& node) override;
    void visit(ast::StringLiteral& node) override;
    void visit(ast::InfixExpr& node) override;
    void visit(ast::PrefixExpr& node) override;
    void visit(ast::CallExpr& node) override;
    void visit(ast::IfExpr& node) override;
    void visit(ast::MatchExpr& node) override;
    void visit(ast::AwaitExpr& node) override;
    void visit(ast::SpawnExpr& node) override;
    void visit(ast::FieldAccess& node) override;
    void visit(ast::IndexExpr& node) override;
    void visit(ast::SliceExpr& node) override;
    void visit(ast::CompositeLiteral& node) override;

    // Patterns
    void visit(ast::IdentifierPattern& node) override;
    void visit(ast::LiteralPattern& node) override;
    void visit(ast::CompositePattern& node) override;
    void visit(ast::WildcardPattern& node) override;

private:
    void checkExpr(const ast::Expr& expr, const types::Type* expected = nullptr);
    void checkPattern(const ast::Pattern& pattern, const types::Type* subjectType = nullptr);
    [[nodiscard]] const types::Type* exprTypeOf(const ast::Expr& expr) const;
    [[nodiscard]] Position posOf(const ast::Expr& expr) const;
    [[nodiscard]] const ValueCategory* valueCategoryOf(const ast::Expr& expr) const;
    Symbol* getRootSymbol(const ast::Expr& expr);

    // Transient pass state (does not leak into CompilerContext)
    Scope* currentScope_ = nullptr;
    const types::Type* expectedReturn_ = nullptr;
    const types::Type* expectedType_ = nullptr;
    bool isAsync_ = false;
};

// Pass 4: Control Flow, Async, & Pattern Safety Checks
class ControlFlowPass : public SemanticPass {
public:
    using SemanticPass::SemanticPass;

    void visit(ast::Program& node) override;
    void visit(ast::FnDecl& node) override;
    void visit(ast::BlockStmt& node) override;
    void visit(ast::IfExpr& node) override;
    void visit(ast::ForStmt& node) override;
    void visit(ast::ReturnStmt& node) override;
    void visit(ast::AwaitExpr& node) override;
    void visit(ast::MatchExpr& node) override;
    void visit(ast::ExprStmt& node) override;

private:
    bool isAsyncContext_ = false;
    const ast::FnDecl* currentFn_ = nullptr;

    void checkExhaustiveness(ast::MatchExpr& match, const types::Type* subjectType);
};

// Pass 5: AST Control Flow Desugaring Pass
class DesugarPass : public SemanticPass {
public:
    using SemanticPass::SemanticPass;

    void run(ast::Program* program) override;

    // AST Traversals
    void visit(ast::Program& node) override;
    void visit(ast::FnDecl& node) override;
    void visit(ast::BlockStmt& node) override;

    // In-place expression desugaring entry point
    ast::Expr desugarExpr(ast::Expr expr);

private:
    int matchSubjCounter_ = 0;

    ast::Expr desugarInfixExpr(ast::InfixExpr* infix);
    ast::Expr desugarMatchExpr(ast::MatchExpr* match);

    ast::BlockStmt* makeExprBlock(ast::Expr expr, const types::Type* blockType, Position pos);
    ast::Expr makeBoolLiteral(bool val, Position pos);
    ast::Expr makeDiscriminantCheck(ast::Expr subjectRef, int discriminant, Position pos);
    void extractPatternBindings(ast::BlockStmt* block, const ast::Pattern& pattern,
        ast::Expr subjectRef, const types::Type* sumType, int variantIndex, Position pos);
};

} // namespace maml::sema