#pragma once

#include "ast.h"
#include "ast_visitor.h"
#include "sym.h"

#include <iostream>

namespace maml::ast {

class AstPrinter : public ASTVisitor {
public:
    explicit AstPrinter(const SymbolTable& sym, std::ostream& out = std::cout);

    void print(Program* prog);

    template <typename VariantHandle> void printNode(const VariantHandle& handle)
    {
        dispatch(handle);
    }

    // Declarations
    void visit(Program& p) override;
    void visit(FnDecl& fn) override;
    void visit(TypeDecl& td) override;

    // Statements
    void visit(BlockStmt& b) override;
    void visit(DeclareStmt& d) override;
    void visit(AssignStmt& a) override;
    void visit(ExprStmt& e) override;
    void visit(ReturnStmt& r) override;
    void visit(YieldStmt& y) override;
    void visit(ForStmt& f) override;
    void visit(BreakStmt& b) override;
    void visit(ContinueStmt& c) override;
    void visit(AliasDecl& a) override;
    void visit(VecPushStmt& v) override;

    // Expressions
    void visit(Identifier& id) override;
    void visit(IntLiteral& i) override;
    void visit(BoolLiteral& b) override;
    void visit(StringLiteral& s) override;
    void visit(InfixExpr& inf) override;
    void visit(PrefixExpr& pre) override;
    void visit(CallExpr& c) override;
    void visit(IfExpr& ife) override;
    void visit(MatchExpr& m) override;
    void visit(AwaitExpr& a) override;
    void visit(SpawnExpr& s) override;
    void visit(CompositeLiteral& cl) override;
    void visit(FieldAccess& fa) override;
    void visit(IndexExpr& idx) override;
    void visit(SliceExpr& sl) override;
    void visit(TypeExprWrapper& tw) override;

    // Type Expressions
    void visit(NamedTypeExpr& n) override;
    void visit(ArrayTypeExpr& a) override;
    void visit(StructTypeExpr& s) override;
    void visit(SumTypeExpr& st) override;
    void visit(GenericTypeExpr& g) override;

    // Patterns
    void visit(WildcardPattern& w) override;
    void visit(IdentifierPattern& ip) override;
    void visit(LiteralPattern& lp) override;
    void visit(CompositePattern& cp) override;

private:
    const SymbolTable& sym_;
    std::ostream& out_;
    int indent_ = 0;

    void indent();
    void printLine(const std::string& text, NodeID id = NoNode);
};

} // namespace maml::ast