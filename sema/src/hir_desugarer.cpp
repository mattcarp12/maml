#include "hir_desugarer.h"
#include "arena.h"
#include "ast.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include "types.h"
#include <cstddef>
#include <format>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

namespace maml::hir {

// Helper for clean std::visit pattern matching
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

Desugarer::Desugarer(types::TypeRegistry& registry, SymbolTable& sym, Arena& arena)
    : registry_(registry)
    , sym_(sym)
    , arena_(arena)
{
}

void Desugarer::desugar(ast::Program* program)
{
    if (!program)
        return;

    for (auto& decl : program->decls) {
        desugarDecl(decl);
    }
}

// =============================================================================
// 1. Declaration Traversal
// =============================================================================

void Desugarer::desugarDecl(ast::Decl& decl)
{
    std::visit(overloaded { [](std::monostate) {}, [this](ast::FnDecl* fn) { desugarFnDecl(fn); },
                   [](ast::TypeDecl*) {
                       // Type declarations are structural; no executable expressions to desugar
                   },
                   [this](ast::Program* prog) { desugar(prog); } },
        decl);
}

void Desugarer::desugarFnDecl(ast::FnDecl* fn)
{
    if (!fn || fn->isExtern)
        return;

    desugarBlockStmt(fn->body);
}

// =============================================================================
// 2. Statement Traversal
// =============================================================================

void Desugarer::desugarStmt(ast::Stmt& stmt)
{
    std::visit(
        overloaded { [](std::monostate) {},
            [this](ast::BlockStmt* block) { desugarBlockStmt(block); },
            [this](ast::DeclareStmt* decl) { desugarDeclareStmt(decl); },
            [this](ast::AssignStmt* assign) { desugarAssignStmt(assign); },
            [this](ast::ExprStmt* exprStmt) { desugarExprStmt(exprStmt); },
            [this](ast::ReturnStmt* ret) { desugarReturnStmt(ret); },
            [this](ast::YieldStmt* yld) { desugarYieldStmt(yld); },
            [this](ast::ForStmt* forStmt) { desugarForStmt(forStmt); }, [](ast::BreakStmt*) {},
            [](ast::ContinueStmt*) {}, [this](ast::AliasDecl* alias) { desugarExpr(alias->value); },
            [this](ast::VecPushStmt* push) { desugarVecPushStmt(push); } },
        stmt);
}

void Desugarer::desugarBlockStmt(ast::BlockStmt* block)
{
    if (!block)
        return;

    for (auto& stmt : block->statements) {
        desugarStmt(stmt);
    }
}

void Desugarer::desugarDeclareStmt(ast::DeclareStmt* decl) { desugarExpr(decl->value); }

void Desugarer::desugarAssignStmt(ast::AssignStmt* assign)
{
    desugarExpr(assign->lValue);
    desugarExpr(assign->rValue);
}

void Desugarer::desugarExprStmt(ast::ExprStmt* exprStmt) { desugarExpr(exprStmt->value); }

void Desugarer::desugarReturnStmt(ast::ReturnStmt* ret) { desugarExpr(ret->value); }

void Desugarer::desugarYieldStmt(ast::YieldStmt* yld) { desugarExpr(yld->value); }

void Desugarer::desugarForStmt(ast::ForStmt* forStmt)
{
    desugarStmt(forStmt->init);
    desugarExpr(forStmt->condition);
    desugarStmt(forStmt->post);
    desugarBlockStmt(forStmt->body);
}

void Desugarer::desugarVecPushStmt(ast::VecPushStmt* push)
{
    desugarExpr(push->lValue);
    desugarExpr(push->rValue);
}

// =============================================================================
// 3. Expression Traversal (In-Place Mutator)
// =============================================================================

void Desugarer::desugarExpr(ast::Expr& expr)
{
    std::visit(
        overloaded { [](std::monostate) {},
            [this, &expr](ast::Identifier*) { tryDesugarSumConstructor(expr); },
            [](ast::IntLiteral*) {}, [](ast::BoolLiteral*) {}, [](ast::StringLiteral*) {},
            [this, &expr](ast::InfixExpr* infix) { desugarInfixExpr(expr, infix); },
            [this](ast::PrefixExpr* prefix) { desugarPrefixExpr(prefix); },
            [this, &expr](ast::CallExpr* call) { desugarCallExpr(expr, call); },
            [this](ast::IfExpr* ifExpr) { desugarIfExpr(ifExpr); },
            [this, &expr](ast::MatchExpr* match) { desugarMatchExpr(expr, match); },
            [this](ast::AwaitExpr* awaitExpr) { desugarAwaitExpr(awaitExpr); },
            [this](ast::SpawnExpr* spawnExpr) { desugarSpawnExpr(spawnExpr); },
            [this](ast::CompositeLiteral* comp) { desugarCompositeLiteral(comp); },
            [this](ast::FieldAccess* field) { desugarFieldAccess(field); },
            [this](ast::IndexExpr* idx) { desugarIndexExpr(idx); },
            [this](ast::SliceExpr* slice) { desugarSliceExpr(slice); },
            [](ast::TypeExprWrapper*) {},
            [this](ast::BlockStmt* block) { desugarBlockStmt(block); },
            [this](ast::TaggedUnionConstructExpr* tagConst) {
                for (auto& arg : tagConst->payloadArgs) {
                    desugarExpr(arg);
                }
            },
            [this](ast::TaggedUnionAccessExpr* tagAccess) { desugarExpr(tagAccess->object); },
            [this](ast::IntrinsicCallExpr* intrinsic) {
                for (auto& arg : intrinsic->arguments) {
                    desugarExpr(arg);
                }
            },
            [this](ast::CastExpr* cast) { desugarCastExpr(cast); } },
        expr);
}

void Desugarer::desugarPrefixExpr(ast::PrefixExpr* prefix) { desugarExpr(prefix->right); }

void Desugarer::desugarIfExpr(ast::IfExpr* ifExpr)
{
    desugarExpr(ifExpr->condition);
    desugarBlockStmt(ifExpr->consequence);
    desugarBlockStmt(ifExpr->alternative);
}

void Desugarer::desugarAwaitExpr(ast::AwaitExpr* awaitExpr) { desugarExpr(awaitExpr->value); }

void Desugarer::desugarSpawnExpr(ast::SpawnExpr* spawnExpr)
{
    if (spawnExpr->value) {
        ast::Expr callRef = spawnExpr->value;
        desugarExpr(callRef);
    }
}

void Desugarer::desugarCompositeLiteral(ast::CompositeLiteral* comp)
{
    for (auto& elem : comp->elements) {
        if (!std::holds_alternative<std::monostate>(elem.key)) {
            desugarExpr(elem.key);
        }
        desugarExpr(elem.value);
    }
}

void Desugarer::desugarFieldAccess(ast::FieldAccess* field) { desugarExpr(field->object); }

void Desugarer::desugarIndexExpr(ast::IndexExpr* idx)
{
    desugarExpr(idx->left);
    desugarExpr(idx->index);
}

void Desugarer::desugarSliceExpr(ast::SliceExpr* slice)
{
    desugarExpr(slice->left);
    desugarExpr(slice->low);
    desugarExpr(slice->high);
}

void Desugarer::desugarCastExpr(ast::CastExpr* cast) { desugarExpr(cast->source); }

ast::BlockStmt* Desugarer::makeExprBlock(ast::Expr expr, const types::Type* blockType, Position pos)
{
    // Wrap the expression inside an ExprStmt so it can live in a BlockStmt
    auto* exprStmt = arena_.make<ast::ExprStmt>();
    exprStmt->pos = pos;
    exprStmt->end = pos;
    exprStmt->value = expr;

    auto* block = arena_.make<ast::BlockStmt>();
    block->pos = pos;
    block->end = pos;
    block->statements.emplace_back(exprStmt);
    block->exprType = blockType; // Explicitly annotate synthetic block type
    return block;
}

ast::Expr Desugarer::makeBoolLiteral(bool val, Position pos)
{
    auto* boolLit = arena_.make<ast::BoolLiteral>();
    boolLit->pos = pos;
    boolLit->end = pos;
    boolLit->value = val;
    boolLit->exprType = registry_.getPrimitive(types::TypeKind::Bool);
    return boolLit;
}

void Desugarer::desugarInfixExpr(ast::Expr& parentRef, ast::InfixExpr* infix)
{
    // 1. Bottom-up: recursively desugar children first
    desugarExpr(infix->left);
    desugarExpr(infix->right);

    // 2. Check for logical short-circuiting operators
    const bool isAnd = (infix->op == TokenType::AND);
    const bool isOr = (infix->op == TokenType::OR);

    if (!isAnd && !isOr) {
        return;
    }

    const types::Type* boolType = registry_.getPrimitive(types::TypeKind::Bool);

    auto* ifExpr = arena_.make<ast::IfExpr>();
    ifExpr->pos = infix->pos;
    ifExpr->end = infix->end;
    ifExpr->condition = infix->left;
    ifExpr->exprType = boolType; // Annotate synthetic IfExpr with bool type

    if (isAnd) {
        // a && b  ->  if a { b } else { false }
        ifExpr->consequence = makeExprBlock(infix->right, boolType, infix->end);
        ifExpr->alternative
            = makeExprBlock(makeBoolLiteral(false, infix->end), boolType, infix->end);
    } else {
        // a || b  ->  if a { true } else { b }
        ifExpr->consequence
            = makeExprBlock(makeBoolLiteral(true, infix->end), boolType, infix->end);
        ifExpr->alternative = makeExprBlock(infix->right, boolType, infix->end);
    }

    // 3. In-place replacement of the InfixExpr with the synthetic IfExpr
    parentRef = ifExpr;
}

// =============================================================================
// Phase 3: Builtin Intrinsic Call Rewrites
// =============================================================================

void Desugarer::desugarCallExpr(ast::Expr& parentRef, ast::CallExpr* call)
{
    // 1. Bottom-up: desugar the callee and all arguments first
    for (auto& arg : call->arguments) {
        desugarExpr(arg.argument);
    }

    // Check if call is a Sum Type variant constructor
    if (tryDesugarSumConstructor(parentRef)) {
        return;
    }

    // Not a constructor call — safe to desugar the callee normally now.
    desugarExpr(call->function);

    // 2. Check if the callee is a simple Identifier calling a builtin intrinsic
    auto* ident = std::get_if<ast::Identifier*>(&call->function);
    if (!ident || !(*ident)) {
        return;
    }

    std::string_view name = sym_.resolve((*ident)->name);

    if (name == "len" || name == "delete" || name == "yield_now") {
        auto* intrinsic = arena_.make<ast::IntrinsicCallExpr>();
        intrinsic->pos = call->pos;
        intrinsic->end = call->end;
        intrinsic->exprType = call->exprType; // Preserve the analyzed return type

        // Map high-level builtin names to interned intrinsic symbols
        if (name == "len") {
            intrinsic->intrinsicSym = sym_.intern("maml_len");
        } else if (name == "delete") {
            intrinsic->intrinsicSym = sym_.intern("maml_delete");
        } else if (name == "yield_now") {
            intrinsic->intrinsicSym = sym_.intern("maml_yield_now");
        }

        // Copy desugared arguments into the IntrinsicCallExpr vector
        intrinsic->arguments.reserve(call->arguments.size());
        for (const auto& arg : call->arguments) {
            intrinsic->arguments.push_back(arg.argument);
        }

        // 3. In-place replacement of the CallExpr with the IntrinsicCallExpr
        parentRef = intrinsic;
    }
}

const types::Type* Desugarer::getTaggedUnionLayout(const types::Type* sumType)
{
    return registry_.getTaggedUnionLayout(sumType, sym_);
}

const types::Type* Desugarer::getExprType(const ast::Expr& expr)
{
    return std::visit(overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
                          [](auto* node) -> const types::Type* {
                              if constexpr (requires { node->exprType; }) {
                                  return node->exprType;
                              }
                              return nullptr;
                          } },
        expr);
}

