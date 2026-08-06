#pragma once

// TODO(you): this file is a placeholder. Share ast.h (or just the list of
// concrete node types) and the full set of `visit(NodeType&)` overloads
// below can be generated to match your actual AST. Every concrete pass
// (TypeCheckPass, ResolvePass, LoweringPass, ...) should derive from
// ASTVisitor and implement only the nodes it cares about.
//
// This also assumes each AST node grows a virtual `accept(ASTVisitor&)`
// method for double dispatch, e.g.:
//
//   struct BinaryExpr : Expr {
//       void accept(ASTVisitor& v) override { v.visit(*this); }
//   };
//
// which lets a pass be written as:
//
//   class TypeCheckPass : public ASTVisitor {
//   public:
//       explicit TypeCheckPass(CompilerContext& ctx) : ctx_(ctx) {}
//       void visit(ast::BinaryExpr&) override { ... }
//       void visit(ast::CallExpr&) override { ... }
//       void visit(ast::IfStmt&) override { ... }
//       // any node kind this pass doesn't care about just falls back to
//       // the default no-op (or recurse-into-children) impl below.
//   private:
//       CompilerContext& ctx_;
//   };
//
// #include "ast.h"

namespace maml {

class ASTVisitor {
public:
    virtual ~ASTVisitor() = default;

    // --- Expressions (from the design notes' example) ---
    // virtual void visit(ast::BinaryExpr& node) = 0;
    // virtual void visit(ast::CallExpr& node) = 0;
    // TODO: UnaryExpr, LiteralExpr, NameExpr, IndexExpr, FieldExpr, ...

    // --- Statements ---
    // virtual void visit(ast::IfStmt& node) = 0;
    // TODO: WhileStmt, ForStmt, ReturnStmt, LetStmt, Block, ...

    // --- Declarations / top-level ---
    // TODO: FunctionDecl, StructDecl, SumDecl, ...
};

} // namespace maml
