#include "analyzer.h"
#include "ast_nodes.h"
#include <format>
#include <variant>

namespace maml::sema {

template <typename... Args>
void Analyzer::addError(Position pos, std::format_string<Args...> fmt, Args&&... args)
{
    errors_.push_back(
        ast::CompileError { "Sema", pos, std::format(fmt, std::forward<Args>(args)...) });
}

Analyzer::Analyzer(types::TypeRegistry& registry, SymbolTable& sym)
    : registry_(registry)
    , sym_(sym)
{
    currentScope_ = createGlobalScope(registry_, sym_);
    scopes_.push_back(currentScope_);
}

Analyzer::~Analyzer()
{
    for (Scope* s : scopes_) {
        delete s;
    }
}

void Analyzer::pushScope()
{
    Scope* s = new Scope(currentScope_);
    scopes_.push_back(s);
    currentScope_ = s;
}

void Analyzer::popScope()
{
    if (currentScope_ && currentScope_->getParent()) {
        currentScope_ = currentScope_->getParent();
    }
}

Symbol* Analyzer::resolve(SymID name)
{
    if (currentScope_) {
        return currentScope_->resolveSymbol(name);
    }
    return nullptr;
}

const types::Type* Analyzer::lookupCustomType(SymID name)
{
    if (currentScope_) {
        return currentScope_->resolveType(name);
    }
    return nullptr;
}

Symbol* Analyzer::getRootSymbol(ast::Expr expr)
{
    if (auto** idPtr = std::get_if<ast::Identifier*>(&expr))
        return resolve((*idPtr)->name);
    if (auto** faPtr = std::get_if<ast::FieldAccess*>(&expr))
        return getRootSymbol((*faPtr)->object);
    if (auto** idxPtr = std::get_if<ast::IndexExpr*>(&expr))
        return getRootSymbol((*idxPtr)->left);
    return nullptr;
}

void Analyzer::analyze(ast::Program* program)
{
    // Pass 1: Hoist declarations into the global scope
    discoverTypes(program);
    resolveTypeBodies(program);
    registerFunctions(program);

    // Pass 2: Type-check bodies and decorate the AST (Phase 4)
    for (auto& decl : program->decls) {
        analyzeDecl(decl);
    }
}

// =============================================================================
// Pass 1: Type Hoisting
// =============================================================================

void Analyzer::discoverTypes(ast::Program* program)
{
    for (auto& declVar : program->decls) {
        if (auto** tdPtr = std::get_if<ast::TypeDecl*>(&declVar)) {
            ast::TypeDecl* td = *tdPtr;
            if (lookupCustomType(td->name->name) != nullptr) {
                addError(td->pos, "type '{}' already defined", sym_.resolve(td->name->name));
                continue;
            }

            if (std::holds_alternative<ast::StructTypeExpr*>(td->rhs)) {
                // Register an empty struct shell to allow recursive references
                currentScope_->defineType(td->name->name, registry_.getStruct(td->name->name, {}));
            } else if (std::holds_alternative<ast::SumTypeExpr*>(td->rhs)) {
                currentScope_->defineType(td->name->name, registry_.getSum(td->name->name, {}));
            }
        }
    }
}

void Analyzer::resolveTypeBodies(ast::Program* program)
{
    for (auto& declVar : program->decls) {
        if (auto** tdPtr = std::get_if<ast::TypeDecl*>(&declVar)) {
            resolveTypeBody(*tdPtr);
        }
    }
}

void Analyzer::resolveTypeBody(ast::TypeDecl* td)
{
    if (auto** structPtr = std::get_if<ast::StructTypeExpr*>(&td->rhs)) {
        ast::StructTypeExpr* rhs = *structPtr;
        std::vector<types::StructField> fields;
        for (const auto& f : rhs->fields) {
            fields.push_back({ f.name, resolveAstType(f.type) });
        }

        // Update the registry with the fully resolved struct
        const types::Type* resolved = registry_.getStruct(td->name->name, std::move(fields));
        currentScope_->defineType(td->name->name, resolved);

    } else if (auto** sumPtr = std::get_if<ast::SumTypeExpr*>(&td->rhs)) {
        ast::SumTypeExpr* rhs = *sumPtr;
        std::vector<types::SumVariant> variants;

        for (size_t i = 0; i < rhs->variants.size(); ++i) {
            const auto& v = rhs->variants[i];
            types::SumVariant variant;
            variant.name = v.name;
            variant.discriminant = static_cast<int>(i);

            for (const auto& f : v.fields) {
                variant.fields.push_back({ f.name, resolveAstType(f.type) });
            }
            for (const auto& tf : v.tupleFields) {
                variant.tupleTypes.push_back(resolveAstType(tf));
            }
            variants.push_back(std::move(variant));
        }

        const types::Type* resolved = registry_.getSum(td->name->name, variants);
        currentScope_->defineType(td->name->name, resolved);

        // Register variant constructors as symbols
        for (size_t i = 0; i < variants.size(); ++i) {
            Symbol sym;
            sym.kind = SymbolKind::Variant;
            sym.name = variants[i].name;
            sym.type = resolved;
            sym.sumType = resolved;
            sym.variantDiscriminant = static_cast<int>(i);
            currentScope_->defineSymbol(variants[i].name, sym);
        }
    }
}

// =============================================================================
// Pass 1.5: Function Hoisting
// =============================================================================

void Analyzer::registerFunctions(ast::Program* program)
{
    for (auto& declVar : program->decls) {
        if (auto** fnPtr = std::get_if<ast::FnDecl*>(&declVar)) {
            registerFunction(*fnPtr);
        }
    }
}

void Analyzer::registerFunction(ast::FnDecl* fn)
{
    std::vector<const types::Type*> paramTypes;
    std::vector<ast::Capability> caps;

    for (const auto& p : fn->params) {
        paramTypes.push_back(resolveAstType(p.type));

        // Inject default capability (CapRo) if none provided
        ast::Capability cap = p.cap;
        if (cap == ast::Capability::None) {
            cap = ast::Capability::Ro;
        }
        caps.push_back(cap);
    }

    const types::Type* returnType = registry_.getPrimitive(types::TypeKind::Unit);
    if (!std::holds_alternative<std::monostate>(fn->returnType)) {
        returnType = resolveAstType(fn->returnType);
    }

    if (fn->isAsync) {
        returnType = registry_.getFuture(returnType);
    }

    const types::Type* fnType
        = registry_.getFunction(std::move(paramTypes), std::move(caps), returnType);

    Symbol sym;
    sym.kind = SymbolKind::Func;
    sym.name = fn->name;
    sym.type = fnType;

    currentScope_->defineSymbol(fn->name, sym);
}

// =============================================================================
// AST -> Semantic Type Translation
// =============================================================================

const types::Type* Analyzer::resolveAstType(ast::TypeExpr expr)
{
    if (std::holds_alternative<std::monostate>(expr)) {
        return registry_.getPrimitive(types::TypeKind::Unknown);
    }

    if (auto** namedPtr = std::get_if<ast::NamedTypeExpr*>(&expr)) {
        ast::NamedTypeExpr* e = *namedPtr;
        std::string_view nameStr = sym_.resolve(e->name->name);

        if (nameStr == "int" || nameStr == "i64")
            return registry_.getPrimitive(types::TypeKind::I64);
        if (nameStr == "i8")
            return registry_.getPrimitive(types::TypeKind::I8);
        if (nameStr == "i16")
            return registry_.getPrimitive(types::TypeKind::I16);
        if (nameStr == "i32")
            return registry_.getPrimitive(types::TypeKind::I32);
        if (nameStr == "i128")
            return registry_.getPrimitive(types::TypeKind::I128);
        if (nameStr == "u8" || nameStr == "byte")
            return registry_.getPrimitive(types::TypeKind::U8);
        if (nameStr == "u16")
            return registry_.getPrimitive(types::TypeKind::U16);
        if (nameStr == "u32")
            return registry_.getPrimitive(types::TypeKind::U32);
        if (nameStr == "u64")
            return registry_.getPrimitive(types::TypeKind::U64);
        if (nameStr == "u128")
            return registry_.getPrimitive(types::TypeKind::U128);
        if (nameStr == "f32")
            return registry_.getPrimitive(types::TypeKind::F32);
        if (nameStr == "f64")
            return registry_.getPrimitive(types::TypeKind::F64);
        if (nameStr == "bool")
            return registry_.getPrimitive(types::TypeKind::Bool);
        if (nameStr == "char")
            return registry_.getPrimitive(types::TypeKind::Char);
        if (nameStr == "string")
            return registry_.getPrimitive(types::TypeKind::String);
        if (nameStr == "unit")
            return registry_.getPrimitive(types::TypeKind::Unit);
        if (nameStr == "any")
            return registry_.getPrimitive(types::TypeKind::Any);

        if (const types::Type* custom = lookupCustomType(e->name->name)) {
            return custom;
        }

        addError(e->pos, "unknown type '{}'", nameStr);
        return registry_.getPrimitive(types::TypeKind::Unknown);
    }

    if (auto** arrPtr = std::get_if<ast::ArrayTypeExpr*>(&expr)) {
        ast::ArrayTypeExpr* e = *arrPtr;
        return registry_.getArray(resolveAstType(e->base), e->size);
    }

    if (auto** genPtr = std::get_if<ast::GenericTypeExpr*>(&expr)) {
        return resolveGenericBuiltin(*genPtr);
    }

    return registry_.getPrimitive(types::TypeKind::Unknown);
}

const types::Type* Analyzer::resolveGenericBuiltin(ast::GenericTypeExpr* expr)
{
    std::string_view nameStr = sym_.resolve(expr->name->name);

    // Validate Arity
    auto validateArgs = [&](size_t expected) -> bool {
        if (expr->args.size() != expected) {
            addError(expr->pos, "generic type '{}' expects {} type argument(s), got {}", nameStr,
                expected, expr->args.size());
            return false;
        }
        return true;
    };

    if (nameStr == "Vec") {
        if (!validateArgs(1))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        return registry_.getVector(resolveAstType(expr->args[0]));
    }
    if (nameStr == "Map") {
        if (!validateArgs(2))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        return registry_.getMap(resolveAstType(expr->args[0]), resolveAstType(expr->args[1]));
    }
    if (nameStr == "Option") {
        if (!validateArgs(1))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        return registry_.getOption(resolveAstType(expr->args[0]), sym_);
    }
    if (nameStr == "Result") {
        if (!validateArgs(2))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        return registry_.getResult(
            resolveAstType(expr->args[0]), resolveAstType(expr->args[1]), sym_);
    }
    if (nameStr == "Future") {
        if (!validateArgs(1))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        return registry_.getFuture(resolveAstType(expr->args[0]));
    }
    if (nameStr == "View") {
        if (!validateArgs(1))
            return registry_.getPrimitive(types::TypeKind::Unknown);
        // Defaulting to immutable view. You can expand parsing later if you want `View<T, mut>`
        return registry_.getView(resolveAstType(expr->args[0]), false);
    }

    addError(expr->pos, "generic type '{}' is not supported", nameStr);
    return registry_.getPrimitive(types::TypeKind::Unknown);
}

// C++17/20 boilerplate for std::visit lambda overloading
template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

static const char* capabilityToString(ast::Capability cap)
{
    switch (cap) {
    case ast::Capability::Own:
        return "own";
    case ast::Capability::Mut:
        return "mut";
    case ast::Capability::Ro:
        return "ro";
    default:
        return "";
    }
}

static bool isValidMemoryPath(ast::Expr expr)
{
    return std::visit(overloaded { [](ast::Identifier*) { return true; },
                          [](ast::FieldAccess* fa) { return isValidMemoryPath(fa->object); },
                          [](ast::IndexExpr* idx) { return isValidMemoryPath(idx->left); },
                          [](auto) { return false; } },
        expr);
}

static bool containsView(const types::Type* t)
{
    if (!t)
        return false;
    switch (t->kind) {
    case types::TypeKind::View:
        return true;
    case types::TypeKind::Struct: {
        const auto& p = std::get<types::StructPayload>(t->payload);
        for (const auto& f : p.fields) {
            if (containsView(f.type))
                return true;
        }
        return false;
    }
    case types::TypeKind::Array:
        return containsView(std::get<types::ArrayPayload>(t->payload).base);
    case types::TypeKind::Vector:
        return containsView(std::get<types::VectorPayload>(t->payload).base);
    case types::TypeKind::Map: {
        const auto& p = std::get<types::MapPayload>(t->payload);
        return containsView(p.key) || containsView(p.value);
    }
    default:
        return false;
    }
}

// Helper: safely extract exprType from an Expr variant (handles monostate)
static const types::Type* exprTypeOf(ast::Expr expr)
{
    return std::visit(
        overloaded { [](std::monostate) -> const types::Type* { return nullptr; },
            [](auto&& e) -> const types::Type* { return e ? e->exprType : nullptr; } },
        expr);
}

static Position posOf(ast::Expr expr)
{
    return std::visit(overloaded { [](std::monostate) -> Position { return Position {}; },
                          [](auto&& e) -> Position { return e ? e->pos : Position {}; } },
        expr);
}

// =============================================================================
// Helper: Merge Types
// =============================================================================
const types::Type* mergeTypes(
    const types::Type* t1, const types::Type* t2, types::TypeRegistry& reg)
{
    if (!t1 || !t2)
        return nullptr;
    if (t1 == t2)
        return t1;
    if (t1->kind != types::TypeKind::Unknown && t2->kind != types::TypeKind::Unknown) {
        return reg.getPrimitive(types::TypeKind::Unknown); // Conflict
    }
    return t1->kind == types::TypeKind::Unknown ? t2 : t1;
}

// =============================================================================
// Pass 2: Declaration Analysis
// =============================================================================

void Analyzer::analyzeDecl(ast::Decl decl)
{
    std::visit(
        overloaded { [&](std::monostate) {},
            [&](ast::Program* prog) {
                for (auto& d : prog->decls)
                    analyzeDecl(d);
            },
            [&](ast::TypeDecl* td) {
                // Types were already hoisted in Pass 1.
            },
            [&](ast::FnDecl* fn) {
                Symbol* fnSym = resolve(fn->name);
                if (!fnSym)
                    return;

                // Enforce MainCannotBeAsync rule
                if (sym_.resolve(fn->name) == "main" && fn->isAsync) {
                    addError(fn->pos,
                        "the 'main' function cannot be async; you must manually spawn tasks");
                }

                const auto& fnPayload = std::get<types::FunctionPayload>(fnSym->type->payload);
                expectedReturn_ = fnPayload.returnType;
                isAsync_ = fn->isAsync;

                pushScope();

                for (size_t i = 0; i < fn->params.size(); ++i) {
                    Symbol paramSym;
                    paramSym.kind = SymbolKind::Param;
                    paramSym.name = fn->params[i].name;
                    paramSym.type = fnPayload.params[i];
                    paramSym.cap = fnPayload.caps[i];
                    currentScope_->defineSymbol(fn->params[i].name, paramSym);
                }

                if (fn->body) {
                    analyzeStmt(fn->body);
                }

                popScope();
                expectedReturn_ = nullptr;
                isAsync_ = false;
            } },
        decl);
}

// =============================================================================
// Pass 2: Statement Analysis
// =============================================================================

void Analyzer::analyzeStmt(ast::Stmt stmt)
{
    std::visit(
        overloaded { [&](std::monostate) {},
            [&](ast::BlockStmt* block) {
                pushScope();
                for (auto& s : block->statements) {
                    analyzeStmt(s);
                }

                // A block's type is the type of its last yielded expression,
                // or Unit if it doesn't yield. For simplicity in this pass,
                // we default to Unit unless a YieldStmt modifies it.
                block->exprType = registry_.getPrimitive(types::TypeKind::Unit);
                if (!block->statements.empty()) {
                    if (auto** yieldPtr = std::get_if<ast::YieldStmt*>(&block->statements.back())) {
                        block->exprType = exprTypeOf((*yieldPtr)->value);
                    }
                }
                popScope();
            },
            [&](ast::DeclareStmt* decl) {
                analyzeExpr(decl->value);
                const types::Type* valType = exprTypeOf(decl->value);

                // NoUnitAssignment rule
                if (valType && valType->kind == types::TypeKind::Unit) {
                    addError(
                        decl->pos, "cannot assign the result of a function that returns 'unit'");
                }

                // NoDuplicateDeclaration rule
                if (currentScope_->resolveSymbol(decl->name) != nullptr) {
                    addError(
                        decl->pos, "variable '{}' is already declared", sym_.resolve(decl->name));
                }

                Symbol sym;
                sym.kind = SymbolKind::Var;
                sym.name = decl->name;
                sym.type = valType ? valType : registry_.getPrimitive(types::TypeKind::Unknown);
                sym.isMutable = decl->isMutable;
                currentScope_->defineSymbol(decl->name, sym);
            },
            [&](ast::AliasDecl* alias) {
                analyzeExpr(alias->value);
                const types::Type* valType = exprTypeOf(alias->value);

                Symbol sym;
                sym.kind = SymbolKind::Var;
                sym.name = alias->name;
                sym.type = valType ? valType : registry_.getPrimitive(types::TypeKind::Unknown);
                sym.isMutable = false;
                sym.cap = alias->cap;
                currentScope_->defineSymbol(alias->name, sym);

                // AliasMutabilityValid
                if (alias->cap == ast::Capability::Mut) {
                    Symbol* rootSym = getRootSymbol(alias->value);
                    if (rootSym && !rootSym->isMutable && rootSym->cap != ast::Capability::Mut) {
                        addError(alias->pos, "cannot take a 'mut' alias of immutable variable '{}'",
                            sym_.resolve(rootSym->name));
                    }
                }

                // AliasPathValid
                if (alias->cap == ast::Capability::Mut || alias->cap == ast::Capability::Own
                    || alias->cap == ast::Capability::Ro) {
                    if (!isValidMemoryPath(alias->value)) {
                        addError(alias->pos,
                            "the '{}' capability can only be applied to variables and their fields",
                            capabilityToString(alias->cap));
                    }
                }
            },
            [&](ast::AssignStmt* assign) {
                analyzeExpr(assign->lValue);
                analyzeExpr(assign->rValue);

                const types::Type* lType = exprTypeOf(assign->lValue);
                const types::Type* rType = exprTypeOf(assign->rValue);

                if (lType && rType && lType != rType && lType->kind != types::TypeKind::Unknown
                    && rType->kind != types::TypeKind::Unknown) {
                    addError(assign->pos, "type mismatch: cannot assign '{}' to '{}'",
                        rType->toString(sym_), lType->toString(sym_));
                }

                // AssignLValueMustBeSymbol & Mutability Check
                if (auto** idPtr = std::get_if<ast::Identifier*>(&assign->lValue)) {
                    Symbol* rootSym = getRootSymbol(assign->lValue);
                    if (!rootSym) {
                        addError(assign->pos, "cannot assign to non-variable expression");
                    } else if (!rootSym->isMutable && rootSym->cap != ast::Capability::Mut) {
                        addError(assign->pos, "cannot mutate immutable variable '{}'",
                            sym_.resolve(rootSym->name));
                    }
                }

                // CannotReassignBorrow
                if (auto** idPtr = std::get_if<ast::Identifier*>(&assign->lValue)) {
                    Symbol* sym = resolve((*idPtr)->name);
                    if (sym && sym->cap == ast::Capability::Ro) {
                        addError(assign->pos, "cannot reassign read-only borrow '{}'",
                            sym_.resolve(sym->name));
                    }
                }
            },
            [&](ast::ReturnStmt* ret) {
                const types::Type* retType = registry_.getPrimitive(types::TypeKind::Unit);
                if (!std::holds_alternative<std::monostate>(ret->value)) {
                    analyzeExpr(ret->value);
                    retType = exprTypeOf(ret->value);
                }

                const types::Type* expected = expectedReturn_
                    ? expectedReturn_
                    : registry_.getPrimitive(types::TypeKind::Unit);

                // AwaitRequiresFuture / Async Unwrapping
                if (isAsync_) {
                    if (expected->kind == types::TypeKind::Future) {
                        expected = std::get<types::FuturePayload>(expected->payload).base;
                    } else {
                        addError(ret->pos,
                            "error: async function return type should be wrapped in Future");
                    }
                }

                // CannotReturnView
                if (retType && containsView(retType)) {
                    addError(ret->pos, "cannot return a View from a function");
                }

                if (retType && expected && retType != expected
                    && retType->kind != types::TypeKind::Unknown
                    && expected->kind != types::TypeKind::Unknown) {
                    addError(ret->pos, "type mismatch: expected return type '{}', got '{}'",
                        expected->toString(sym_), retType->toString(sym_));
                }
            },
            [&](ast::ForStmt* forStmt) {
                pushScope();
                analyzeStmt(forStmt->init);
                analyzeExpr(forStmt->condition);
                analyzeStmt(forStmt->post);

                const types::Type* condType = exprTypeOf(forStmt->condition);
                if (condType && condType->kind != types::TypeKind::Bool
                    && condType->kind != types::TypeKind::Unknown) {
                    addError(forStmt->pos, "for-loop condition must be of type 'bool', got '{}'",
                        condType->toString(sym_));
                }

                if (forStmt->body)
                    analyzeStmt(forStmt->body);
                popScope();
            },
            [&](ast::ExprStmt* exprStmt) { analyzeExpr(exprStmt->value); },
            [&](ast::YieldStmt* yield) { analyzeExpr(yield->value); },
            [&](auto) { /* Fallback for Break, Continue etc. */ } },
        stmt);
}

// =============================================================================
// Pass 2: Expression Analysis
// =============================================================================

void Analyzer::analyzeExpr(ast::Expr expr)
{
    std::visit(
        overloaded {
            [&](std::monostate) {},
            [&](ast::Identifier* id) {
                Symbol* sym = resolve(id->name);
                if (!sym) {
                    addError(id->pos, "undefined name '{}'", sym_.resolve(id->name));
                    id->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }
                id->exprType = sym->type;
            },
            [&](ast::IntLiteral* lit) {
                lit->exprType
                    = registry_.getPrimitive(types::TypeKind::I64); // Default integer type
            },
            [&](ast::BoolLiteral* lit) {
                lit->exprType = registry_.getPrimitive(types::TypeKind::Bool);
            },
            [&](ast::StringLiteral* lit) {
                lit->exprType = registry_.getPrimitive(types::TypeKind::String);
            },
            [&](ast::InfixExpr* infix) {
                analyzeExpr(infix->left);
                analyzeExpr(infix->right);

                const types::Type* lType = exprTypeOf(infix->left);
                const types::Type* rType = exprTypeOf(infix->right);

                // --- Integer Literal Auto-Coercion ---
                if (auto* leftLit = std::get_if<ast::IntLiteral*>(&infix->left)) {
                    if (rType && rType->isInteger() && rType->canRepresentInt((*leftLit)->value)) {
                        (*leftLit)->exprType = rType;
                        lType = rType;
                    }
                }
                if (auto* rightLit = std::get_if<ast::IntLiteral*>(&infix->right)) {
                    if (lType && lType->isInteger() && lType->canRepresentInt((*rightLit)->value)) {
                        (*rightLit)->exprType = lType;
                        rType = lType;
                    }
                }

                if (!lType || !rType || lType->kind == types::TypeKind::Unknown
                    || rType->kind == types::TypeKind::Unknown) {
                    infix->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                // InfixTypeCompatibility
                if (lType != rType) {
                    addError(infix->pos, "type mismatch: cannot apply operator to '{}' and '{}'",
                        lType->toString(sym_), rType->toString(sym_));
                    infix->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                switch (infix->op) {
                case TokenType::PLUS:
                case TokenType::MINUS:
                case TokenType::MULTIPLY:
                case TokenType::DIVIDE:
                case TokenType::MODULO:
                    if (!lType->isInteger()) {
                        addError(infix->pos, "operator requires integer operands, got '{}'",
                            lType->toString(sym_));
                    }
                    infix->exprType = lType;
                    break;
                case TokenType::EQ:
                case TokenType::NOT_EQ:
                case TokenType::LT:
                case TokenType::LTE:
                case TokenType::GT:
                case TokenType::GTE:
                    infix->exprType = registry_.getPrimitive(types::TypeKind::Bool);
                    break;
                case TokenType::AND:
                case TokenType::OR:
                    if (lType->kind != types::TypeKind::Bool) {
                        addError(infix->pos, "operator requires 'bool' operands");
                    }
                    infix->exprType = registry_.getPrimitive(types::TypeKind::Bool);
                    break;
                default:
                    infix->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                }
            },
            [&](ast::CallExpr* call) {
                analyzeExpr(call->function);
                for (auto& arg : call->arguments) {
                    analyzeExpr(arg.argument);
                }

                // --- Variant Constructor Intercept ---
                if (auto** funcIdPtr = std::get_if<ast::Identifier*>(&call->function)) {
                    Symbol* sym = resolve((*funcIdPtr)->name);
                    if (sym && sym->kind == SymbolKind::Variant) {
                        const types::Type* sumType = sym->sumType;
                        const auto& payload = std::get<types::SumPayload>(sumType->payload);
                        const types::SumVariant* targetVariant = nullptr;

                        for (const auto& v : payload.variants) {
                            if (v.name == sym->name) {
                                targetVariant = &v;
                                break;
                            }
                        }

                        if (!targetVariant) {
                            call->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                            return;
                        }

                        if (call->arguments.size() != targetVariant->tupleTypes.size()) {
                            addError(call->pos, "variant '{}' expects {} argument(s), got {}",
                                sym_.resolve(sym->name), targetVariant->tupleTypes.size(),
                                call->arguments.size());
                            call->exprType = sumType;
                            return;
                        }

                        for (size_t i = 0; i < call->arguments.size(); ++i) {
                            const types::Type* argType = exprTypeOf(call->arguments[i].argument);

                            const types::Type* expectedType = targetVariant->tupleTypes[i];
                            if (argType && argType != expectedType
                                && argType->kind != types::TypeKind::Unknown
                                && expectedType->kind != types::TypeKind::Unknown) {
                                addError(call->arguments[i].pos,
                                    "type mismatch for variant argument {}: expected '{}', got "
                                    "'{}'",
                                    i + 1, expectedType->toString(sym_), argType->toString(sym_));
                            }
                        }

                        call->exprType = sumType;
                        return;
                    }
                }

                const types::Type* fnType = exprTypeOf(call->function);

                if (!fnType || fnType->kind != types::TypeKind::Function) {
                    if (fnType && fnType->kind != types::TypeKind::Unknown) {
                        addError(call->pos, "cannot call non-function type '{}'",
                            fnType->toString(sym_));
                    }
                    call->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                const auto& fnPayload = std::get<types::FunctionPayload>(fnType->payload);
                call->exprType = fnPayload.returnType;

                // handleRunExecutor
                if (auto** funcIdPtr = std::get_if<ast::Identifier*>(&call->function)) {
                    if (sym_.resolve((*funcIdPtr)->name) == "run_executor") {
                        if (call->arguments.size() != 1) {
                            addError(call->pos, "'run_executor' expects exactly 1 argument");
                        } else {
                            const types::Type* argType = exprTypeOf(call->arguments[0].argument);
                            if (argType && argType->kind == types::TypeKind::Future) {
                                call->exprType
                                    = std::get<types::FuturePayload>(argType->payload).base;
                            } else if (argType && argType->kind != types::TypeKind::Unknown) {
                                addError(call->pos, "'run_executor' expects a Future, got '{}'",
                                    argType->toString(sym_));
                            }
                        }
                    }
                }

                // No Bare Call Trap
                bool isAsyncCall = (fnPayload.returnType->kind == types::TypeKind::Future);
                if (isAsyncCall && !allowAsyncCall_) {
                    std::string fnName = "async function";
                    if (auto** idPtr = std::get_if<ast::Identifier*>(&call->function)) {
                        fnName = "'" + std::string(sym_.resolve((*idPtr)->name)) + "'";
                    }
                    addError(call->pos, "{} must be called with 'await' or 'spawn'", fnName);
                }

                // CallArgumentCount
                if (call->arguments.size() != fnPayload.params.size()) {
                    addError(call->pos, "wrong number of arguments: expected {}, got {}",
                        fnPayload.params.size(), call->arguments.size());
                    return;
                }

                // CallArgumentTypeCompatibility
                for (size_t i = 0; i < call->arguments.size(); ++i) {
                    const types::Type* argType = exprTypeOf(call->arguments[i].argument);
                    ast::Capability actualCap = call->arguments[i].cap;
                    ast::Capability expectedCap = fnPayload.caps[i];
                    const types::Type* expectedType = fnPayload.params[i];

                    // --- Literal Auto-Coercion ---
                    bool isLiteral
                        = std::holds_alternative<ast::StringLiteral*>(call->arguments[i].argument);
                    if (isLiteral && actualCap == ast::Capability::None
                        && (expectedCap == ast::Capability::Ro
                            || expectedCap == ast::Capability::Own)) {
                        actualCap = expectedCap;
                        call->arguments[i].cap = expectedCap;
                    }

                    // --- Integer Literal Auto-Coercion ---
                    if (auto* argLit
                        = std::get_if<ast::IntLiteral*>(&call->arguments[i].argument)) {
                        if (expectedType->isInteger()
                            && expectedType->canRepresentInt((*argLit)->value)) {
                            (*argLit)->exprType = expectedType;
                            argType = expectedType;
                        }
                    }

                    // 1. Type Check
                    if (argType && argType != expectedType
                        && argType->kind != types::TypeKind::Unknown
                        && expectedType->kind != types::TypeKind::Unknown) {
                        addError(call->arguments[i].pos,
                            "argument {} type mismatch: expected '{}', got '{}'", i + 1,
                            expectedType->toString(sym_), argType->toString(sym_));
                    }

                    // 2. Capability matching check
                    if (expectedCap != actualCap) {
                        addError(call->arguments[i].pos,
                            "capability mismatch on argument {}: function expects '{}', but call "
                            "site passed '{}'",
                            i + 1, capabilityToString(expectedCap), capabilityToString(actualCap));
                    }

                    // 3. Memory Path Check
                    if (actualCap == ast::Capability::Mut) {
                        if (!isValidMemoryPath(call->arguments[i].argument)) {
                            addError(call->arguments[i].pos,
                                "the 'mut' capability can only be applied to variables and their "
                                "fields");
                        }
                    }
                }
            },
            [&](ast::IfExpr* ifExpr) {
                analyzeExpr(ifExpr->condition);
                if (ifExpr->consequence)
                    analyzeStmt(ifExpr->consequence);
                if (ifExpr->alternative)
                    analyzeStmt(ifExpr->alternative);

                const types::Type* condType = exprTypeOf(ifExpr->condition);
                if (condType && condType->kind != types::TypeKind::Bool
                    && condType->kind != types::TypeKind::Unknown) {
                    addError(ifExpr->pos, "if condition must be of type 'bool'");
                }

                const types::Type* consType = ifExpr->consequence
                    ? ifExpr->consequence->exprType
                    : registry_.getPrimitive(types::TypeKind::Unit);
                const types::Type* altType = ifExpr->alternative
                    ? ifExpr->alternative->exprType
                    : registry_.getPrimitive(types::TypeKind::Unit);

                ifExpr->exprType = mergeTypes(consType, altType, registry_);
                if (ifExpr->exprType->kind == types::TypeKind::Unknown
                    && consType->kind != types::TypeKind::Unknown
                    && altType->kind != types::TypeKind::Unknown) {
                    addError(ifExpr->pos, "if/else branches have incompatible types");
                }
            },
            [&](ast::AwaitExpr* await) {
                allowAsyncCall_ = true;
                analyzeExpr(await->value);
                allowAsyncCall_ = false;

                const types::Type* valType = exprTypeOf(await->value);

                if (!isAsync_) {
                    addError(await->pos, "cannot use 'await' outside of an async function");
                }

                if (valType && valType->kind == types::TypeKind::Future) {
                    await->exprType = std::get<types::FuturePayload>(valType->payload).base;
                } else {
                    if (valType && valType->kind != types::TypeKind::Unknown) {
                        addError(await->pos, "cannot await non-Future type '{}'",
                            valType->toString(sym_));
                    }
                    await->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                }
            },
            [&](ast::PrefixExpr* prefix) {
                analyzeExpr(prefix->right);
                const types::Type* rType = exprTypeOf(prefix->right);

                if (!rType || rType->kind == types::TypeKind::Unknown) {
                    prefix->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                if (prefix->op == TokenType::NOT) {
                    if (rType->kind != types::TypeKind::Bool) {
                        addError(prefix->pos, "operator '!' expects 'bool', got '{}'",
                            rType->toString(sym_));
                    }
                    prefix->exprType = registry_.getPrimitive(types::TypeKind::Bool);
                } else if (prefix->op == TokenType::MINUS) {
                    if (!rType->isInteger()) {
                        addError(prefix->pos, "operator '-' expects integer, got '{}'",
                            rType->toString(sym_));
                    }
                    prefix->exprType = rType;
                }
            },
            [&](ast::SpawnExpr* spawn) {
                allowAsyncCall_ = true;
                if (spawn->value)
                    analyzeExpr(spawn->value);
                allowAsyncCall_ = false;

                const types::Type* valType = spawn->value ? spawn->value->exprType : nullptr;
                if (valType && valType->kind == types::TypeKind::Future) {
                    spawn->exprType = valType;
                } else {
                    if (valType && valType->kind != types::TypeKind::Unknown) {
                        addError(spawn->pos,
                            "spawn can only be applied to async functions, got '{}'",
                            valType->toString(sym_));
                    }
                    spawn->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                }
            },
            [&](ast::FieldAccess* fa) {
                analyzeExpr(fa->object);
                const types::Type* objType = exprTypeOf(fa->object);

                if (!objType || objType->kind == types::TypeKind::Unknown) {
                    fa->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                if (objType->kind != types::TypeKind::Struct) {
                    addError(fa->pos, "cannot access field '{}' on non-struct type '{}'",
                        sym_.resolve(fa->field->name), objType->toString(sym_));
                    fa->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                const auto& payload = std::get<types::StructPayload>(objType->payload);
                for (const auto& field : payload.fields) {
                    if (field.name == fa->field->name) {
                        fa->exprType = field.type;
                        fa->field->exprType = field.type; // Decorate the identifier
                        return;
                    }
                }

                addError(fa->field->pos, "field '{}' does not exist on struct '{}'",
                    sym_.resolve(fa->field->name), sym_.resolve(payload.name));
                fa->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
            },
            [&](ast::IndexExpr* idx) {
                analyzeExpr(idx->left);
                analyzeExpr(idx->index);

                const types::Type* leftType = exprTypeOf(idx->left);
                const types::Type* idxType = exprTypeOf(idx->index);

                if (!leftType || leftType->kind == types::TypeKind::Unknown) {
                    idx->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                    return;
                }

                if (leftType->kind == types::TypeKind::Array
                    || leftType->kind == types::TypeKind::Vector
                    || leftType->kind == types::TypeKind::View
                    || leftType->kind == types::TypeKind::String) {

                    if (idxType && idxType->kind != types::TypeKind::I64
                        && idxType->kind != types::TypeKind::Unknown) {
                        addError(posOf(idx->index), "index must be an integer, got '{}'",
                            idxType->toString(sym_));
                    }

                    if (leftType->kind == types::TypeKind::Array)
                        idx->exprType = std::get<types::ArrayPayload>(leftType->payload).base;
                    else if (leftType->kind == types::TypeKind::Vector)
                        idx->exprType = std::get<types::VectorPayload>(leftType->payload).base;
                    else if (leftType->kind == types::TypeKind::View)
                        idx->exprType = std::get<types::ViewPayload>(leftType->payload).base;
                    else
                        idx->exprType = registry_.getPrimitive(
                            types::TypeKind::I64); // String characters as ints
                } else if (leftType->kind == types::TypeKind::Map) {
                    const auto& mapPayload = std::get<types::MapPayload>(leftType->payload);
                    if (idxType && idxType != mapPayload.key
                        && idxType->kind != types::TypeKind::Unknown) {
                        addError(posOf(idx->index), "map index must be of type '{}', got '{}'",
                            mapPayload.key->toString(sym_), idxType->toString(sym_));
                    }
                    idx->exprType = registry_.getOption(mapPayload.value, sym_);
                } else {
                    addError(idx->pos, "cannot index into non-collection type '{}'",
                        leftType->toString(sym_));
                    idx->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
                }
            },
            [&](ast::CompositeLiteral* comp) {
                const types::Type* resolvedType = resolveAstType(comp->typeExpr);
                comp->exprType = resolvedType;

                if (!resolvedType || resolvedType->kind == types::TypeKind::Unknown)
                    return;

                if (resolvedType->kind == types::TypeKind::Array) {
                    const auto& payload = std::get<types::ArrayPayload>(resolvedType->payload);
                    if (comp->elements.size() > static_cast<size_t>(payload.size)) {
                        addError(comp->pos,
                            "too many elements in array literal: declared size is {}, got {}",
                            payload.size, comp->elements.size());
                    }
                    for (auto& el : comp->elements) {
                        if (!std::holds_alternative<std::monostate>(el.key)) {
                            addError(el.pos, "array literals cannot have keyed elements");
                        }
                        analyzeExpr(el.value);
                        const types::Type* elType = exprTypeOf(el.value);
                        if (elType && elType != payload.base
                            && elType->kind != types::TypeKind::Unknown
                            && payload.base->kind != types::TypeKind::Unknown) {
                            addError(el.pos, "array element type mismatch: expected '{}', got '{}'",
                                payload.base->toString(sym_), elType->toString(sym_));
                        }
                    }
                } else if (resolvedType->kind == types::TypeKind::Vector) {
                    const auto& payload = std::get<types::VectorPayload>(resolvedType->payload);
                    for (auto& el : comp->elements) {
                        if (!std::holds_alternative<std::monostate>(el.key)) {
                            addError(el.pos,
                                "vector literals should only contain values, not key-value "
                                "pairs");
                        }
                        analyzeExpr(el.value);
                        const types::Type* elType = exprTypeOf(el.value);
                        if (elType && elType != payload.base
                            && elType->kind != types::TypeKind::Unknown
                            && payload.base->kind != types::TypeKind::Unknown) {
                            addError(el.pos,
                                "vector element type mismatch: expected '{}', got '{}'",
                                payload.base->toString(sym_), elType->toString(sym_));
                        }
                    }
                } else if (resolvedType->kind == types::TypeKind::Map) {
                    const auto& payload = std::get<types::MapPayload>(resolvedType->payload);
                    for (auto& el : comp->elements) {
                        if (std::holds_alternative<std::monostate>(el.key)) {
                            addError(el.pos, "map literals require explicit key-value pairs");
                            continue;
                        }
                        analyzeExpr(el.key);
                        analyzeExpr(el.value);
                        const types::Type* kType = exprTypeOf(el.key);
                        const types::Type* vType = exprTypeOf(el.value);

                        if (kType && kType != payload.key && kType->kind != types::TypeKind::Unknown
                            && payload.key->kind != types::TypeKind::Unknown) {
                            addError(el.pos, "map key type mismatch: expected '{}', got '{}'",
                                payload.key->toString(sym_), kType->toString(sym_));
                        }
                        if (vType && vType != payload.value
                            && vType->kind != types::TypeKind::Unknown
                            && payload.value->kind != types::TypeKind::Unknown) {
                            addError(el.pos, "map value type mismatch: expected '{}', got '{}'",
                                payload.value->toString(sym_), vType->toString(sym_));
                        }
                    }
                } else if (resolvedType->kind == types::TypeKind::Struct) {
                    const auto& payload = std::get<types::StructPayload>(resolvedType->payload);
                    std::vector<bool> seen(payload.fields.size(), false);

                    for (auto& el : comp->elements) {
                        auto** keyIdentPtr = std::get_if<ast::Identifier*>(&el.key);
                        if (!keyIdentPtr) {
                            addError(el.pos, "struct fields must be keyed with identifiers");
                            analyzeExpr(el.value);
                            continue;
                        }
                        SymID fieldName = (*keyIdentPtr)->name;
                        analyzeExpr(el.value);
                        const types::Type* valType = exprTypeOf(el.value);

                        int foundIdx = -1;
                        for (size_t i = 0; i < payload.fields.size(); ++i) {
                            if (payload.fields[i].name == fieldName) {
                                foundIdx = static_cast<int>(i);
                                break;
                            }
                        }

                        if (foundIdx == -1) {
                            addError(el.pos, "field '{}' does not exist on struct '{}'",
                                sym_.resolve(fieldName), sym_.resolve(payload.name));
                        } else {
                            if (seen[foundIdx]) {
                                addError(el.pos, "duplicate field '{}' in struct literal",
                                    sym_.resolve(fieldName));
                            }
                            seen[foundIdx] = true;

                            const types::Type* expectedType = payload.fields[foundIdx].type;
                            if (valType && valType != expectedType
                                && valType->kind != types::TypeKind::Unknown
                                && expectedType->kind != types::TypeKind::Unknown) {
                                addError(el.pos,
                                    "type mismatch for field '{}': expected '{}', got '{}'",
                                    sym_.resolve(fieldName), expectedType->toString(sym_),
                                    valType->toString(sym_));
                            }
                        }
                    }

                    for (size_t i = 0; i < payload.fields.size(); ++i) {
                        if (!seen[i])
                            addError(comp->pos, "missing field '{}' in struct literal",
                                sym_.resolve(payload.fields[i].name));
                    }
                } else if (resolvedType->kind == types::TypeKind::Sum) {
                    SymID variantName = NoSymbol;
                    if (auto** namedType = std::get_if<ast::NamedTypeExpr*>(&comp->typeExpr)) {
                        variantName = (*namedType)->name->name;
                    }

                    const auto& payload = std::get<types::SumPayload>(resolvedType->payload);
                    const types::SumVariant* targetVariant = nullptr;
                    for (const auto& v : payload.variants) {
                        if (v.name == variantName) {
                            targetVariant = &v;
                            break;
                        }
                    }

                    if (!targetVariant) {
                        addError(comp->pos, "variant '{}' does not exist on type '{}'",
                            sym_.resolve(variantName), sym_.resolve(payload.baseName));
                        return;
                    }

                    std::vector<bool> seen(targetVariant->fields.size(), false);
                    for (auto& el : comp->elements) {
                        auto** keyIdentPtr = std::get_if<ast::Identifier*>(&el.key);
                        if (!keyIdentPtr) {
                            addError(el.pos,
                                "variant fields must be keyed with identifiers (e.g., field: "
                                "value)");
                            analyzeExpr(el.value);
                            continue;
                        }
                        SymID fieldName = (*keyIdentPtr)->name;
                        analyzeExpr(el.value);
                        const types::Type* valType = exprTypeOf(el.value);

                        int foundIdx = -1;
                        for (size_t i = 0; i < targetVariant->fields.size(); ++i) {
                            if (targetVariant->fields[i].name == fieldName) {
                                foundIdx = static_cast<int>(i);
                                break;
                            }
                        }

                        if (foundIdx == -1) {
                            addError(el.pos, "field '{}' does not exist on variant '{}'",
                                sym_.resolve(fieldName), sym_.resolve(targetVariant->name));
                        } else {
                            if (seen[foundIdx]) {
                                addError(el.pos, "duplicate field '{}' in variant literal",
                                    sym_.resolve(fieldName));
                            }
                            seen[foundIdx] = true;

                            const types::Type* expectedType = targetVariant->fields[foundIdx].type;
                            if (valType && valType != expectedType
                                && valType->kind != types::TypeKind::Unknown
                                && expectedType->kind != types::TypeKind::Unknown) {
                                addError(el.pos,
                                    "type mismatch for field '{}': expected '{}', got '{}'",
                                    sym_.resolve(fieldName), expectedType->toString(sym_),
                                    valType->toString(sym_));
                            }
                        }
                    }

                    for (size_t i = 0; i < targetVariant->fields.size(); ++i) {
                        if (!seen[i])
                            addError(comp->pos, "missing field '{}' in variant literal",
                                sym_.resolve(targetVariant->fields[i].name));
                    }
                } else {
                    addError(comp->pos, "type '{}' does not support literal instantiation",
                        resolvedType->toString(sym_));
                }
            },
            [&](ast::MatchExpr* match) {
                analyzeExpr(match->subject);
                const types::Type* subjectType = exprTypeOf(match->subject);

                const types::Type* resultType = nullptr;

                for (auto& arm : match->arms) {
                    pushScope(); // Scope for variables bound in the pattern
                    analyzePattern(arm.pattern, subjectType);
                    analyzeExpr(arm.body);
                    popScope();

                    const types::Type* armType = exprTypeOf(arm.body);

                    if (!resultType) {
                        resultType = armType;
                    } else {
                        const types::Type* merged = mergeTypes(resultType, armType, registry_);
                        if (!merged && armType && armType->kind != types::TypeKind::Unknown
                            && resultType->kind != types::TypeKind::Unknown) {
                            addError(arm.pos,
                                "match arm has incompatible type: expected '{}', got '{}'",
                                resultType->toString(sym_), armType->toString(sym_));
                        }
                        if (merged)
                            resultType = merged;
                    }
                }
                match->exprType
                    = resultType ? resultType : registry_.getPrimitive(types::TypeKind::Unknown);

                // Exhaustiveness Checking
                if (subjectType && subjectType->kind == types::TypeKind::Sum) {
                    const auto& payload = std::get<types::SumPayload>(subjectType->payload);
                    std::vector<bool> covered(payload.variants.size(), false);
                    bool hasWildcard = false;

                    for (const auto& arm : match->arms) {
                        if (std::holds_alternative<ast::WildcardPattern*>(arm.pattern)) {
                            hasWildcard = true;
                            break;
                        } else if (auto ip = std::get_if<ast::IdentifierPattern*>(&arm.pattern)) {
                            // In C++, IdentifierPattern can represent a unit variant OR a
                            // catch-all variable. We must differentiate based on the SumType
                            // definition
                            bool isUnitVariant = false;
                            for (size_t i = 0; i < payload.variants.size(); ++i) {
                                if (payload.variants[i].name == (*ip)->name) {
                                    covered[i] = true;
                                    isUnitVariant = true;
                                    break;
                                }
                            }
                            if (!isUnitVariant) {
                                // It's a catch-all variable binding. Covers all remaining
                                // cases.
                                hasWildcard = true;
                                break;
                            }
                        } else if (auto cp = std::get_if<ast::CompositePattern*>(&arm.pattern)) {
                            if (auto** namedType
                                = std::get_if<ast::NamedTypeExpr*>(&(*cp)->typeExpr)) {
                                SymID varName = (*namedType)->name->name;
                                for (size_t i = 0; i < payload.variants.size(); ++i) {
                                    if (payload.variants[i].name == varName) {
                                        covered[i] = true;
                                        break;
                                    }
                                }
                            }
                        }
                    }

                    if (!hasWildcard) {
                        for (size_t i = 0; i < covered.size(); ++i) {
                            if (!covered[i]) {
                                addError(match->pos,
                                    "non-exhaustive match: missing case for variant '{}'",
                                    sym_.resolve(payload.variants[i].name));
                            }
                        }
                    }
                } else if (subjectType && subjectType->kind != types::TypeKind::Unknown) {
                    // Non-sum types require a wildcard
                    bool hasWildcard = false;
                    for (const auto& arm : match->arms) {
                        if (std::holds_alternative<ast::WildcardPattern*>(arm.pattern)
                            || std::holds_alternative<ast::IdentifierPattern*>(arm.pattern)) {
                            hasWildcard = true;
                            break;
                        }
                    }
                    if (!hasWildcard) {
                        addError(match->pos,
                            "non-exhaustive match: matching on type '{}' requires a wildcard "
                            "'_' "
                            "pattern or catch-all variable",
                            subjectType->toString(sym_));
                    }
                }
            },
            [&](ast::SliceExpr* slice) {
                analyzeExpr(slice->left);
                const types::Type* leftType = exprTypeOf(slice->left);

                if (!std::holds_alternative<std::monostate>(slice->low)) {
                    analyzeExpr(slice->low);
                    const types::Type* lowType = exprTypeOf(slice->low);
                    if (lowType && lowType->kind != types::TypeKind::I64
                        && lowType->kind != types::TypeKind::Unknown) {
                        addError(posOf(slice->low), "slice low index must be an integer");
                    }
                }

                if (!std::holds_alternative<std::monostate>(slice->high)) {
                    analyzeExpr(slice->high);
                    const types::Type* highType = exprTypeOf(slice->high);
                    if (highType && highType->kind != types::TypeKind::I64
                        && highType->kind != types::TypeKind::Unknown) {
                        addError(posOf(slice->high), "slice high index must be an integer");
                    }
                }

                const types::Type* resultType = registry_.getPrimitive(types::TypeKind::Unknown);
                if (leftType) {
                    switch (leftType->kind) {
                    case types::TypeKind::Array:
                        resultType = registry_.getView(
                            std::get<types::ArrayPayload>(leftType->payload).base, false);
                        break;
                    case types::TypeKind::Vector:
                        resultType = registry_.getView(
                            std::get<types::VectorPayload>(leftType->payload).base, false);
                        break;
                    case types::TypeKind::View:
                        resultType = registry_.getView(
                            std::get<types::ViewPayload>(leftType->payload).base, false);
                        break;
                    case types::TypeKind::String:
                        resultType = registry_.getPrimitive(types::TypeKind::String);
                        break;
                    default:
                        if (leftType->kind != types::TypeKind::Unknown) {
                            addError(slice->pos, "cannot slice non-array/vector/view type '{}'",
                                leftType->toString(sym_));
                        }
                    }
                }
                slice->exprType = resultType;
            },
            [&](ast::BlockStmt* block) {
                pushScope();
                for (auto& s : block->statements) {
                    analyzeStmt(s);
                }
                block->exprType = registry_.getPrimitive(types::TypeKind::Unit);
                if (!block->statements.empty()) {
                    if (auto** yieldPtr = std::get_if<ast::YieldStmt*>(&block->statements.back())) {
                        block->exprType = exprTypeOf((*yieldPtr)->value);
                    }
                }
                popScope();
            },
            [&](ast::TypeExprWrapper* tw) {
                addError(tw->pos, "internal error: TypeExprWrapper leaked into expression context");
                tw->exprType = registry_.getPrimitive(types::TypeKind::Unknown);
            },
        },
        expr);
}

// =============================================================================
// Pass 2: Pattern Analysis
// =============================================================================

void Analyzer::analyzePattern(ast::Pattern pattern, const types::Type* subjectType)
{
    if (!subjectType)
        subjectType = registry_.getPrimitive(types::TypeKind::Unknown);

    std::visit(
        overloaded { [&](std::monostate) {},
            [&](ast::WildcardPattern* p) {
                // Wildcard accepts anything, no bindings.
            },
            [&](ast::LiteralPattern* p) {
                analyzeExpr(p->value);
                const types::Type* valType = exprTypeOf(p->value);
                if (valType && valType != subjectType && valType->kind != types::TypeKind::Unknown
                    && subjectType->kind != types::TypeKind::Unknown) {
                    addError(p->pos, "pattern type mismatch: expected '{}', got '{}'",
                        subjectType->toString(sym_), valType->toString(sym_));
                }
            },
            [&](ast::IdentifierPattern* p) {
                // Check if it's a known unit variant first
                if (subjectType->kind == types::TypeKind::Sum) {
                    const auto& payload = std::get<types::SumPayload>(subjectType->payload);
                    for (const auto& variant : payload.variants) {
                        if (variant.name == p->name) {
                            return; // It's a unit variant match, no binding needed.
                        }
                    }
                }

                // Otherwise, it is a catch-all variable binding.
                if (currentScope_->resolveSymbol(p->name) != nullptr) {
                    addError(p->pos, "variable '{}' is already bound in this pattern",
                        sym_.resolve(p->name));
                    return;
                }

                Symbol sym;
                sym.kind = SymbolKind::Var;
                sym.name = p->name;
                sym.type = subjectType;
                currentScope_->defineSymbol(p->name, sym);
            },
            // --- Composite Patterns (Inside Match Arms) ---
            [&](ast::CompositePattern* p) {
                if (subjectType->kind != types::TypeKind::Sum) {
                    if (subjectType->kind != types::TypeKind::Unknown) {
                        addError(p->pos, "cannot match variant pattern on non-sum type '{}'",
                            subjectType->toString(sym_));
                    }
                    return;
                }

                auto** namedType = std::get_if<ast::NamedTypeExpr*>(&p->typeExpr);
                if (!namedType) {
                    addError(p->pos, "invalid type in composite pattern");
                    return;
                }
                SymID variantName = (*namedType)->name->name;

                const auto& payload = std::get<types::SumPayload>(subjectType->payload);
                const types::SumVariant* targetVariant = nullptr;
                for (const auto& v : payload.variants) {
                    if (v.name == variantName) {
                        targetVariant = &v;
                        break;
                    }
                }

                if (!targetVariant) {
                    addError(p->pos, "variant '{}' does not exist on type '{}'",
                        sym_.resolve(variantName), sym_.resolve(payload.baseName));
                    return;
                }

                size_t tupleIdx = 0;
                for (const auto& elem : p->elements) {
                    if (std::holds_alternative<std::monostate>(elem.key)) {
                        // Positional (Tuple) Binding
                        const types::Type* fieldType = nullptr;
                        if (tupleIdx < targetVariant->tupleTypes.size()) {
                            fieldType = targetVariant->tupleTypes[tupleIdx];
                        } else {
                            addError(elem.pos, "extra positional argument in tuple variant '{}'",
                                sym_.resolve(variantName));
                            fieldType = registry_.getPrimitive(types::TypeKind::Unknown);
                        }

                        if (auto identPat = std::get_if<ast::IdentifierPattern*>(&elem.pattern)) {
                            if (currentScope_->resolveSymbol((*identPat)->name)) {
                                addError(elem.pos, "variable '{}' is already bound in this pattern",
                                    sym_.resolve((*identPat)->name));
                            } else {
                                Symbol sym;
                                sym.kind = SymbolKind::Var;
                                sym.name = (*identPat)->name;
                                sym.type = fieldType;
                                currentScope_->defineSymbol((*identPat)->name, sym);
                            }
                        } else {
                            addError(elem.pos,
                                "nested complex sub-patterns are currently unsupported inside "
                                "tuple variants");
                        }
                        tupleIdx++;
                    } else {
                        // Named (Struct) Binding
                        auto keyIdent = std::get_if<ast::Identifier*>(&elem.key);
                        if (!keyIdent) {
                            addError(elem.pos, "struct pattern key must be a valid identifier");
                            continue;
                        }
                        SymID fieldName = (*keyIdent)->name;

                        const types::Type* fieldType = nullptr;
                        for (const auto& vf : targetVariant->fields) {
                            if (vf.name == fieldName) {
                                fieldType = vf.type;
                                break;
                            }
                        }

                        if (!fieldType) {
                            addError(elem.pos, "field '{}' does not exist on variant '{}'",
                                sym_.resolve(fieldName), sym_.resolve(targetVariant->name));
                            fieldType = registry_.getPrimitive(types::TypeKind::Unknown);
                        }

                        if (auto identPat = std::get_if<ast::IdentifierPattern*>(&elem.pattern)) {
                            if (currentScope_->resolveSymbol((*identPat)->name)) {
                                addError(elem.pos, "variable '{}' is already bound in this pattern",
                                    sym_.resolve((*identPat)->name));
                            } else {
                                Symbol sym;
                                sym.kind = SymbolKind::Var;
                                sym.name = (*identPat)->name;
                                sym.type = fieldType;
                                currentScope_->defineSymbol((*identPat)->name, sym);
                            }
                        } else {
                            addError(elem.pos,
                                "nested complex sub-patterns are currently unsupported inside "
                                "struct fields");
                        }
                    }
                }

                if (!targetVariant->tupleTypes.empty()
                    && tupleIdx != targetVariant->tupleTypes.size()) {
                    addError(p->pos,
                        "wrong number of bindings for tuple variant '{}': expected {}, got {}",
                        sym_.resolve(variantName), targetVariant->tupleTypes.size(), tupleIdx);
                }
            } },
        pattern);
}

} // namespace maml::sema