bool Desugarer::tryDesugarSumConstructor(ast::Expr& parentRef)
{
    // 1. Safely extract the expression type using our helper
    const types::Type* exprType = getExprType(parentRef);

    if (!exprType || exprType->kind != types::TypeKind::Sum) {
        return false;
    }

    const auto& sumPayload = std::get<types::SumPayload>(exprType->payload);

    // 2. Identify variant name and payload arguments
    SymID variantName = NoSymbol;
    std::vector<ast::Expr> payloadArgs;
    Position pos {};
    Position end {};

    if (auto* call = std::get_if<ast::CallExpr*>(&parentRef); call && *call) {
        pos = (*call)->pos;
        end = (*call)->end;
        if (auto* ident = std::get_if<ast::Identifier*>(&(*call)->function); ident && *ident) {
            variantName = (*ident)->name;
        }
        for (const auto& arg : (*call)->arguments) {
            payloadArgs.push_back(arg.argument);
        }
    } else if (auto* ident = std::get_if<ast::Identifier*>(&parentRef); ident && *ident) {
        // Zero-argument unit variant (e.g., `None`)
        pos = (*ident)->pos;
        end = (*ident)->end;
        variantName = (*ident)->name;
    } else {
        return false;
    }

    // 3. Find the matching variant in the Sum Type definition
    int variantIndex = -1;
    for (size_t i = 0; i < sumPayload.variants.size(); ++i) {
        if (sumPayload.variants[i].name == variantName) {
            variantIndex = static_cast<int>(i);
            break;
        }
    }

    if (variantIndex == -1) {
        return false;
    }

    const auto& variant = sumPayload.variants[variantIndex];

    // 4. Create a synthetic structural payload struct type for this variant
    //    so downstream MIR knows the exact field types of the payload.
    std::vector<types::StructField> payloadFields;
    for (size_t i = 0; i < variant.tupleTypes.size(); ++i) {
        payloadFields.push_back(
            { .name = sym_.intern(std::format("_{}", i)), .type = variant.tupleTypes[i] });
    }
    for (const auto& field : variant.fields) {
        payloadFields.push_back(field);
    }

    SymID payloadStructName = sym_.intern(
        std::format("{}_{}_Payload", exprType->toString(sym_), sym_.resolve(variant.name)));

    const types::Type* payloadStructType
        = registry_.getStruct(payloadStructName, std::move(payloadFields),
            /*isReprC=*/true);

    // 5. Construct TaggedUnionConstructExpr and replace parentRef in-place
    auto* tagConstruct = arena_.make<ast::TaggedUnionConstructExpr>();
    tagConstruct->pos = pos;
    tagConstruct->end = end;
    tagConstruct->exprType = getTaggedUnionLayout(exprType);
    tagConstruct->discriminant = variant.discriminant;
    tagConstruct->payloadArgs = std::move(payloadArgs);
    tagConstruct->payloadStructType = payloadStructType;

    parentRef = tagConstruct;
    return true;
}

