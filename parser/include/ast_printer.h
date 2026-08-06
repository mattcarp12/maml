#pragma once

#include "ast.h"
#include "sym.h"
#include "token.h"

#include <format>
#include <iostream>
#include <string>
#include <variant>

namespace maml::ast {

// C++ std::visit visitor overload helper
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

class AstPrinter {
public:
    explicit AstPrinter(const SymbolTable& sym, std::ostream& out = std::cout)
        : sym_(sym)
        , out_(out)
    {
    }

    void print(const Program* prog)
    {
        if (!prog)
            return;
        printLine("Program");
        indent_++;
        for (const auto& decl : prog->decls) {
            printDecl(decl);
        }
        indent_--;
    }

    void printDecl(const Decl& decl)
    {
        std::visit(overloaded { [&](std::monostate) {},
                       [&](FnDecl* fn) {
                           if (!fn)
                               return;
                           std::string header = std::format("FnDecl: {} (async={}, extern={})",
                               sym_.resolve(fn->name), fn->isAsync, fn->isExtern);
                           printLine(header);
                           indent_++;

                           printLine("Params:");
                           indent_++;
                           for (const auto& p : fn->params) {
                               indent();
                               out_ << std::format("{} : ", sym_.resolve(p.name));
                               printTypeExpr(p.type);
                               out_ << "\n";
                           }
                           indent_--;

                           // Print ReturnType inline on the same line
                           indent();
                           out_ << "ReturnType: ";
                           printTypeExpr(fn->returnType);
                           out_ << "\n";

                           if (fn->body) {
                               printLine("Body:");
                               indent_++;
                               printStmt(fn->body);
                               indent_--;
                           }
                           indent_--;
                       },
                       [&](TypeDecl* td) {
                           if (!td)
                               return;
                           printLine(std::format(
                               "TypeDecl: {}", td->name ? sym_.resolve(td->name->name) : "<anon>"));
                           indent_++;
                           printTypeExpr(td->rhs);
                           out_ << "\n";
                           indent_--;
                       },
                       [&](Program* p) { print(p); } },
            decl);
    }

    void printStmt(const Stmt& stmt)
    {
        std::visit(overloaded { [&](std::monostate) {},
                       [&](BlockStmt* b) {
                           if (!b)
                               return;
                           printLine("BlockStmt");
                           indent_++;
                           for (const auto& s : b->statements) {
                               printStmt(s);
                           }
                           indent_--;
                       },
                       [&](DeclareStmt* d) {
                           if (!d)
                               return;
                           printLine(std::format(
                               "DeclareStmt: {} (mut={})", sym_.resolve(d->name), d->isMutable));
                           indent_++;
                           printExpr(d->value);
                           indent_--;
                       },
                       [&](AssignStmt* a) {
                           if (!a)
                               return;
                           printLine(std::format("AssignStmt (op={})", TokenTypeToString(a->op)));
                           indent_++;
                           printExpr(a->lValue);
                           printExpr(a->rValue);
                           indent_--;
                       },
                       [&](ExprStmt* e) {
                           if (!e)
                               return;
                           printLine("ExprStmt");
                           indent_++;
                           printExpr(e->value);
                           indent_--;
                       },
                       [&](ReturnStmt* r) {
                           if (!r)
                               return;
                           printLine("ReturnStmt");
                           indent_++;
                           printExpr(r->value);
                           indent_--;
                       },
                       [&](YieldStmt* y) {
                           if (!y)
                               return;
                           printLine("YieldStmt");
                           indent_++;
                           printExpr(y->value);
                           indent_--;
                       },
                       [&](ForStmt* f) {
                           if (!f)
                               return;
                           printLine("ForStmt");
                           indent_++;
                           printLine("Init:");
                           printStmt(f->init);
                           printLine("Cond:");
                           printExpr(f->condition);
                           printLine("Post:");
                           printStmt(f->post);
                           printLine("Body:");
                           printStmt(f->body);
                           indent_--;
                       },
                       [&](BreakStmt*) { printLine("BreakStmt"); },
                       [&](ContinueStmt*) { printLine("ContinueStmt"); },
                       [&](AliasDecl* a) {
                           if (!a)
                               return;
                           printLine(std::format("AliasDecl: {}", sym_.resolve(a->name)));
                           indent_++;
                           printExpr(a->value);
                           indent_--;
                       },
                       [&](VecPushStmt* v) {
                           if (!v)
                               return;
                           printLine("VecPushStmt");
                           indent_++;
                           printExpr(v->lValue);
                           printExpr(v->rValue);
                           indent_--;
                       } },
            stmt);
    }

