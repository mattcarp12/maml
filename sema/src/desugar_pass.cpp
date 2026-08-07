#include "ast.h"
#include "capability.h"
#include "compiler_context.h"
#include "intrinsics.h"
#include "passes.h"
#include "semantic_tables.h"
#include "sym.h"
#include "token.h"
#include "types.h"

#include <cstddef>
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
        stmt = desugarStmt(stmt);
    }
}

const types::Type* DesugarPass::typeOfExpr(const ast::Expr& expr)
{
    return std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                          [this](auto* node) -> const types::Type* {
                              return node ? ctx_.semantic.typeOf(node) : nullptr;
                          } },
        expr);
};

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
                if (!idx)
                    return std::monostate {};

                const types::Type* sourceType = typeOfExpr(idx->left);
                if (sourceType && sourceType->kind == types::TypeKind::Map) {
                    return desugarMapGet(idx);
                }

                idx->left = desugarExpr(idx->left);
                idx->index = desugarExpr(idx->index);
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

    const types::Type* originalSubjectType
        = std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                         [this](auto* node) -> const types::Type* {
                             return node ? ctx_.semantic.typeOf(node) : nullptr;
                         } },
            match->subject);

    if (!originalSubjectType) {
        return match;
    }

    ast::Expr subjectAST = desugarExpr(match->subject);
    const types::Type* matchRetType = ctx_.semantic.typeOf(match);

    // 1. Bind the match subject to a temporary variable: let __match_subj_N = subject;
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
        ctx_.semantic.setTypeOf(ident, originalSubjectType);
        return ident;
    };

    ast::BlockStmt* currentElse = nullptr;

    // 2. Iterate backwards over match arms to build the nested if/else decision tree
    for (size_t i = match->arms.size(); i > 0; --i) {
        auto& arm = match->arms[i - 1];

        ast::Expr desugaredArmBody = desugarExpr(arm.body);
        ast::BlockStmt* consequenceBlock = nullptr;

        if (auto* block = std::get_if<ast::BlockStmt*>(&desugaredArmBody); block && *block) {
            consequenceBlock = *block;
        } else {
            consequenceBlock = makeExprBlock(desugaredArmBody, matchRetType, arm.pos);
        }

        if (originalSubjectType->kind == types::TypeKind::Sum) {
            SymID variantName = NoSymbol;
            std::visit(
                overloaded { [](auto&&) {},
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

            const auto& sumPayload = std::get<types::SumPayload>(originalSubjectType->payload);
            int variantIndex = 0;
            for (size_t v = 0; v < sumPayload.variants.size(); ++v) {
                if (sumPayload.variants[v].name == variantName) {
                    variantIndex = static_cast<int>(v);
                    break;
                }
            }

            // Unpacks payload fields (e.g., let c = __match_subj_N.payload_0;) into
            // consequenceBlock
            extractPatternBindings(consequenceBlock, arm.pattern, makeSubjectRef(),
                originalSubjectType, variantIndex, arm.pos);
        } else {
            // Also support catch-all identifier pattern bindings on non-Sum types (e.g., other =>
            // ...)
            if (auto* idPat = std::get_if<ast::IdentifierPattern*>(&arm.pattern); idPat && *idPat) {
                auto* decl = ctx_.arena.make<ast::DeclareStmt>();
                decl->pos = arm.pos;
                decl->end = arm.pos;
                decl->isMutable = false;
                decl->name = (*idPat)->name;
                decl->value = makeSubjectRef();
                consequenceBlock->statements.insert(consequenceBlock->statements.begin(), decl);
            }
        }

        // The final arm (wildcard '_' or catch-all) becomes our unconditional fallback 'else' block
        if (i == match->arms.size()) {
            currentElse = consequenceBlock;
            continue;
        }

        ast::Expr condExpr = std::monostate {};

        // --- Branch condition generation for Sum vs. Non-Sum types ---
        if (originalSubjectType->kind == types::TypeKind::Sum) {
            SymID variantName = NoSymbol;
            std::visit(
                overloaded { [](auto&&) {},
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

            const auto& sumPayload = std::get<types::SumPayload>(originalSubjectType->payload);
            int variantDiscriminant = 0;
            for (size_t v = 0; v < sumPayload.variants.size(); ++v) {
                if (sumPayload.variants[v].name == variantName) {
                    variantDiscriminant = sumPayload.variants[v].discriminant;
                    break;
                }
            }
            condExpr = makeDiscriminantCheck(makeSubjectRef(), variantDiscriminant, arm.pos);
        } else {
            // For primitive/scalar types (integers, booleans, strings), compare via `==`
            if (auto* litPatPtr = std::get_if<ast::LiteralPattern*>(&arm.pattern);
                litPatPtr && *litPatPtr) {
                auto* eqExpr = ctx_.arena.make<ast::InfixExpr>();
                eqExpr->pos = arm.pos;
                eqExpr->end = arm.end;
                eqExpr->left = makeSubjectRef();
                eqExpr->op = TokenType::EQ;
                eqExpr->right = desugarExpr((*litPatPtr)->value);
                ctx_.semantic.setTypeOf(
                    eqExpr, ctx_.types.registry.getPrimitive(types::TypeKind::Bool));
                ctx_.semantic.setValueCategory(eqExpr, ValueCategory::RValue);
                condExpr = eqExpr;
            } else {
                condExpr = makeBoolLiteral(true, arm.pos);
            }
        }

        auto* ifExpr = ctx_.arena.make<ast::IfExpr>();
        ifExpr->pos = arm.pos;
        ifExpr->end = arm.end;
        ifExpr->condition = condExpr;
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

ast::Stmt DesugarPass::desugarStmt(ast::Stmt stmt)
{
    return std::visit(overloaded { [](std::monostate) -> ast::Stmt { return std::monostate {}; },
                          [this](ast::BlockStmt* b) -> ast::Stmt {
                              if (b)
                                  visit(*b);
                              return b;
                          },
                          [this](ast::DeclareStmt* d) -> ast::Stmt {
                              if (d)
                                  d->value = desugarExpr(d->value);
                              return d;
                          },
                          [this](ast::AssignStmt* a) -> ast::Stmt {
                              if (!a)
                                  return a;

                              // --- MAP ASSIGNMENT INTERCEPTION ---
                              if (auto** idxPtr = std::get_if<ast::IndexExpr*>(&a->lValue);
                                  idxPtr && *idxPtr) {
                                  ast::IndexExpr* idx = *idxPtr;
                                  const types::Type* sourceType = typeOfExpr(idx->left);
                                  if (sourceType && sourceType->kind == types::TypeKind::Map) {
                                      return desugarMapAssign(a, idx);
                                  }
                              }

                              a->lValue = desugarExpr(a->lValue);
                              a->rValue = desugarExpr(a->rValue);
                              return a;
                          },
                          [this](ast::ExprStmt* e) -> ast::Stmt {
                              if (e)
                                  e->value = desugarExpr(e->value);
                              return e;
                          },
                          [this](ast::ReturnStmt* r) -> ast::Stmt {
                              if (r)
                                  r->value = desugarExpr(r->value);
                              return r;
                          },
                          [this](ast::YieldStmt* y) -> ast::Stmt {
                              if (y)
                                  y->value = desugarExpr(y->value);
                              return y;
                          },
                          [this](ast::ForStmt* f) -> ast::Stmt {
                              if (f) {
                                  f->init = desugarStmt(f->init);
                                  f->condition = desugarExpr(f->condition);
                                  f->post = desugarStmt(f->post);
                                  if (f->body)
                                      visit(*f->body);
                              }
                              return f;
                          },
                          [this](ast::VecPushStmt* v) -> ast::Stmt {
                              if (v) {
                                  v->lValue = desugarExpr(v->lValue);
                                  v->rValue = desugarExpr(v->rValue);
                              }
                              return v;
                          },
                          [](auto* s) -> ast::Stmt { return s; } },
        stmt);
}

ast::ExprStmt* DesugarPass::desugarMapAssign(ast::AssignStmt* assign, ast::IndexExpr* idx)
{
    // 1. Recursively desugar sub-expressions (in case of nested matches/lookups)
    ast::Expr mapExpr = desugarExpr(idx->left);
    ast::Expr keyExpr = desugarExpr(idx->index);
    ast::Expr valExpr = desugarExpr(assign->rValue);

    // 2. Create the identifier node for "__builtin_map_put"
    auto* fnId = ctx_.arena.make<ast::Identifier>();
    fnId->pos = assign->pos;
    fnId->end = assign->pos;
    fnId->name = ctx_.symbols.intern(intrinsics::MAP_PUT);

    // 3. Construct the CallExpr: __builtin_map_put(m, key, val)
    auto* call = ctx_.arena.make<ast::CallExpr>();
    call->pos = assign->pos;
    call->end = assign->end;
    call->function = fnId;

    // Arg 0: Map container
    // We pass `m` with Capability::Mut so that MIR's CallExpr builder automatically
    // invokes `addressOf(m)`, passing a pointer to the map struct to the runtime.
    call->arguments.push_back(
        ast::CallArg { .argument = mapExpr, .cap = Capability::Mut, .pos = assign->pos });

    // Arg 1: Key
    call->arguments.push_back(
        ast::CallArg { .argument = keyExpr, .cap = Capability::Ro, .pos = assign->pos });

    // Arg 2: Value
    call->arguments.push_back(
        ast::CallArg { .argument = valExpr, .cap = Capability::Own, .pos = assign->pos });

    // 4. Decorate semantic tables: __builtin_map_put returns 'unit'
    const types::Type* unitType = ctx_.types.registry.getPrimitive(types::TypeKind::Unit);
    ctx_.semantic.setTypeOf(call, unitType);
    ctx_.semantic.setValueCategory(call, ValueCategory::RValue);

    // 5. Wrap the call in an ExprStmt and return it
    auto* exprStmt = ctx_.arena.make<ast::ExprStmt>();
    exprStmt->pos = assign->pos;
    exprStmt->end = assign->end;
    exprStmt->value = call;

    return exprStmt;
}

ast::BlockStmt* DesugarPass::desugarMapGet(ast::IndexExpr* idx)
{
    // 1. Recursively desugar sub-expressions
    ast::Expr mapExpr = desugarExpr(idx->left);
    ast::Expr keyExpr = desugarExpr(idx->index);

    // 2. Resolve semantic types
    // const types::Type* sourceType = ctx_.semantic.typeOf(&(idx->left));
    const types::Type* sourceType = typeOfExpr(idx->left);
    const auto& mapPayload = std::get<types::MapPayload>(sourceType->payload);
    const types::Type* valType = mapPayload.value;
    const types::Type* optionType = ctx_.semantic.typeOf(idx); // Option<V>
    const types::Type* ptrType = ctx_.types.registry.getPrimitive(types::TypeKind::Ptr);
    const types::Type* boolType = ctx_.types.registry.getPrimitive(types::TypeKind::Bool);
    const types::Type* i64Type = ctx_.types.registry.getPrimitive(types::TypeKind::I64);

    // 3. Create a unique temporary variable name for the raw pointer
    SymID rawPtrSym = ctx_.symbols.intern(std::format("__map_raw_ptr_{}", mapSubjCounter_++));

    // 4. Build CallExpr: __builtin_map_get(m, key) -> ptr
    auto* fnId = ctx_.arena.make<ast::Identifier>();
    fnId->pos = idx->pos;
    fnId->end = idx->pos;
    fnId->name = ctx_.symbols.intern(intrinsics::MAP_GET);

    auto* getCall = ctx_.arena.make<ast::CallExpr>();
    getCall->pos = idx->pos;
    getCall->end = idx->end;
    getCall->function = fnId;
    getCall->arguments.push_back(ast::CallArg { .argument = mapExpr,
        .cap = Capability::Mut, // Triggers addressOf(m) automatically in MIR
        .pos = idx->pos });
    getCall->arguments.push_back(
        ast::CallArg { .pos = idx->pos, .cap = Capability::Ro, .argument = keyExpr });
    ctx_.semantic.setTypeOf(getCall, ptrType);
    ctx_.semantic.setValueCategory(getCall, ValueCategory::RValue);

    // 5. Build statement: let __map_raw_ptr_N = __builtin_map_get(m, key);
    auto* declStmt = ctx_.arena.make<ast::DeclareStmt>();
    declStmt->pos = idx->pos;
    declStmt->end = idx->pos;
    declStmt->isMutable = false;
    declStmt->name = rawPtrSym;
    declStmt->value = getCall;

    // 6. Build condition: __map_raw_ptr_N != 0
    auto* condId = ctx_.arena.make<ast::Identifier>();
    condId->pos = idx->pos;
    condId->end = idx->pos;
    condId->name = rawPtrSym;
    ctx_.semantic.setTypeOf(condId, ptrType);
    ctx_.semantic.setValueCategory(condId, ValueCategory::RValue);

    auto* zeroLit = ctx_.arena.make<ast::IntLiteral>();
    zeroLit->pos = idx->pos;
    zeroLit->end = idx->pos;
    zeroLit->value = 0;
    ctx_.semantic.setTypeOf(zeroLit, i64Type);
    ctx_.semantic.setValueCategory(zeroLit, ValueCategory::RValue);

    auto* condExpr = ctx_.arena.make<ast::InfixExpr>();
    condExpr->pos = idx->pos;
    condExpr->end = idx->pos;
    condExpr->left = condId;
    condExpr->op = TokenType::NOT_EQ;
    condExpr->right = zeroLit;
    ctx_.semantic.setTypeOf(condExpr, boolType);
    ctx_.semantic.setValueCategory(condExpr, ValueCategory::RValue);

    // 7. Build consequence: Some(__map_raw_ptr_N)
    // NOTE: Setting the semantic type of `derefId` to `valType` triggers MIR's
    // automatic pointer-dereferencing mechanism (`emitLoad`) when evaluated!
    auto* derefId = ctx_.arena.make<ast::Identifier>();
    derefId->pos = idx->pos;
    derefId->end = idx->pos;
    derefId->name = rawPtrSym;
    ctx_.semantic.setTypeOf(derefId, valType); // <-- Key to auto-dereferencing!
    ctx_.semantic.setValueCategory(derefId, ValueCategory::LValue);

    auto* someId = ctx_.arena.make<ast::Identifier>();
    someId->pos = idx->pos;
    someId->end = idx->pos;
    someId->name = ctx_.symbols.intern("Some");
    ctx_.semantic.setTypeOf(someId, optionType);

    auto* someCall = ctx_.arena.make<ast::CallExpr>();
    someCall->pos = idx->pos;
    someCall->end = idx->end;
    someCall->function = someId;
    someCall->arguments.push_back(
        ast::CallArg { .pos = idx->pos, .cap = Capability::Own, .argument = derefId });
    ctx_.semantic.setTypeOf(someCall, optionType);
    ctx_.semantic.setValueCategory(someCall, ValueCategory::RValue);

    ast::BlockStmt* consequenceBlock = makeExprBlock(someCall, optionType, idx->pos);

    // 8. Build alternative: None
    auto* noneId = ctx_.arena.make<ast::Identifier>();
    noneId->pos = idx->pos;
    noneId->end = idx->pos;
    noneId->name = ctx_.symbols.intern("None");
    ctx_.semantic.setTypeOf(noneId, optionType);
    ctx_.semantic.setValueCategory(noneId, ValueCategory::RValue);

    ast::BlockStmt* alternativeBlock = makeExprBlock(noneId, optionType, idx->pos);

    // 9. Build IfExpr: if (cond) { Some(...) } else { None }
    auto* ifExpr = ctx_.arena.make<ast::IfExpr>();
    ifExpr->pos = idx->pos;
    ifExpr->end = idx->end;
    ifExpr->condition = condExpr;
    ifExpr->consequence = consequenceBlock;
    ifExpr->alternative = alternativeBlock;
    ctx_.semantic.setTypeOf(ifExpr, optionType);
    ctx_.semantic.setValueCategory(ifExpr, ValueCategory::RValue);

    // 10. Wrap in enclosing block expression and return
    auto* enclosingBlock = ctx_.arena.make<ast::BlockStmt>();
    enclosingBlock->pos = idx->pos;
    enclosingBlock->end = idx->end;
    ctx_.semantic.setTypeOf(enclosingBlock, optionType);
    ctx_.semantic.setValueCategory(enclosingBlock, ValueCategory::RValue);

    enclosingBlock->statements.push_back(declStmt);

    auto* trailingExprStmt = ctx_.arena.make<ast::ExprStmt>();
    trailingExprStmt->pos = idx->pos;
    trailingExprStmt->end = idx->end;
    trailingExprStmt->value = ifExpr;
    enclosingBlock->statements.push_back(trailingExprStmt);

    return enclosingBlock;
}

} // namespace maml::sema