// =============================================================================
// Phase 4B: MatchExpr Decision Tree Lowering
// =============================================================================

ast::Expr Desugarer::makeDiscriminantCheck(ast::Expr subjectRef, int discriminant, Position pos)
{
    const types::Type* i32Type = registry_.getPrimitive(types::TypeKind::I32);
    const types::Type* boolType = registry_.getPrimitive(types::TypeKind::Bool);

    // 1. subject.discriminant
    auto* fieldAccess = arena_.make<ast::FieldAccess>();
    fieldAccess->pos = pos;
    fieldAccess->end = pos;
    fieldAccess->object = subjectRef;
    fieldAccess->field = arena_.make<ast::Identifier>();
    fieldAccess->field->pos = pos;
    fieldAccess->field->end = pos;
    fieldAccess->field->name = sym_.intern("discriminant");
    fieldAccess->field->exprType = i32Type;
    fieldAccess->exprType = i32Type;

    // 2. int literal for discriminant
    auto* tagLit = arena_.make<ast::IntLiteral>();
    tagLit->pos = pos;
    tagLit->end = pos;
    tagLit->value = discriminant;
    tagLit->exprType = i32Type;

    // 3. subject.discriminant == tag
    auto* eqExpr = arena_.make<ast::InfixExpr>();
    eqExpr->pos = pos;
    eqExpr->end = pos;
    eqExpr->left = fieldAccess;
    eqExpr->op = TokenType::EQ;
    eqExpr->right = tagLit;
    eqExpr->exprType = boolType;

    return eqExpr;
}

