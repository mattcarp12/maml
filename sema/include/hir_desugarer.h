#pragma once

#include "arena.h"
#include "ast.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"

namespace maml::hir {

class Desugarer {
public:
    Desugarer(types::TypeRegistry& registry, SymbolTable& sym, Arena& arena);

    void desugar(ast::Program* program);

private:
    types::TypeRegistry& registry_;
    SymbolTable& sym_;
    Arena& arena_;

    // Core Mutating Entry Points
    void desugarDecl(ast::Decl& decl);
    void desugarStmt(ast::Stmt& stmt);
    void desugarExpr(ast::Expr& expr);

    // Shared / Compound Node Traversal Helpers
    void desugarBlockStmt(ast::BlockStmt* block);
    void desugarFnDecl(ast::FnDecl* fn);

    // Expression Traversal Helpers
    void desugarInfixExpr(ast::Expr& parentRef, ast::InfixExpr* infix);
    void desugarPrefixExpr(ast::PrefixExpr* prefix);
    void desugarCallExpr(ast::Expr& parentRef, ast::CallExpr* call);
    void desugarIfExpr(ast::IfExpr* ifExpr);
    void desugarMatchExpr(ast::Expr& parentRef, ast::MatchExpr* match);
    void desugarAwaitExpr(ast::AwaitExpr* awaitExpr);
    void desugarSpawnExpr(ast::SpawnExpr* spawnExpr);
    void desugarCompositeLiteral(ast::CompositeLiteral* comp);
    void desugarFieldAccess(ast::FieldAccess* field);
    void desugarIndexExpr(ast::IndexExpr* idx);
    void desugarSliceExpr(ast::SliceExpr* slice);
    void desugarCastExpr(ast::CastExpr* cast);

    // Statement Traversal Helpers
    void desugarDeclareStmt(ast::DeclareStmt* decl);
    void desugarAssignStmt(ast::AssignStmt* assign);
    void desugarExprStmt(ast::ExprStmt* exprStmt);
    void desugarReturnStmt(ast::ReturnStmt* ret);
    void desugarYieldStmt(ast::YieldStmt* yld);
    void desugarForStmt(ast::ForStmt* forStmt);
    void desugarVecPushStmt(ast::VecPushStmt* push);

    ast::BlockStmt* makeExprBlock(ast::Expr expr, const types::Type* blockType, Position pos);
    ast::Expr makeBoolLiteral(bool val, Position pos);
    const types::Type* getTaggedUnionLayout(const types::Type* sumType);
    bool tryDesugarSumConstructor(ast::Expr& parentRef);

    ast::Expr makeDiscriminantCheck(ast::Expr subjectRef, int discriminant, Position pos);
    void extractPatternBindings(ast::BlockStmt* block, ast::Pattern pattern, ast::Expr subjectRef,
        const types::Type* sumType, int variantIndex, Position pos);
    const types::Type* getExprType(const ast::Expr& expr);
    int matchSubjCounter_ = 0;
};

} // namespace maml::hir