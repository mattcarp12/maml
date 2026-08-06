#pragma once

#include "arena.h"
#include "ast.h"

#include "sym.h"
#include "token.h"
#include <array>
#include <cstddef>
#include <cstdint>
#include <format>
#include <type_traits>
#include <vector>

namespace maml {

enum Precedence : uint8_t {
    LOWEST = 0,
    OR,
    AND,
    EQUALS, // == or !=
    LESSGREATER, // > or < or >= or <=
    SUM, // + or -
    PRODUCT, // * or / or %
    PREFIX, // -X, !X, or await X
    CALL, // fn() or struct literal or field access
    INDEX // array[index]
};

class Parser {
public:
    using PrefixFn = ast::Expr (Parser::*)();
    using InfixFn = ast::Expr (Parser::*)(ast::Expr);

    Parser(const std::vector<Token>& tokens, SymbolTable& sym, Arena& arena);

    ast::Program* parseProgram();
    const std::vector<ast::CompileError>& getErrors() const { return errors_; }

private:
    const std::vector<Token>& tokens_;
    size_t tokenIndex_ = 0;

    SymbolTable& sym_;
    Arena& arena_;

    Token curToken_;
    Token peekToken_;

    std::vector<ast::CompileError> errors_;
    static constexpr size_t MAX_ERRORS = 25;
    bool panicMode_ = false;

    std::array<PrefixFn, 256> prefixParseFns_ {};
    std::array<InfixFn, 256> infixParseFns_ {};
    std::array<Precedence, 256> precedences_ {};

    // Core Setup
    void nextToken();
    // Error handling
    template <typename... Args>
    void addError(Position pos, std::format_string<Args...> fmt, Args&&... args);
    bool expectPeek(TokenType t);
    void peekError(TokenType t);
    Precedence peekPrecedence() const;
    Precedence curPrecedence() const;
    void synchronize();
    void synchronizeToDecl();

    // Declarations
    ast::Decl parseDecl();
    ast::FnDecl* parseFnDecl();
    std::vector<ast::Param> parseFnParams();
    ast::Param parseParam();
    ast::TypeDecl* parseTypeDecl();
    ast::SumTypeExpr* parseSumType();
    ast::VariantTypeExpr parseSumVariant();
    ast::StructTypeExpr* parseProductType();

    // Statements
    ast::BlockStmt* parseBlockStmt();
    ast::Stmt parseStmt();
    ast::Stmt parseDeclareStmt();
    ast::ReturnStmt* parseReturnStmt();
    ast::YieldStmt* parseYieldStmt();
    ast::Stmt parseExpressionStmt();
    ast::ForStmt* parseForStmt();
    ast::BreakStmt* parseBreakStmt();
    ast::ContinueStmt* parseContinueStmt();

    // Expressions
    ast::Expr parseExpression(Precedence precedence);
    ast::Expr parseIdentifier();
    ast::Expr parseIntegerLiteral();
    ast::Expr parseBooleanLiteral();
    ast::Expr parseStringLiteral();
    ast::Expr parsePrefixExpression();
    ast::Expr parseInfixExpression(ast::Expr left);
    ast::Expr parseGroupedExpression();
    ast::Expr parseIfExpression();
    ast::Expr parseMatchExpression();
    ast::MatchArm parseMatchArm();
    ast::Pattern parsePattern();
    ast::Expr parseCallExpression(ast::Expr function);
    ast::Expr parseArrayTypePrefix();
    ast::Expr parseCompositeLiteral(ast::Expr left);
    ast::Expr parseFieldAccess(ast::Expr left);
    ast::Expr parseIndexExpression(ast::Expr left);
    ast::Expr parseAwaitExpression();
    ast::Expr parseSpawnExpression();

    // Types
    ast::TypeExpr parseTypeExpr();
    ast::GenericTypeExpr* parseGenericTypeExpr(SymID name, Position pos);
    ast::TypeExpr extractTypeExpr(ast::Expr expr);

    // Helpers
    bool parseCommaSeparatedList(TokenType endToken, auto parseElemCallback);
    bool looksLikeGenericInstantiation() const;

    // =============================================================================
    // Helper Utilities
    // =============================================================================

    // AST Node Allocation & Position Factory
    template <typename T, typename... Args> T* makeNode(Args&&... args)
    {
        auto* node = arena_.make<T>(std::forward<Args>(args)...);
        node->pos = curToken_.pos;
        return node;
    }

    // Marks the end position of a node right before returning
    template <typename T> T* finishNode(T* node)
    {
        if (node) {
            node->end = curToken_.pos;
        }
        return node;
    }

    // Generic helper for matching enclosed structures: ( expr ), { stmt }, etc.
    template <typename Fn> auto parseEnclosed(TokenType openToken, TokenType closeToken, Fn&& fn)
    {
        using Ret = std::invoke_result_t<Fn>;
        if (!expectPeek(openToken)) {
            return Ret {};
        }
        nextToken(); // Step inside enclosure
        auto res = fn();
        if (!expectPeek(closeToken)) {
            return Ret {};
        }
        return res;
    }

    template <typename Fn> auto parseParenthesized(Fn&& fn)
    {
        return parseEnclosed(TokenType::LPAREN, TokenType::RPAREN, std::forward<Fn>(fn));
    }

    template <typename Fn> auto parseBraced(Fn&& fn)
    {
        return parseEnclosed(TokenType::LBRACE, TokenType::RBRACE, std::forward<Fn>(fn));
    }
};

} // namespace maml