#include "ast_printer.h"
#include "token.h"

#include <format>

namespace maml::ast {

AstPrinter::AstPrinter(const SymbolTable& sym, std::ostream& out)
    : sym_(sym)
    , out_(out)
{
}

void AstPrinter::print(Program* prog)
{
    if (prog)
        visit(*prog);
}

// --- Declarations ---

void AstPrinter::visit(Program& p)
{
    printLine("Program", p.id);
    indent_++;
    for (const auto& decl : p.decls)
        dispatch(decl);
    indent_--;
}

void AstPrinter::visit(FnDecl& fn)
{
    printLine(std::format("FnDecl: {} (async={}, extern={})", sym_.resolve(fn.name), fn.isAsync,
                  fn.isExtern),
        fn.id);
    indent_++;

    printLine("Params:");
    indent_++;
    for (const auto& p : fn.params) {
        indent();
        out_ << std::format("{} : ", sym_.resolve(p.name));
        dispatch(p.type);
        out_ << "\n";
    }
    indent_--;

    indent();
    out_ << "ReturnType: ";
    dispatch(fn.returnType);
    out_ << "\n";

    if (fn.body) {
        printLine("Body:");
        indent_++;
        visit(*fn.body);
        indent_--;
    }
    indent_--;
}

void AstPrinter::visit(TypeDecl& td)
{
    printLine(
        std::format("TypeDecl: {}", td.name ? sym_.resolve(td.name->name) : "<anon>"), td.id);
    indent_++;
    dispatch(td.rhs);
    out_ << "\n";
    indent_--;
}

// --- Statements ---

void AstPrinter::visit(BlockStmt& b)
{
    printLine("BlockStmt", b.id);
    indent_++;
    for (const auto& s : b.statements)
        dispatch(s);
    indent_--;
}

void AstPrinter::visit(DeclareStmt& d)
{
    printLine(std::format("DeclareStmt: {} (mut={})", sym_.resolve(d.name), d.isMutable), d.id);
    indent_++;
    dispatch(d.value);
    indent_--;
}

void AstPrinter::visit(AssignStmt& a)
{
    printLine(std::format("AssignStmt (op={})", TokenTypeToString(a.op)), a.id);
    indent_++;
    dispatch(a.lValue);
    dispatch(a.rValue);
    indent_--;
}

void AstPrinter::visit(ExprStmt& e)
{
    printLine("ExprStmt", e.id);
    indent_++;
    dispatch(e.value);
    indent_--;
}

void AstPrinter::visit(ReturnStmt& r)
{
    printLine("ReturnStmt", r.id);
    indent_++;
    dispatch(r.value);
    indent_--;
}

void AstPrinter::visit(YieldStmt& y)
{
    printLine("YieldStmt", y.id);
    indent_++;
    dispatch(y.value);
    indent_--;
}

void AstPrinter::visit(ForStmt& f)
{
    printLine("ForStmt", f.id);
    indent_++;
    printLine("Init:");
    dispatch(f.init);
    printLine("Cond:");
    dispatch(f.condition);
    printLine("Post:");
    dispatch(f.post);
    printLine("Body:");
    if (f.body)
        visit(*f.body);
    indent_--;
}

void AstPrinter::visit(BreakStmt& b) { printLine("BreakStmt", b.id); }
void AstPrinter::visit(ContinueStmt& c) { printLine("ContinueStmt", c.id); }

void AstPrinter::visit(AliasDecl& a)
{
    printLine(std::format("AliasDecl: {}", sym_.resolve(a.name)), a.id);
    indent_++;
    dispatch(a.value);
    indent_--;
}

void AstPrinter::visit(VecPushStmt& v)
{
    printLine("VecPushStmt", v.id);
    indent_++;
    dispatch(v.lValue);
    dispatch(v.rValue);
    indent_--;
}

// --- Expressions ---

void AstPrinter::visit(Identifier& id)
{
    printLine(std::format("Identifier: {}", sym_.resolve(id.name)), id.id);
}

void AstPrinter::visit(IntLiteral& i) { printLine(std::format("IntLiteral: {}", i.value), i.id); }

void AstPrinter::visit(BoolLiteral& b)
{
    printLine(std::format("BoolLiteral: {}", b.value ? "true" : "false"), b.id);
}

void AstPrinter::visit(StringLiteral& s)
{
    printLine(std::format("StringLiteral: \"{}\"", s.value), s.id);
}

void AstPrinter::visit(InfixExpr& inf)
{
    printLine(std::format("InfixExpr: {}", TokenTypeToString(inf.op)), inf.id);
    indent_++;
    dispatch(inf.left);
    dispatch(inf.right);
    indent_--;
}

void AstPrinter::visit(PrefixExpr& pre)
{
    printLine(std::format("PrefixExpr: {}", TokenTypeToString(pre.op)), pre.id);
    indent_++;
    dispatch(pre.right);
    indent_--;
}

void AstPrinter::visit(CallExpr& c)
{
    printLine("CallExpr", c.id);
    indent_++;
    printLine("Callee:");
    dispatch(c.function);
    printLine("Args:");
    indent_++;
    for (const auto& arg : c.arguments) {
        dispatch(arg.argument);
    }
    indent_--;
    indent_--;
}

void AstPrinter::visit(IfExpr& ife)
{
    printLine("IfExpr", ife.id);
    indent_++;
    printLine("Cond:");
    dispatch(ife.condition);
    printLine("Then:");
    if (ife.consequence)
        visit(*ife.consequence);
    if (ife.alternative) {
        printLine("Else:");
        visit(*ife.alternative);
    }
    indent_--;
}

void AstPrinter::visit(MatchExpr& m)
{
    printLine("MatchExpr", m.id);
    indent_++;
    printLine("Subject:");
    dispatch(m.subject);
    printLine("Arms:");
    indent_++;
    for (const auto& arm : m.arms) {
        printLine("Arm Pattern:");
        indent_++;
        dispatch(arm.pattern);
        indent_--;
        printLine("Arm Body:");
        indent_++;
        dispatch(arm.body);
        indent_--;
    }
    indent_--;
    indent_--;
}

void AstPrinter::visit(AwaitExpr& a)
{
    printLine("AwaitExpr", a.id);
    indent_++;
    dispatch(a.value);
    indent_--;
}

void AstPrinter::visit(SpawnExpr& s)
{
    printLine("SpawnExpr", s.id);
    indent_++;
    if (s.value)
        visit(*s.value);
    indent_--;
}

void AstPrinter::visit(CompositeLiteral& cl)
{
    indent();
    out_ << "CompositeLiteral Type: ";
    dispatch(cl.typeExpr);
    out_ << "\n";
    indent_++;
    for (const auto& el : cl.elements) {
        dispatch(el.value);
    }
    indent_--;
}

void AstPrinter::visit(FieldAccess& fa)
{
    printLine(
        std::format("FieldAccess: .{}", fa.field ? sym_.resolve(fa.field->name) : ""), fa.id);
    indent_++;
    dispatch(fa.object);
    indent_--;
}

void AstPrinter::visit(IndexExpr& idx)
{
    printLine("IndexExpr", idx.id);
    indent_++;
    dispatch(idx.left);
    dispatch(idx.index);
    indent_--;
}

void AstPrinter::visit(SliceExpr& sl)
{
    printLine("SliceExpr", sl.id);
    indent_++;
    dispatch(sl.left);
    dispatch(sl.low);
    dispatch(sl.high);
    indent_--;
}

void AstPrinter::visit(TypeExprWrapper& tw)
{
    indent();
    out_ << "TypeExprWrapper: ";
    dispatch(tw.typeExpr);
    out_ << "\n";
}

// --- Type Expressions ---

void AstPrinter::visit(NamedTypeExpr& n)
{
    if (n.name)
        out_ << sym_.resolve(n.name->name);
}

void AstPrinter::visit(ArrayTypeExpr& a)
{
    out_ << "[" << a.size << "]";
    dispatch(a.base);
}

void AstPrinter::visit(StructTypeExpr& s)
{
    out_ << "Struct { ";
    for (const auto& f : s.fields) {
        out_ << sym_.resolve(f.name) << " ";
    }
    out_ << "}";
}

void AstPrinter::visit(SumTypeExpr& st)
{
    out_ << "SumType [";
    for (const auto& v : st.variants) {
        out_ << sym_.resolve(v.name) << " ";
    }
    out_ << "]";
}

void AstPrinter::visit(GenericTypeExpr& g)
{
    out_ << (g.name ? sym_.resolve(g.name->name) : "") << "<";
    for (size_t i = 0; i < g.args.size(); ++i) {
        if (i > 0)
            out_ << ", ";
        dispatch(g.args[i]);
    }
    out_ << ">";
}

// --- Patterns ---

void AstPrinter::visit(WildcardPattern& w) { printLine("Wildcard (_)", w.id); }

void AstPrinter::visit(IdentifierPattern& ip)
{
    printLine(std::format("IdentifierPattern: {}", sym_.resolve(ip.name)), ip.id);
}

void AstPrinter::visit(LiteralPattern& lp)
{
    printLine("LiteralPattern:", lp.id);
    indent_++;
    dispatch(lp.value);
    indent_--;
}

void AstPrinter::visit(CompositePattern& cp)
{
    indent();
    out_ << "CompositePattern Type: ";
    dispatch(cp.typeExpr);
    out_ << "\n";
    indent_++;
    for (const auto& elem : cp.elements) {
        dispatch(elem.pattern);
    }
    indent_--;
}

// --- Helper Functions ---

void AstPrinter::indent()
{
    for (int i = 0; i < indent_; ++i) {
        out_ << "  ";
    }
}

void AstPrinter::printLine(const std::string& text, NodeID id)
{
    indent();
    if (id != NoNode) {
        out_ << std::format("[#{}] ", id);
    }
    out_ << text << "\n";
}

} // namespace maml::ast