void Desugarer::extractPatternBindings(ast::BlockStmt* block, ast::Pattern pattern,
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

    // Build the structural payload struct type for TaggedUnionAccessExpr
    std::vector<types::StructField> payloadFields;
    for (size_t i = 0; i < variant.tupleTypes.size(); ++i) {
        payloadFields.push_back(
            { .name = sym_.intern(std::format("_{}", i)), .type = variant.tupleTypes[i] });
    }
    for (const auto& field : variant.fields) {
        payloadFields.push_back(field);
    }

    const types::Type* payloadStructType = nullptr;
    if (!payloadFields.empty()) {
        SymID payloadStructName = sym_.intern(
            std::format("{}_{}_Payload", sumType->toString(sym_), sym_.resolve(variant.name)));
        payloadStructType = registry_.getStruct(payloadStructName, payloadFields,
            /*isReprC=*/true);
    }

    // Inspect pattern bindings and prepend explicit DeclareStmt extractions
    std::visit(overloaded { [](std::monostate) {}, [](ast::WildcardPattern*) {},
                   [](ast::LiteralPattern*) {},
                   [&](ast::IdentifierPattern* idPat) {
                       if (!idPat)
                           return;

                       // 1. Check if identifier is a unit variant name (e.g., `None` or `Red`).
                       //    If so, this is a unit variant pattern—there are NO payload variables to
                       //    extract.
                       for (const auto& v : sumPayload.variants) {
                           if (v.name == idPat->name) {
                               return;
                           }
                       }

                       // 2. Otherwise, it is a catch-all variable binding (e.g., `other => ...`).
                       //    Bind the entire subject to the identifier: let other = subjectRef;
                       auto* decl = arena_.make<ast::DeclareStmt>();
                       decl->pos = pos;
                       decl->end = pos;
                       decl->isMutable = false;
                       decl->name = idPat->name;
                       decl->value = subjectRef;

                       block->statements.insert(block->statements.begin(), decl);
                   },
                   [&](ast::CompositePattern* compPat) {
                       if (!compPat || !payloadStructType || payloadFields.empty())
                           return;

                       // Multiple bindings: e.g., Some(val) or Point(x, y)
                       for (size_t i = 0; i < compPat->elements.size(); ++i) {
                           const auto& elem = compPat->elements[i];
                           if (auto* idPat = std::get_if<ast::IdentifierPattern*>(&elem.pattern);
                               idPat && *idPat) {
                               // Safety check: ensure we don't index beyond available payload
                               // fields
                               if (i >= payloadFields.size())
                                   break;

                               auto* access = arena_.make<ast::TaggedUnionAccessExpr>();
                               access->pos = pos;
                               access->end = pos;
                               access->object = subjectRef;
                               access->fieldIndex = static_cast<int>(i);
                               access->payloadStructType = payloadStructType;
                               access->exprType = payloadFields[i].type;

                               auto* decl = arena_.make<ast::DeclareStmt>();
                               decl->pos = pos;
                               decl->end = pos;
                               decl->isMutable = false;
                               decl->name = (*idPat)->name;
                               decl->value = access;

                               block->statements.insert(block->statements.begin() + i, decl);
                           }
                       }
                   } },
        pattern);
}

