#pragma once
#include "ast.h"
#include "scope.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include <format>
#include <vector>

namespace maml::sema {

class Analyzer {
public:
    Analyzer(types::TypeRegistry& registry, SymbolTable& sym);
    ~Analyzer();

    // Disable copy/move
    Analyzer(const Analyzer&) = delete;
    Analyzer& operator=(const Analyzer&) = delete;

    // Entry point
    void analyze(ast::Program* program);

    const std::vector<ast::CompileError>& getErrors() const { return errors_; }

private:
    types::TypeRegistry& registry_;
    SymbolTable& sym_;

    Scope* currentScope_ = nullptr;
    std::vector<Scope*> scopes_; // Tracks allocated scopes for memory cleanup
    std::vector<ast::CompileError> errors_;

    // Context state for rule checking
    const types::Type* expectedReturn_ = nullptr;
    bool isAsync_ = false;
    bool allowAsyncCall_ = false;

    // Scope management
    void pushScope();
    void popScope();
    Symbol* resolve(SymID name);
    const types::Type* lookupCustomType(SymID name);

    // Recursively strip field accesses and index expressions to find the base identifier.
    Symbol* getRootSymbol(ast::Expr expr);

    // Error handling
    template <typename... Args>
    void addError(Position pos, std::format_string<Args...> fmt, Args&&... args);

    // --- Pass 1: Hoisting ---
    void discoverTypes(ast::Program* program);
    void resolveTypeBodies(ast::Program* program);
    void resolveTypeBody(ast::TypeDecl* decl);
    void registerFunctions(ast::Program* program);
    void registerFunction(ast::FnDecl* decl);

    // --- Type Resolution ---
    const types::Type* resolveAstType(ast::TypeExpr expr);
    const types::Type* resolveGenericBuiltin(ast::GenericTypeExpr* expr);

    // --- Pass 2: AST Decoration & Rule Checking (Implemented in Phase 4) ---
    void analyzeDecl(ast::Decl decl);
    void analyzeStmt(ast::Stmt stmt);
    void analyzeExpr(ast::Expr expr, const types::Type* expectedType = nullptr);
    void analyzePattern(ast::Pattern pattern, const types::Type* subjectType);
};

} // namespace maml::sema