    void printExpr(const Expr& expr)
    {
        std::visit(overloaded { [&](std::monostate) {},
                       [&](Identifier* id) {
                           if (id)
                               printLine(std::format("Identifier: {}", sym_.resolve(id->name)));
                       },
                       [&](IntLiteral* i) {
                           if (i)
                               printLine(std::format("IntLiteral: {}", i->value));
                       },
                       [&](BoolLiteral* b) {
                           if (b)
                               printLine(
                                   std::format("BoolLiteral: {}", b->value ? "true" : "false"));
                       },
                       [&](StringLiteral* s) {
                           if (s)
                               printLine(std::format("StringLiteral: \"{}\"", s->value));
                       },
                       [&](InfixExpr* inf) {
                           if (!inf)
                               return;
                           printLine(std::format("InfixExpr: {}", TokenTypeToString(inf->op)));
                           indent_++;
                           printExpr(inf->left);
                           printExpr(inf->right);
                           indent_--;
                       },
                       [&](PrefixExpr* pre) {
                           if (!pre)
                               return;
                           printLine(std::format("PrefixExpr: {}", TokenTypeToString(pre->op)));
                           indent_++;
                           printExpr(pre->right);
                           indent_--;
                       },
                       [&](CallExpr* c) {
                           if (!c)
                               return;
                           printLine("CallExpr");
                           indent_++;
                           printLine("Callee:");
                           printExpr(c->function);
                           printLine("Args:");
                           indent_++;
                           for (const auto& arg : c->arguments) {
                               printExpr(arg.argument);
                           }
                           indent_--;
                           indent_--;
                       },
                       [&](IfExpr* ife) {
                           if (!ife)
                               return;
                           printLine("IfExpr");
                           indent_++;
                           printLine("Cond:");
                           printExpr(ife->condition);
                           printLine("Then:");
                           printStmt(ife->consequence);
                           if (ife->alternative) {
                               printLine("Else:");
                               printStmt(ife->alternative);
                           }
                           indent_--;
                       },
                       [&](MatchExpr* m) {
                           if (!m)
                               return;
                           printLine("MatchExpr");
                           indent_++;
                           printLine("Subject:");
                           printExpr(m->subject);
                           printLine("Arms:");
                           indent_++;
                           for (const auto& arm : m->arms) {
                               printLine("Arm Pattern:");
                               indent_++;
                               printPattern(arm.pattern);
                               indent_--;
                               printLine("Arm Body:");
                               indent_++;
                               printExpr(arm.body);
                               indent_--;
                           }
                           indent_--;
                           indent_--;
                       },
                       [&](AwaitExpr* a) {
                           if (!a)
                               return;
                           printLine("AwaitExpr");
                           indent_++;
                           printExpr(a->value);
                           indent_--;
                       },
                       [&](SpawnExpr* s) {
                           if (!s)
                               return;
                           printLine("SpawnExpr");
                           indent_++;
                           if (s->value)
                               printExpr(s->value);
                           indent_--;
                       },
                       [&](CompositeLiteral* cl) {
                           if (!cl)
                               return;
                           indent();
                           out_ << "CompositeLiteral Type: ";
                           printTypeExpr(cl->typeExpr);
                           out_ << "\n";
                           indent_++;
                           for (const auto& el : cl->elements) {
                               printExpr(el.value);
                           }
                           indent_--;
                       },
                       [&](FieldAccess* fa) {
                           if (!fa)
                               return;
                           printLine(std::format(
                               "FieldAccess: .{}", fa->field ? sym_.resolve(fa->field->name) : ""));
                           indent_++;
                           printExpr(fa->object);
                           indent_--;
                       },
                       [&](IndexExpr* idx) {
                           if (!idx)
                               return;
                           printLine("IndexExpr");
                           indent_++;
                           printExpr(idx->left);
                           printExpr(idx->index);
                           indent_--;
                       },
                       [&](SliceExpr* sl) {
                           if (!sl)
                               return;
                           printLine("SliceExpr");
                           indent_++;
                           printExpr(sl->left);
                           printExpr(sl->low);
                           printExpr(sl->high);
                           indent_--;
                       },
                       [&](TypeExprWrapper* tw) {
                           if (!tw)
                               return;
                           indent();
                           out_ << "TypeExprWrapper: ";
                           printTypeExpr(tw->typeExpr);
                           out_ << "\n";
                       },
                       [&](BlockStmt* b) { printStmt(b); }, [&](TaggedUnionConstructExpr* tuce) {},
                       [&](TaggedUnionAccessExpr* tuae) {}, [&](IntrinsicCallExpr* ice) {},
                       [&](CastExpr* ce) {} },
            expr);
    }