void Desugarer::desugarMatchExpr(ast::Expr& parentRef, ast::MatchExpr* match)
{
    // 1. Extract the Sum Type BEFORE desugaring mutates match->subject!
    const types::Type* originalSumType = getExprType(match->subject);
    if (!originalSumType || originalSumType->kind != types::TypeKind::Sum) {
        return;
    }

    // Now it is safe to recursively desugar the subject and arm bodies
    desugarExpr(match->subject);
    for (auto& arm : match->arms) {
        desugarExpr(arm.body);
    }

    const types::Type* matchRetType = match->exprType;
    const auto& sumPayload = std::get<types::SumPayload>(originalSumType->payload);

    // 2. Hoist Subject into a Synthetic Enclosing Block: let __match_subj_N = <subject>;
    SymID subjSym = sym_.intern(std::format("__match_subj_{}", matchSubjCounter_++));
    auto* subjDecl = arena_.make<ast::DeclareStmt>();
    subjDecl->pos = match->pos;
    subjDecl->end = match->pos;
    subjDecl->isMutable = false;
    subjDecl->name = subjSym;
    subjDecl->value = match->subject;

    auto makeSubjectRef = [&]() -> ast::Expr {
        auto* ident = arena_.make<ast::Identifier>();
        ident->pos = match->pos;
        ident->end = match->pos;
        ident->name = subjSym;
        ident->exprType = getTaggedUnionLayout(originalSumType);
        return ident;
    };

    // 3. Chain Branches from Right-to-Left into nested IfExpr nodes
    ast::BlockStmt* currentElse = nullptr;

    for (size_t i = match->arms.size(); i > 0; --i) {
        auto& arm = match->arms[i - 1];

        ast::BlockStmt* consequenceBlock = nullptr;
        if (auto* block = std::get_if<ast::BlockStmt*>(&arm.body); block && *block) {
            consequenceBlock = *block;
        } else {
            consequenceBlock = makeExprBlock(arm.body, matchRetType, arm.pos);
        }

        // Check for Wildcard (_) Catch-All
        if (std::holds_alternative<ast::WildcardPattern*>(arm.pattern) && i == match->arms.size()) {
            currentElse = consequenceBlock;
            continue;
        }

        // CRITICAL FIX: Resolve variant symbol from both IdentifierPattern AND CompositePattern
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
                variantDiscriminant = sumPayload.variants[v].discriminant
                    ? sumPayload.variants[v].discriminant
                    : static_cast<int>(v);
                variantIndex = static_cast<int>(v);
                break;
            }
        }

        // Prepend pattern bindings into consequenceBlock
        extractPatternBindings(consequenceBlock, arm.pattern, makeSubjectRef(), originalSumType,
            variantIndex, arm.pos);

        // Build the IfExpr for this arm
        auto* ifExpr = arena_.make<ast::IfExpr>();
        ifExpr->pos = arm.pos;
        ifExpr->end = arm.end;
        ifExpr->condition = makeDiscriminantCheck(makeSubjectRef(), variantDiscriminant, arm.pos);
        ifExpr->consequence = consequenceBlock;
        ifExpr->alternative = currentElse;
        ifExpr->alternativeIsUnreachable = (currentElse == nullptr);
        ifExpr->exprType = matchRetType;

        currentElse = makeExprBlock(ifExpr, matchRetType, arm.pos);
    }

    // 4. Build Enclosing BlockStmt and replace MatchExpr in-place
    auto* enclosingBlock = arena_.make<ast::BlockStmt>();
    enclosingBlock->pos = match->pos;
    enclosingBlock->end = match->end;
    enclosingBlock->exprType = matchRetType;
    enclosingBlock->statements.push_back(subjDecl);

    if (currentElse && !currentElse->statements.empty()) {
        enclosingBlock->statements.push_back(currentElse->statements[0]);
    }

    parentRef = enclosingBlock;
}

} // namespace maml::hir