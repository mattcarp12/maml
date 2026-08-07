#include "ast.h"
#include "compiler_context.h"
#include "passes.h"
#include "semantic_tables.h"
#include "sym.h"
#include "token.h"
#include "types.h"

#include <format>
#include <string_view>
#include <variant>
#include <vector>

namespace maml::sema {

// C++ pattern matching helper
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

// =============================================================================
// Program, Function & Statement AST Traversals
// =============================================================================

void DesugarPass::run(ast::Program* program)
{
    if (program) {
        visit(*program);
    }
}

void DesugarPass::visit(ast::Program& program)
{
    for (auto& decl : program.decls) {
        std::visit(overloaded { [](std::monostate) {},
                       [this](ast::FnDecl* fn) {
                           if (fn) {
                               visit(*fn);
                           }
                       },
                       [](ast::TypeDecl*) {}, [](ast::Program*) {} },
            decl);
    }
}

void DesugarPass::visit(ast::FnDecl& fn)
{
    if (!fn.isExtern && fn.body) {
        visit(*fn.body);
    }
}

void DesugarPass::visit(ast::BlockStmt& block)
{
    for (auto& stmt : block.statements) {
        std::visit(overloaded { [](std::monostate) {},
                       [this](ast::BlockStmt* b) {
                           if (b)
                               visit(*b);
                       },
                       [this](ast::DeclareStmt* d) {
                           if (d)
                               d->value = desugarExpr(d->value);
                       },
                       [this](ast::AssignStmt* a) {
                           if (a) {
                               a->lValue = desugarExpr(a->lValue);
                               a->rValue = desugarExpr(a->rValue);
                           }
                       },
                       [this](ast::ExprStmt* e) {
                           if (e)
                               e->value = desugarExpr(e->value);
                       },
                       [this](ast::ReturnStmt* r) {
                           if (r)
                               r->value = desugarExpr(r->value);
                       },
                       [this](ast::YieldStmt* y) {
                           if (y)
                               y->value = desugarExpr(y->value);
                       },
                       [this](ast::ForStmt* f) {
                           if (f) {
                               f->condition = desugarExpr(f->condition);
                               if (f->body)
                                   visit(*f->body);
                           }
                       },
                       [this](ast::VecPushStmt* v) {
                           if (v) {
                               v->lValue = desugarExpr(v->lValue);
                               v->rValue = desugarExpr(v->rValue);
                           }
                       },
                       [](auto*) {} },
            stmt);
    }
}

// =============================================================================
// In-Place Expression Desugaring Traversal
// =============================================================================

ast::Expr DesugarPass::desugarExpr(ast::Expr expr)
{
    return std::visit(
        overloaded { [](std::monostate) -> ast::Expr { return std::monostate {}; },
            [this](ast::InfixExpr* infix) -> ast::Expr { return desugarInfixExpr(infix); },
            [this](ast::MatchExpr* match) -> ast::Expr { return desugarMatchExpr(match); },
            [this](ast::CallExpr* call) -> ast::Expr {
                if (call) {
                    call->function = desugarExpr(call->function);
                    for (auto& arg : call->arguments) {
                        arg.argument = desugarExpr(arg.argument);
                    }
                }
                return call;
            },
            [this](ast::PrefixExpr* prefix) -> ast::Expr {
                if (prefix) {
                    prefix->right = desugarExpr(prefix->right);
                }
                return prefix;
            },
            [this](ast::IfExpr* ifExpr) -> ast::Expr {
                if (ifExpr) {
                    ifExpr->condition = desugarExpr(ifExpr->condition);
                    if (ifExpr->consequence)
                        visit(*ifExpr->consequence);
                    if (ifExpr->alternative)
                        visit(*ifExpr->alternative);
                }
                return ifExpr;
            },
            [this](ast::AwaitExpr* await) -> ast::Expr {
                if (await) {
                    await->value = desugarExpr(await->value);
                }
                return await;
            },
            [this](ast::SpawnExpr* spawn) -> ast::Expr {
                if (spawn && spawn->value) {
                    ast::Expr desugared = desugarExpr(spawn->value);
                    if (auto** callPtr = std::get_if<ast::CallExpr*>(&desugared)) {
                        spawn->value = *callPtr;
                    }
                }
                return spawn;
            },
            [this](ast::CompositeLiteral* comp) -> ast::Expr {
                if (comp) {
                    for (auto& el : comp->elements) {
                        if (!std::holds_alternative<std::monostate>(el.key)) {
                            el.key = desugarExpr(el.key);
                        }
                        el.value = desugarExpr(el.value);
                    }
                }
                return comp;
            },
            [this](ast::FieldAccess* fa) -> ast::Expr {
                if (fa) {
                    fa->object = desugarExpr(fa->object);
                }
                return fa;
            },
            [this](ast::IndexExpr* idx) -> ast::Expr {
                if (idx) {
                    idx->left = desugarExpr(idx->left);
                    idx->index = desugarExpr(idx->index);
                }
                return idx;
            },
            [this](ast::SliceExpr* slice) -> ast::Expr {
                if (slice) {
                    slice->left = desugarExpr(slice->left);
                    if (!std::holds_alternative<std::monostate>(slice->low)) {
                        slice->low = desugarExpr(slice->low);
                    }
                    if (!std::holds_alternative<std::monostate>(slice->high)) {
                        slice->high = desugarExpr(slice->high);
                    }
                }
                return slice;
            },
            [this](ast::BlockStmt* block) -> ast::Expr {
                if (block)
                    visit(*block);
                return block;
            },
            [](ast::Identifier* id) -> ast::Expr { return id; },
            [](ast::IntLiteral* lit) -> ast::Expr { return lit; },
            [](ast::BoolLiteral* lit) -> ast::Expr { return lit; },
            [](ast::StringLiteral* lit) -> ast::Expr { return lit; },
            [](ast::TypeExprWrapper* tw) -> ast::Expr { return tw; } },
        expr);
}

// =============================================================================
// Short-Circuiting Infix Rewrites (`&&` / `||`)
// =============================================================================

ast::Expr DesugarPass::desugarInfixExpr(ast::InfixExpr* infix)
{
    if (!infix)
        return std::monostate {};

    const bool isAnd = (infix->op == TokenType::AND);
    const bool isOr = (infix->op == TokenType::OR);

    if (!isAnd && !isOr) {
        infix->left = desugarExpr(infix->left);
        infix->right = desugarExpr(infix->right);
        return infix;
    }

    const types::Type* boolType = ctx_.types.registry.getPrimitive(types::TypeKind::Bool);

    auto* ifExpr = ctx_.arena.make<ast::IfExpr>();
    ifExpr->pos = infix->pos;
    ifExpr->end = infix->end;

    ifExpr->condition = desugarExpr(infix->left);
    ctx_.semantic.setTypeOf(ifExpr, boolType);

    ast::Expr desugaredRight = desugarExpr(infix->right);

    if (isAnd) {
        ifExpr->consequence = makeExprBlock(desugaredRight, boolType, infix->end);
        ifExpr->alternative
            = makeExprBlock(makeBoolLiteral(false, infix->end), boolType, infix->end);
    } else {
        ifExpr->consequence
            = makeExprBlock(makeBoolLiteral(true, infix->end), boolType, infix->end);
        ifExpr->alternative = makeExprBlock(desugaredRight, boolType, infix->end);
    }

    return ifExpr;
}

// =============================================================================
// Match Expression Decision Tree Rewrites
// =============================================================================

ast::Expr DesugarPass::makeDiscriminantCheck(ast::Expr subjectRef, int discriminant, Position pos)
{
    const types::Type* i32Type = ctx_.types.registry.getPrimitive(types::TypeKind::I32);
    const types::Type* boolType = ctx_.types.registry.getPrimitive(types::TypeKind::Bool);

    auto* fieldAccess = ctx_.arena.make<ast::FieldAccess>();
    fieldAccess->pos = pos;
    fieldAccess->end = pos;
    fieldAccess->object = subjectRef;
    fieldAccess->field = ctx_.arena.make<ast::Identifier>();
    fieldAccess->field->pos = pos;
    fieldAccess->field->end = pos;
    fieldAccess->field->name = ctx_.symbols.intern("discriminant");

    ctx_.semantic.setTypeOf(fieldAccess->field, i32Type);
    ctx_.semantic.setTypeOf(fieldAccess, i32Type);

    auto* tagLit = ctx_.arena.make<ast::IntLiteral>();
    tagLit->pos = pos;
    tagLit->end = pos;
    tagLit->value = discriminant;
    ctx_.semantic.setTypeOf(tagLit, i32Type);

    auto* eqExpr = ctx_.arena.make<ast::InfixExpr>();
    eqExpr->pos = pos;
    eqExpr->end = pos;
    eqExpr->left = fieldAccess;
    eqExpr->op = TokenType::EQ;
    eqExpr->right = tagLit;
    ctx_.semantic.setTypeOf(eqExpr, boolType);

    return eqExpr;
}

void DesugarPass::extractPatternBindings(ast::BlockStmt* block, const ast::Pattern& pattern,
    ast::Expr subjectRef, const types::Type* sumType, int variantIndex, Position pos)
{
    if (!sumType || sumType->kind != types::TypeKind::Sum || !block) {
        return;
    }

    const auto& sumPayload = std::get<types::SumPayload>(sumType->payload);
    if (variantIndex < 0 || static_cast<size_t>(variantIndex) >= sumPayload.variants.size()) {
        return;
    }

    const auto& variant = sumPayload.variants[variantIndex];

    std::vector<types::StructField> payloadFields;
    for (size_t i = 0; i < variant.tupleTypes.size(); ++i) {
        payloadFields.push_back({ .name = ctx_.symbols.intern(std::format("payload_{}", i)),
            .type = variant.tupleTypes[i] });
    }
    for (const auto& field : variant.fields) {
        payloadFields.push_back(field);
    }

    std::visit(overloaded { [](std::monostate) {}, [](ast::WildcardPattern*) {},
                   [](ast::LiteralPattern*) {},
                   [&](ast::IdentifierPattern* idPat) {
                       if (!idPat)
                           return;

                       for (const auto& v : sumPayload.variants) {
                           if (v.name == idPat->name) {
                               return;
                           }
                       }

                       auto* decl = ctx_.arena.make<ast::DeclareStmt>();
                       decl->pos = pos;
                       decl->end = pos;
                       decl->isMutable = false;
                       decl->name = idPat->name;
                       decl->value = subjectRef;

                       block->statements.insert(block->statements.begin(), decl);
                   },
                   [&](ast::CompositePattern* compPat) {
                       if (!compPat || payloadFields.empty())
                           return;

                       for (size_t i = 0; i < compPat->elements.size(); ++i) {
                           const auto& elem = compPat->elements[i];
                           if (auto* idPat = std::get_if<ast::IdentifierPattern*>(&elem.pattern);
                               idPat && *idPat) {
                               if (i >= payloadFields.size())
                                   break;

                               auto* fa = ctx_.arena.make<ast::FieldAccess>();
                               fa->pos = pos;
                               fa->end = pos;
                               fa->object = subjectRef;
                               fa->field = ctx_.arena.make<ast::Identifier>();
                               fa->field->pos = pos;
                               fa->field->end = pos;
                               fa->field->name = payloadFields[i].name;
                               ctx_.semantic.setTypeOf(fa, payloadFields[i].type);

                               auto* decl = ctx_.arena.make<ast::DeclareStmt>();
                               decl->pos = pos;
                               decl->end = pos;
                               decl->isMutable = false;
                               decl->name = (*idPat)->name;
                               decl->value = fa;

                               block->statements.insert(block->statements.begin() + i, decl);
                           }
                       }
                   } },
        pattern);
}

ast::Expr DesugarPass::desugarMatchExpr(ast::MatchExpr* match)
{
    if (!match)
        return std::monostate {};

    const types::Type* originalSumType
        = std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                         [this](auto* node) -> const types::Type* {
                             return node ? ctx_.semantic.typeOf(node) : nullptr;
                         } },
            match->subject);

    if (!originalSumType || originalSumType->kind != types::TypeKind::Sum) {
        return match;
    }

    ast::Expr subjectAST = desugarExpr(match->subject);

    const types::Type* matchRetType = ctx_.semantic.typeOf(match);
    const auto& sumPayload = std::get<types::SumPayload>(originalSumType->payload);

    SymID subjSym = ctx_.symbols.intern(std::format("__match_subj_{}", matchSubjCounter_++));
    auto* subjDecl = ctx_.arena.make<ast::DeclareStmt>();
    subjDecl->pos = match->pos;
    subjDecl->end = match->pos;
    subjDecl->isMutable = false;
    subjDecl->name = subjSym;
    subjDecl->value = subjectAST;

    auto makeSubjectRef = [&]() -> ast::Expr {
        auto* ident = ctx_.arena.make<ast::Identifier>();
        ident->pos = match->pos;
        ident->end = match->pos;
        ident->name = subjSym;
        ctx_.semantic.setTypeOf(ident, originalSumType);
        return ident;
    };

    ast::BlockStmt* currentElse = nullptr;

    for (size_t i = match->arms.size(); i > 0; --i) {
        auto& arm = match->arms[i - 1];

        ast::Expr desugaredArmBody = desugarExpr(arm.body);
        ast::BlockStmt* consequenceBlock = nullptr;

        if (auto* block = std::get_if<ast::BlockStmt*>(&desugaredArmBody); block && *block) {
            consequenceBlock = *block;
        } else {
            consequenceBlock = makeExprBlock(desugaredArmBody, matchRetType, arm.pos);
        }

        SymID variantName = NoSymbol;
        std::visit(overloaded { [](auto&&) {},
                       [&](ast::IdentifierPattern* idPat) {
                           if (idPat)
                               variantName = idPat->name;
                       },
                       [&](ast::CompositePattern* compPat) {
                           if (compPat) {
                               if (auto* nte = std::get_if<ast::NamedTypeExpr*>(&compPat->typeExpr);
                                   nte && *nte && (*nte)->name) {
                                   variantName = (*nte)->name->name;
                               }
                           }
                       } },
            arm.pattern);

        int variantDiscriminant = 0;
        int variantIndex = 0;
        for (size_t v = 0; v < sumPayload.variants.size(); ++v) {
            if (sumPayload.variants[v].name == variantName) {
                variantDiscriminant = sumPayload.variants[v].discriminant;
                variantIndex = static_cast<int>(v);
                break;
            }
        }

        // Extract pattern bindings into the consequence block for this arm
        extractPatternBindings(consequenceBlock, arm.pattern, makeSubjectRef(), originalSumType,
            variantIndex, arm.pos);

        // --- THE CLEAN FIX ---
        // Since ControlFlowPass guarantees exhaustiveness, the final arm in the match
        // (whether wildcard '_' or the last variant) becomes our unconditional fallback 'else'
        // block!
        if (i == match->arms.size()) {
            currentElse = consequenceBlock;
            continue;
        }
        // ---------------------

        auto* ifExpr = ctx_.arena.make<ast::IfExpr>();
        ifExpr->pos = arm.pos;
        ifExpr->end = arm.end;
        ifExpr->condition = makeDiscriminantCheck(makeSubjectRef(), variantDiscriminant, arm.pos);
        ifExpr->consequence = consequenceBlock;
        ifExpr->alternative = currentElse;

        ctx_.semantic.setTypeOf(ifExpr, matchRetType);
        currentElse = makeExprBlock(ifExpr, matchRetType, arm.pos);
    }

    auto* enclosingBlock = ctx_.arena.make<ast::BlockStmt>();
    enclosingBlock->pos = match->pos;
    enclosingBlock->end = match->end;
    ctx_.semantic.setTypeOf(enclosingBlock, matchRetType);
    enclosingBlock->statements.push_back(subjDecl);

    if (currentElse && !currentElse->statements.empty()) {
        enclosingBlock->statements.push_back(currentElse->statements[0]);
    }

    return enclosingBlock;
}

ast::BlockStmt* DesugarPass::makeExprBlock(
    ast::Expr expr, const types::Type* blockType, Position pos)
{
    auto* exprStmt = ctx_.arena.make<ast::ExprStmt>();
    exprStmt->pos = pos;
    exprStmt->end = pos;
    exprStmt->value = expr;

    auto* block = ctx_.arena.make<ast::BlockStmt>();
    block->pos = pos;
    block->end = pos;
    block->statements.emplace_back(exprStmt);
    ctx_.semantic.setTypeOf(block, blockType);
    return block;
}

ast::Expr DesugarPass::makeBoolLiteral(bool val, Position pos)
{
    auto* boolLit = ctx_.arena.make<ast::BoolLiteral>();
    boolLit->pos = pos;
    boolLit->end = pos;
    boolLit->value = val;
    ctx_.semantic.setTypeOf(boolLit, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
    return boolLit;
}

} // namespace maml::sema