    void printTypeExpr(const TypeExpr& typeExpr)
    {
        std::visit(overloaded { [&](std::monostate) { out_ << "<none>"; },
                       [&](NamedTypeExpr* n) {
                           if (n && n->name)
                               out_ << sym_.resolve(n->name->name);
                       },
                       [&](ArrayTypeExpr* a) {
                           if (!a)
                               return;
                           out_ << "[" << a->size << "]";
                           printTypeExpr(a->base);
                       },
                       [&](StructTypeExpr* s) {
                           if (!s)
                               return;
                           out_ << "Struct { ";
                           for (const auto& f : s->fields) {
                               out_ << sym_.resolve(f.name) << " ";
                           }
                           out_ << "}";
                       },
                       [&](SumTypeExpr* st) {
                           if (!st)
                               return;
                           out_ << "SumType [";
                           for (const auto& v : st->variants) {
                               out_ << sym_.resolve(v.name) << " ";
                           }
                           out_ << "]";
                       },
                       [&](GenericTypeExpr* g) {
                           if (!g)
                               return;
                           out_ << (g->name ? sym_.resolve(g->name->name) : "") << "<";
                           for (size_t i = 0; i < g->args.size(); ++i) {
                               if (i > 0)
                                   out_ << ", ";
                               printTypeExpr(g->args[i]);
                           }
                           out_ << ">";
                       } },
            typeExpr);
    }

    void printPattern(const Pattern& pattern)
    {
        std::visit(overloaded { [&](std::monostate) { printLine("<none>"); },
                       [&](WildcardPattern*) { printLine("Wildcard (_)"); },
                       [&](IdentifierPattern* ip) {
                           if (ip)
                               printLine(
                                   std::format("IdentifierPattern: {}", sym_.resolve(ip->name)));
                       },
                       [&](LiteralPattern* lp) {
                           if (!lp)
                               return;
                           printLine("LiteralPattern:");
                           indent_++;
                           printExpr(lp->value);
                           indent_--;
                       },
                       [&](CompositePattern* cp) {
                           if (!cp)
                               return;
                           indent();
                           out_ << "CompositePattern Type: ";
                           printTypeExpr(cp->typeExpr);
                           out_ << "\n";
                           indent_++;
                           for (const auto& elem : cp->elements) {
                               printPattern(elem.pattern);
                           }
                           indent_--;
                       } },
            pattern);
    }

private:
    const SymbolTable& sym_;
    std::ostream& out_;
    int indent_ = 0;

    void indent()
    {
        for (int i = 0; i < indent_; ++i) {
            out_ << "  ";
        }
    }

    void printLine(const std::string& text)
    {
        indent();
        out_ << text << "\n";
    }
};

} // namespace maml::ast