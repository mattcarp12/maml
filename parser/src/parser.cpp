#include "parser.h"
#include "arena.h"
#include "ast.h"

#include "capability.h"
#include "sym.h"
#include "token.h"
#include <charconv>
#include <cstddef>
#include <format>
#include <string>
#include <type_traits>
#include <variant>
#include <vector>

namespace maml {

template <typename... Args>
void Parser::addError(Position pos, std::format_string<Args...> fmt, Args&&... args)
{
    if (panicMode_ || errors_.size() >= MAX_ERRORS)
        return;
    panicMode_ = true; // Enter panic mode on the first error
    if (errors_.size() >= MAX_ERRORS)
        return;
    errors_.push_back(
        ast::CompileError { "Parser", pos, std::format(fmt, std::forward<Args>(args)...) });
}

Parser::Parser(const std::vector<Token>& tokens, SymbolTable& sym, Arena& arena)
    : tokens_(tokens)
    , sym_(sym)
    , arena_(arena)
{

    // Initialize Precedences
    precedences_[static_cast<size_t>(TokenType::AND)] = AND;
    precedences_[static_cast<size_t>(TokenType::OR)] = OR;
    precedences_[static_cast<size_t>(TokenType::EQ)] = EQUALS;
    precedences_[static_cast<size_t>(TokenType::NOT_EQ)] = EQUALS;
    precedences_[static_cast<size_t>(TokenType::LT)] = LESSGREATER;
    precedences_[static_cast<size_t>(TokenType::LTE)] = LESSGREATER;
    precedences_[static_cast<size_t>(TokenType::GT)] = LESSGREATER;
    precedences_[static_cast<size_t>(TokenType::GTE)] = LESSGREATER;
    precedences_[static_cast<size_t>(TokenType::PLUS)] = SUM;
    precedences_[static_cast<size_t>(TokenType::MINUS)] = SUM;
    precedences_[static_cast<size_t>(TokenType::MULTIPLY)] = PRODUCT;
    precedences_[static_cast<size_t>(TokenType::DIVIDE)] = PRODUCT;
    precedences_[static_cast<size_t>(TokenType::MODULO)] = PRODUCT;
    precedences_[static_cast<size_t>(TokenType::LPAREN)] = CALL;
    precedences_[static_cast<size_t>(TokenType::LBRACE)] = CALL;
    precedences_[static_cast<size_t>(TokenType::DOT)] = CALL;
    precedences_[static_cast<size_t>(TokenType::NOT)] = PREFIX;
    precedences_[static_cast<size_t>(TokenType::LBRACKET)] = INDEX;

    // Register Prefix functions
    prefixParseFns_[static_cast<size_t>(TokenType::IDENT)] = &Parser::parseIdentifier;
    prefixParseFns_[static_cast<size_t>(TokenType::INT)] = &Parser::parseIntegerLiteral;
    prefixParseFns_[static_cast<size_t>(TokenType::BOOL_LIT)] = &Parser::parseBooleanLiteral;
    prefixParseFns_[static_cast<size_t>(TokenType::STRING_LIT)] = &Parser::parseStringLiteral;
    prefixParseFns_[static_cast<size_t>(TokenType::LPAREN)] = &Parser::parseGroupedExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::IF)] = &Parser::parseIfExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::NOT)] = &Parser::parsePrefixExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::MINUS)] = &Parser::parsePrefixExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::MATCH)] = &Parser::parseMatchExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::AWAIT)] = &Parser::parseAwaitExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::SPAWN)] = &Parser::parseSpawnExpression;
    prefixParseFns_[static_cast<size_t>(TokenType::LBRACKET)] = &Parser::parseArrayTypePrefix;

    // Register Infix functions
    infixParseFns_[static_cast<size_t>(TokenType::PLUS)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::MINUS)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::EQ)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::NOT_EQ)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::LT)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::LTE)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::GT)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::GTE)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::MULTIPLY)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::DIVIDE)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::MODULO)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::AND)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::OR)] = &Parser::parseInfixExpression;
    infixParseFns_[static_cast<size_t>(TokenType::LPAREN)] = &Parser::parseCallExpression;
    infixParseFns_[static_cast<size_t>(TokenType::LBRACE)] = &Parser::parseCompositeLiteral;
    infixParseFns_[static_cast<size_t>(TokenType::DOT)] = &Parser::parseFieldAccess;
    infixParseFns_[static_cast<size_t>(TokenType::LBRACKET)] = &Parser::parseIndexExpression;

    nextToken();
    nextToken();
}

void Parser::nextToken()
{
    curToken_ = peekToken_;

    if (tokenIndex_ < tokens_.size()) {
        peekToken_ = tokens_[tokenIndex_++];
    } else {
        // Safe fallback in case of overrun.
        // Ensure this matches your updated Position struct!
        peekToken_ = { TokenType::END_OF_FILE, "", { "", 0, 0 } };
    }
}

bool Parser::expectPeek(TokenType t)
{
    if (peekToken_.type == t) {
        nextToken();
        return true;
    }
    peekError(t);
    return false;
}

void Parser::peekError(TokenType t)
{
    addError(peekToken_.pos, "expected next token to be '{}', got '{}'", TokenTypeToString(t),
        TokenTypeToString(peekToken_.type));
}

Precedence Parser::peekPrecedence() const
{
    return precedences_[static_cast<size_t>(peekToken_.type)];
}

Precedence Parser::curPrecedence() const
{
    return precedences_[static_cast<size_t>(curToken_.type)];
}

void Parser::synchronize()
{
    panicMode_ = false; // We are attempting to recover
    while (curToken_.type != TokenType::END_OF_FILE) {
        if (curToken_.type == TokenType::RBRACE)
            return;
        if (curToken_.type == TokenType::SEMICOLON) {
            nextToken();
            return;
        }
        if (peekToken_.type == TokenType::RBRACE) {
            nextToken();
            return;
        }
        nextToken();
    }
}

void Parser::synchronizeToDecl()
{
    panicMode_ = false;
    while (curToken_.type != TokenType::END_OF_FILE) {
        if (curToken_.type == TokenType::FN || curToken_.type == TokenType::TYPE
            || curToken_.type == TokenType::ASYNC) {
            return;
        }
        nextToken();
    }
}

bool Parser::parseCommaSeparatedList(TokenType endToken, auto parseElemCallback)
{
    if (peekToken_.type == endToken) {
        nextToken();
        return true;
    }
    nextToken();
    parseElemCallback();
    while (peekToken_.type == TokenType::COMMA) {
        nextToken();
        nextToken();
        parseElemCallback();
    }
    return expectPeek(endToken);
}

// =============================================================================
// Top-Level Declarations
// =============================================================================

ast::Program* Parser::parseProgram()
{
    auto* prog = makeNode<ast::Program>();
    while (curToken_.type != TokenType::END_OF_FILE) {
        if (errors_.size() >= MAX_ERRORS)
            break;
        ast::Decl decl = parseDecl();
        if (!std::holds_alternative<std::monostate>(decl)) {
            prog->decls.push_back(decl);
        } else {
            synchronizeToDecl();
            continue;
        }
        if (curToken_.type != TokenType::FN && curToken_.type != TokenType::TYPE
            && curToken_.type != TokenType::ASYNC) {
            nextToken();
        }
    }
    return finishNode(prog);
}

ast::Decl Parser::parseDecl()
{
    switch (curToken_.type) {
    case TokenType::FN:
    case TokenType::ASYNC:
    case TokenType::EXTERN:
        return parseFnDecl();
    case TokenType::TYPE:
        return parseTypeDecl();
    default:
        addError(
            curToken_.pos, "only function and type declarations are supported at the top level");
        return std::monostate {};
    }
}

ast::FnDecl* Parser::parseFnDecl()
{
    auto* fn = makeNode<ast::FnDecl>();
    fn->isAsync = false;
    fn->isExtern = false;

    if (curToken_.type == TokenType::ASYNC) {
        fn->isAsync = true;
        if (!expectPeek(TokenType::FN))
            return nullptr;
    } else if (curToken_.type == TokenType::EXTERN) {
        fn->isExtern = true;
        if (!expectPeek(TokenType::FN))
            return nullptr;
    }

    if (!expectPeek(TokenType::IDENT))
        return nullptr;
    fn->name = sym_.intern(curToken_.literal);

    if (!expectPeek(TokenType::LPAREN))
        return nullptr;
    fn->params = parseFnParams();

    if (peekToken_.type == TokenType::IDENT || peekToken_.type == TokenType::LBRACKET) {
        nextToken();
        fn->returnType = parseTypeExpr();
    } else {
        fn->returnType = std::monostate {};
    }

    if (fn->isExtern) {
        if (peekToken_.type == TokenType::SEMICOLON)
            nextToken();
        fn->body = nullptr;
    } else {
        if (!expectPeek(TokenType::LBRACE))
            return nullptr;
        fn->body = parseBlockStmt();
    }

    return finishNode(fn);
}

std::vector<ast::Param> Parser::parseFnParams()
{
    std::vector<ast::Param> params;
    parseCommaSeparatedList(TokenType::RPAREN, [&]() { params.push_back(parseParam()); });
    return params;
}

ast::Param Parser::parseParam()
{
    ast::Param p;
    p.pos = curToken_.pos;

    if (curToken_.type == TokenType::MUT || curToken_.type == TokenType::OWN
        || curToken_.type == TokenType::RO) {
        p.cap = parseCapability(curToken_.literal);
        nextToken();
    }

    if (curToken_.type != TokenType::IDENT) {
        addError(curToken_.pos, "expected parameter name");
        return p;
    }
    p.name = sym_.intern(curToken_.literal);
    nextToken();
    p.type = parseTypeExpr();
    p.end = curToken_.pos;
    return p;
}

ast::TypeDecl* Parser::parseTypeDecl()
{
    auto* td = makeNode<ast::TypeDecl>();
    if (!expectPeek(TokenType::IDENT))
        return nullptr;

    td->name = makeNode<ast::Identifier>();
    td->name->name = sym_.intern(curToken_.literal);
    finishNode(td->name);

    if (!expectPeek(TokenType::ASSIGN))
        return nullptr;
    nextToken();

    if (curToken_.type == TokenType::LBRACE) {
        td->rhs = parseProductType();
    } else if (curToken_.type == TokenType::SEPARATOR || curToken_.type == TokenType::IDENT) {
        td->rhs = parseSumType();
    } else {
        addError(curToken_.pos, "expected '{{' or '|' in type declaration");
        return nullptr;
    }

    expectPeek(TokenType::SEMICOLON);
    return finishNode(td);
}

ast::SumTypeExpr* Parser::parseSumType()
{
    auto* st = makeNode<ast::SumTypeExpr>();
    st->pos = curToken_.pos;

    if (curToken_.type == TokenType::SEPARATOR)
        nextToken();

    while (true) {
        if (curToken_.type != TokenType::IDENT) {
            addError(curToken_.pos, "expected variant name identifier");
            return nullptr;
        }
        st->variants.push_back(parseSumVariant());

        // Only advance if there is another '|' variant coming
        if (peekToken_.type == TokenType::SEPARATOR) {
            nextToken(); // curToken_ is now '|'
            nextToken(); // curToken_ is now the next variant name
        } else {
            break; // Leaves peekToken_ pointing to ';'
        }
    }
    st->end = curToken_.pos;
    return st;
}

ast::VariantTypeExpr Parser::parseSumVariant()
{
    ast::VariantTypeExpr variant;
    variant.pos = curToken_.pos;
    variant.name = sym_.intern(curToken_.literal);

    if (peekToken_.type == TokenType::LBRACE) {
        nextToken();
        if (auto* pt = parseProductType()) {
            variant.fields = pt->fields;
        }
    } else if (peekToken_.type == TokenType::LPAREN) {
        nextToken();
        if (peekToken_.type != TokenType::RPAREN) {
            while (true) {
                nextToken();
                variant.tupleFields.push_back(parseTypeExpr());
                if (peekToken_.type == TokenType::COMMA) {
                    nextToken();
                    if (peekToken_.type == TokenType::RPAREN)
                        break;
                } else {
                    break;
                }
            }
        }
        expectPeek(TokenType::RPAREN);
    }
    variant.end = curToken_.pos;
    return variant;
}

ast::StructTypeExpr* Parser::parseProductType()
{
    auto* pt = makeNode<ast::StructTypeExpr>();
    pt->pos = curToken_.pos;

    if (peekToken_.type == TokenType::RBRACE) {
        nextToken();
        pt->end = curToken_.pos;
        return pt;
    }

    auto parseField = [&]() {
        ast::Param param = parseParam();
        pt->fields.push_back({ param.pos, param.end, param.name, param.type });
    };

    nextToken();
    parseField();

    while (peekToken_.type == TokenType::COMMA) {
        nextToken();
        nextToken();
        parseField();
    }
    nextToken();

    if (curToken_.type != TokenType::RBRACE) {
        addError(curToken_.pos, "expected '}}'");
        return nullptr;
    }
    pt->end = curToken_.pos;
    return pt;
}

// =============================================================================
// Statements
// =============================================================================

ast::BlockStmt* Parser::parseBlockStmt()
{
    auto* block = makeNode<ast::BlockStmt>();
    block->pos = curToken_.pos;
    nextToken();

    while (curToken_.type != TokenType::RBRACE && curToken_.type != TokenType::END_OF_FILE) {
        if (errors_.size() >= MAX_ERRORS)
            break;
        size_t errorsBefore = errors_.size();
        ast::Stmt stmt = parseStmt();

        if (!std::holds_alternative<std::monostate>(stmt)) {
            block->statements.push_back(stmt);
        } else if (errors_.size() > errorsBefore) {
            synchronize();
            continue;
        }

        if (peekToken_.type == TokenType::RBRACE) {
            nextToken();
            break;
        }
        nextToken();
    }
    block->end = curToken_.pos;
    return block;
}

ast::Stmt Parser::parseStmt()
{
    switch (curToken_.type) {
    case TokenType::MUT:
        return parseDeclareStmt();
    case TokenType::OWN:
    case TokenType::RO:
        if (peekToken_.type == TokenType::IDENT && tokenIndex_ < tokens_.size()
            && tokens_[tokenIndex_].type == TokenType::DECLARE) {
            addError(curToken_.pos, "invalid annotation on left side of declaration");
            return std::monostate {};
        }
        return parseExpressionStmt();
    case TokenType::IDENT:
        if (peekToken_.type == TokenType::DECLARE)
            return parseDeclareStmt();
        return parseExpressionStmt();
    case TokenType::RETURN:
        return parseReturnStmt();
    case TokenType::YIELD:
        return parseYieldStmt();
    case TokenType::FOR:
        return parseForStmt();
    case TokenType::BREAK:
        return parseBreakStmt();
    case TokenType::CONTINUE:
        return parseContinueStmt();
    default:
        return parseExpressionStmt();
    }
}

ast::Stmt Parser::parseDeclareStmt()
{
    Position pos = curToken_.pos;
    bool isMutable = false;

    if (curToken_.type == TokenType::MUT) {
        isMutable = true;
        if (!expectPeek(TokenType::IDENT))
            return std::monostate {};
    }

    SymID name = sym_.intern(curToken_.literal);
    if (!expectPeek(TokenType::DECLARE))
        return std::monostate {};
    nextToken();

    bool isAlias = false;
    Capability cap = Capability::Ro; // only meaningful when isAlias == true

    if (curToken_.type == TokenType::MUT || curToken_.type == TokenType::OWN
        || curToken_.type == TokenType::RO) {
        isAlias = true;
        cap = parseCapability(curToken_.literal);
        nextToken();
    }

    ast::Expr value = parseExpression(LOWEST);
    expectPeek(TokenType::SEMICOLON); // Explicit semicolon required

    if (isAlias) {
        if (isMutable) {
            addError(curToken_.pos, "Alias Declarations not allowed to be mutable.");
            return std::monostate {};
        }
        auto* alias = makeNode<ast::AliasDecl>();
        alias->pos = pos;
        alias->end = curToken_.pos;
        alias->cap = cap;
        alias->name = name;
        alias->value = value;
        return alias;
    }

    auto* decl = makeNode<ast::DeclareStmt>();
    decl->pos = pos;
    decl->end = curToken_.pos;
    decl->isMutable = isMutable;
    decl->name = name;
    decl->value = value;
    return decl;
}

ast::ReturnStmt* Parser::parseReturnStmt()
{
    auto* stmt = makeNode<ast::ReturnStmt>();

    if (peekToken_.type == TokenType::SEMICOLON) {
        nextToken();
        stmt->value = std::monostate {};
    } else {
        nextToken();
        stmt->value = parseExpression(LOWEST);
        expectPeek(TokenType::SEMICOLON);
    }
    return finishNode(stmt);
}

ast::YieldStmt* Parser::parseYieldStmt()
{
    auto* stmt = makeNode<ast::YieldStmt>();
    nextToken();
    stmt->value = parseExpression(LOWEST);
    expectPeek(TokenType::SEMICOLON);
    return finishNode(stmt);
}

ast::Stmt Parser::parseExpressionStmt()
{
    Position pos = curToken_.pos;
    ast::Expr expr = parseExpression(LOWEST);

    if (peekToken_.type == TokenType::ASSIGN || peekToken_.type == TokenType::PLUS_EQ
        || peekToken_.type == TokenType::MINUS_EQ || peekToken_.type == TokenType::MUL_EQ
        || peekToken_.type == TokenType::DIV_EQ) {

        TokenType op = peekToken_.type;
        nextToken();
        nextToken();

        ast::Expr rValue = parseExpression(LOWEST);
        if (peekToken_.type == TokenType::SEMICOLON) {
            nextToken();
        } else if (peekToken_.type != TokenType::RPAREN) {
            expectPeek(TokenType::SEMICOLON);
        }

        auto* assign = makeNode<ast::AssignStmt>();
        assign->pos = pos;
        assign->end = curToken_.pos;
        assign->lValue = expr;
        assign->op = op;
        assign->rValue = rValue;
        return assign;
    }

    if (peekToken_.type == TokenType::PUSH) {
        nextToken();
        nextToken();
        ast::Expr rValue = parseExpression(LOWEST);
        if (peekToken_.type == TokenType::SEMICOLON) {
            nextToken();
        } else if (peekToken_.type != TokenType::RPAREN) {
            expectPeek(TokenType::SEMICOLON);
        }

        auto* push = makeNode<ast::VecPushStmt>();
        push->pos = pos;
        push->end = curToken_.pos;
        push->lValue = expr;
        push->rValue = rValue;
        return push;
    }

    bool isBlockLike = std::holds_alternative<ast::IfExpr*>(expr)
        || std::holds_alternative<ast::MatchExpr*>(expr);

    if (!isBlockLike) {
        if (peekToken_.type == TokenType::SEMICOLON) {
            nextToken();
        } else if (peekToken_.type != TokenType::RPAREN) {
            expectPeek(TokenType::SEMICOLON);
        }
    } else if (peekToken_.type == TokenType::SEMICOLON) {
        nextToken(); // tolerate an optional semicolon
    }

    auto* estmt = makeNode<ast::ExprStmt>();
    estmt->pos = pos;
    estmt->end = curToken_.pos;
    estmt->value = expr;
    return estmt;
}

ast::ForStmt* Parser::parseForStmt()
{
    auto* stmt = makeNode<ast::ForStmt>();
    stmt->pos = curToken_.pos;

    // We now enforce parentheses around the condition/init block
    expectPeek(TokenType::LPAREN);
    nextToken(); // Step into parens

    ast::Stmt first = parseStmt(); // Will consume its own semicolon if it's a declare/assign

    if (curToken_.type == TokenType::SEMICOLON || std::holds_alternative<ast::DeclareStmt*>(first)
        || std::holds_alternative<ast::AssignStmt*>(first)) {
        // It's a C-style for loop
        stmt->init = first;
        if (curToken_.type == TokenType::SEMICOLON)
            nextToken(); // skip it if parseStmt didn't

        stmt->condition = parseExpression(LOWEST);
        expectPeek(TokenType::SEMICOLON);
        nextToken();

        stmt->post = parseStmt(); // Note: This parseStmt will look for a semicolon. C-style for
                                  // posts don't usually have one. We might need a parseSimpleStmt
                                  // here, but for now we reuse.
    } else {
        // While loop style
        if (auto* estmt = std::get_if<ast::ExprStmt*>(&first)) {
            stmt->condition = (*estmt)->value;
            stmt->init = std::monostate {};
            stmt->post = std::monostate {};
        }
    }

    expectPeek(TokenType::RPAREN);
    if (!expectPeek(TokenType::LBRACE))
        return nullptr;

    stmt->body = parseBlockStmt();
    stmt->end = curToken_.pos;
    return stmt;
}

ast::BreakStmt* Parser::parseBreakStmt()
{
    auto* stmt = makeNode<ast::BreakStmt>();
    stmt->token = curToken_;
    expectPeek(TokenType::SEMICOLON);
    return finishNode(stmt);
}

ast::ContinueStmt* Parser::parseContinueStmt()
{
    auto* stmt = makeNode<ast::ContinueStmt>();
    stmt->token = curToken_;
    expectPeek(TokenType::SEMICOLON);
    return finishNode(stmt);
}

// =============================================================================
// Expressions (Pratt Core)
// =============================================================================

ast::Expr Parser::parseExpression(Precedence precedence)
{
    PrefixFn prefix = prefixParseFns_[static_cast<size_t>(curToken_.type)];
    if (!prefix) {
        addError(curToken_.pos, "no prefix parse function for type '{}' found",
            TokenTypeToString(curToken_.type));
        return std::monostate {};
    }

    ast::Expr leftExp = (this->*prefix)();

    while (peekToken_.type != TokenType::END_OF_FILE && precedence < peekPrecedence()) {
        InfixFn infix = infixParseFns_[static_cast<size_t>(peekToken_.type)];
        if (!infix)
            return leftExp;

        nextToken();
        leftExp = (this->*infix)(leftExp);
    }

    return leftExp;
}

ast::Expr Parser::parseIdentifier()
{
    Position pos = curToken_.pos;
    SymID name = sym_.intern(curToken_.literal);

    if (peekToken_.type == TokenType::LBRACE) {
        auto* named = makeNode<ast::NamedTypeExpr>();
        named->pos = pos;
        named->name = makeNode<ast::Identifier>();
        named->name->name = name;
        named->name->pos = pos;

        auto* wrapper = makeNode<ast::TypeExprWrapper>();
        wrapper->pos = pos;
        wrapper->typeExpr = named;
        return wrapper;
    }

    if (peekToken_.type == TokenType::LT && looksLikeGenericInstantiation()) {
        nextToken(); // Advance so curToken_ is '<'
        auto* genType = parseGenericTypeExpr(name, pos);

        auto* wrapper = makeNode<ast::TypeExprWrapper>();
        wrapper->pos = pos;
        wrapper->typeExpr = genType;
        return wrapper;
    }

    auto* id = makeNode<ast::Identifier>();
    id->name = name;
    id->pos = pos;
    id->end = curToken_.pos;
    return id;
}

ast::Expr Parser::parseIntegerLiteral()
{
    auto* lit = makeNode<ast::IntLiteral>();
    std::from_chars(
        curToken_.literal.data(), curToken_.literal.data() + curToken_.literal.size(), lit->value);
    return finishNode(lit);
}

ast::Expr Parser::parseBooleanLiteral()
{
    auto* lit = makeNode<ast::BoolLiteral>();
    lit->value = (curToken_.literal == "true");
    return finishNode(lit);
}

static std::string unescapeString(std::string_view raw)
{
    std::string result;
    result.reserve(raw.size());

    for (size_t i = 0; i < raw.size(); ++i) {
        if (raw[i] == '\\' && i + 1 < raw.size()) {
            ++i; // Step past the backslash
            switch (raw[i]) {
            case 'n':
                result.push_back('\n');
                break;
            case 't':
                result.push_back('\t');
                break;
            case 'r':
                result.push_back('\r');
                break;
            case '0':
                result.push_back('\0');
                break;
            case '\\':
                result.push_back('\\');
                break;
            case '"':
                result.push_back('"');
                break;
            default:
                result.push_back(raw[i]);
                break;
            }
        } else {
            result.push_back(raw[i]);
        }
    }

    return result;
}

ast::Expr Parser::parseStringLiteral()
{
    auto* lit = makeNode<ast::StringLiteral>();
    lit->value = unescapeString(curToken_.literal);
    return finishNode(lit);
}

ast::Expr Parser::parsePrefixExpression()
{
    auto* expr = makeNode<ast::PrefixExpr>();
    expr->pos = curToken_.pos;
    expr->op = curToken_.type;
    nextToken();
    expr->right = parseExpression(PREFIX);
    expr->end = curToken_.pos;
    return expr;
}

ast::Expr Parser::parseInfixExpression(ast::Expr left)
{
    auto* expr = makeNode<ast::InfixExpr>();
    expr->pos = std::visit(
        [](auto&& arg) -> Position {
            using T = std::decay_t<decltype(arg)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return Position {};
            } else {
                return arg ? arg->pos : Position {};
            }
        },
        left);
    expr->left = left;
    expr->op = curToken_.type;

    Precedence precedence = curPrecedence();
    nextToken();
    expr->right = parseExpression(precedence);
    expr->end = curToken_.pos;
    return expr;
}

ast::Expr Parser::parseGroupedExpression()
{
    nextToken();
    ast::Expr exp = parseExpression(LOWEST);
    if (!expectPeek(TokenType::RPAREN))
        return std::monostate {};
    return exp;
}

ast::Expr Parser::parseIfExpression()
{
    auto* expr = makeNode<ast::IfExpr>();

    expr->condition = parseParenthesized([this] { return parseExpression(LOWEST); });
    if (std::holds_alternative<std::monostate>(expr->condition))
        return std::monostate {};

    if (!expectPeek(TokenType::LBRACE))
        return std::monostate {};

    expr->consequence = parseBlockStmt();

    if (peekToken_.type == TokenType::ELSE) {
        nextToken();
        if (peekToken_.type == TokenType::IF) {
            nextToken();
            ast::Expr innerIf = parseIfExpression();
            auto* yield = makeNode<ast::YieldStmt>();
            yield->value = innerIf;
            finishNode(yield);

            auto* altBlock = makeNode<ast::BlockStmt>();
            altBlock->statements.push_back(yield);
            finishNode(altBlock);

            expr->alternative = altBlock;
        } else {
            if (!expectPeek(TokenType::LBRACE))
                return std::monostate {};
            expr->alternative = parseBlockStmt();
        }
    } else {
        expr->alternative = nullptr;
    }

    return finishNode(expr);
}

ast::Expr Parser::parseMatchExpression()
{
    auto* expr = makeNode<ast::MatchExpr>();

    expr->subject = parseParenthesized([this] { return parseExpression(LOWEST); });
    if (std::holds_alternative<std::monostate>(expr->subject))
        return std::monostate {};

    if (!expectPeek(TokenType::LBRACE))
        return std::monostate {};
    nextToken();

    while (curToken_.type != TokenType::RBRACE && curToken_.type != TokenType::END_OF_FILE) {
        expr->arms.push_back(parseMatchArm());
        nextToken();
    }

    return finishNode(expr);
}

ast::MatchArm Parser::parseMatchArm()
{
    ast::MatchArm arm;
    if (curToken_.type != TokenType::CASE) {
        addError(curToken_.pos, "expected 'case'");
        return arm;
    }
    arm.pos = curToken_.pos;
    nextToken();
    arm.pattern = parsePattern();

    if (!expectPeek(TokenType::COLON))
        return arm;
    nextToken();

    if (curToken_.type == TokenType::YIELD) {
        nextToken();
        ast::Expr yieldVal = parseExpression(LOWEST);
        expectPeek(TokenType::SEMICOLON); // Need semicolon for one-liners
        auto* block = makeNode<ast::BlockStmt>();
        auto* ys = makeNode<ast::YieldStmt>();
        ys->value = yieldVal;
        block->statements.push_back(ys);
        arm.body = block;
    } else if (curToken_.type == TokenType::LBRACE) {
        arm.body = parseBlockStmt();
    }

    arm.end = curToken_.pos;
    return arm;
}

ast::Pattern Parser::parsePattern()
{
    Position pos = curToken_.pos;
    if (curToken_.type == TokenType::IDENT) {
        if (curToken_.literal == "_") {
            auto* w = makeNode<ast::WildcardPattern>();
            w->pos = pos;
            return w;
        }

        SymID name = sym_.intern(curToken_.literal);

        // Check if this is a variant pattern like Active(level) or Error{code: c}
        if (peekToken_.type == TokenType::LPAREN || peekToken_.type == TokenType::LBRACE) {
            bool isBraced = (peekToken_.type == TokenType::LBRACE);
            TokenType closeToken = isBraced ? TokenType::RBRACE : TokenType::RPAREN;

            auto* cp = makeNode<ast::CompositePattern>();
            cp->pos = pos;

            auto* named = makeNode<ast::NamedTypeExpr>();
            named->pos = pos;
            named->name = makeNode<ast::Identifier>();
            named->name->name = name;
            named->name->pos = pos;
            cp->typeExpr = named;

            nextToken(); // Advance curToken_ to '(' or '{'

            while (peekToken_.type != closeToken && peekToken_.type != TokenType::END_OF_FILE) {
                nextToken(); // Move to the start of the next element/field

                ast::CompositePatternElement el;
                el.pos = curToken_.pos;

                // Handle key: pattern bindings like `code: c` in struct patterns
                if (isBraced && peekToken_.type == TokenType::COLON) {
                    auto* keyId = makeNode<ast::Identifier>();
                    keyId->name = sym_.intern(curToken_.literal);
                    keyId->pos = curToken_.pos;
                    keyId->end = curToken_.pos;
                    el.key = keyId;

                    nextToken(); // Advance to ':'
                    nextToken(); // Advance to value pattern
                    el.pattern = parsePattern();
                } else {
                    el.key = std::monostate {};
                    el.pattern = parsePattern();
                }

                el.end = curToken_.pos;
                cp->elements.push_back(el);

                if (peekToken_.type == TokenType::COMMA) {
                    nextToken(); // Consume ','
                } else if (peekToken_.type != closeToken) {
                    addError(peekToken_.pos, "expected ',' or closing delimiter in pattern");
                    break;
                }
            }

            if (peekToken_.type == closeToken) {
                nextToken(); // Consume ')' or '}'
            }
            return cp;
        }

        // Standard identifier pattern
        auto* ip = makeNode<ast::IdentifierPattern>();
        ip->pos = pos;
        ip->name = name;
        return ip;
    } else if (curToken_.type == TokenType::INT) {
        auto* lit = makeNode<ast::LiteralPattern>();
        lit->pos = pos;
        lit->value = parseIntegerLiteral();
        return lit;
    }

    addError(curToken_.pos, "invalid pattern");
    return std::monostate {};
}

ast::Expr Parser::parseCallExpression(ast::Expr function)
{
    auto* expr = makeNode<ast::CallExpr>();
    expr->pos = curToken_.pos;
    expr->function = function;

    parseCommaSeparatedList(TokenType::RPAREN, [&]() {
        ast::CallArg arg;
        arg.pos = curToken_.pos;
        if (curToken_.type == TokenType::MUT || curToken_.type == TokenType::OWN
            || curToken_.type == TokenType::RO) {
            arg.cap = parseCapability(curToken_.literal);
            nextToken();
        }
        arg.argument = parseExpression(LOWEST);
        expr->arguments.push_back(arg);
    });

    expr->end = curToken_.pos;
    return expr;
}

ast::Expr Parser::parseCompositeLiteral(ast::Expr left)
{
    auto* cl = makeNode<ast::CompositeLiteral>();
    cl->pos = curToken_.pos;
    cl->typeExpr = extractTypeExpr(left);

    parseCommaSeparatedList(TokenType::RBRACE, [&]() {
        ast::CompositeElement el;
        el.pos = curToken_.pos;
        ast::Expr first = parseExpression(LOWEST);

        if (peekToken_.type == TokenType::COLON) {
            nextToken();
            nextToken();
            el.key = first;
            el.value = parseExpression(LOWEST);
        } else {
            el.key = std::monostate {};
            el.value = first;
        }
        cl->elements.push_back(el);
    });

    cl->end = curToken_.pos;
    return cl;
}

bool Parser::looksLikeGenericInstantiation() const
{
    // We are currently at curToken_ (the identifier), peekToken_ is '<'
    // tokenIndex_ is the token AFTER peekToken_
    size_t i = tokenIndex_;
    int angleBrackets = 1;

    while (i < tokens_.size()) {
        TokenType t = tokens_[i].type;
        if (t == TokenType::GT) {
            angleBrackets--;
            if (angleBrackets == 0) {
                // We found the matching '>'. If the next token is '{', it's a generic literal
                if (i + 1 < tokens_.size() && tokens_[i + 1].type == TokenType::LBRACE) {
                    return true;
                }
                return false;
            }
        } else if (t == TokenType::LT) {
            angleBrackets++;
        } else if (t == TokenType::SEMICOLON || t == TokenType::LBRACE
            || t == TokenType::END_OF_FILE) {
            // Abort early if we hit statement boundaries before closing the brackets
            return false;
        }
        i++;
    }
    return false;
}

ast::Expr Parser::parseFieldAccess(ast::Expr left)
{
    auto* fa = makeNode<ast::FieldAccess>();
    fa->pos = curToken_.pos;
    fa->object = left;

    if (!expectPeek(TokenType::IDENT))
        return std::monostate {};
    fa->field = makeNode<ast::Identifier>();
    fa->field->name = sym_.intern(curToken_.literal);
    fa->field->pos = curToken_.pos;
    fa->end = curToken_.pos;
    return fa;
}

ast::Expr Parser::parseIndexExpression(ast::Expr left)
{
    nextToken();
    ast::Expr low;
    if (curToken_.type != TokenType::COLON) {
        low = parseExpression(LOWEST);
    }

    if (peekToken_.type == TokenType::COLON || curToken_.type == TokenType::COLON) {
        if (peekToken_.type == TokenType::COLON)
            nextToken();
        ast::Expr high;
        if (peekToken_.type != TokenType::RBRACKET) {
            nextToken();
            high = parseExpression(LOWEST);
        }
        expectPeek(TokenType::RBRACKET);
        auto* slice = makeNode<ast::SliceExpr>();
        slice->left = left;
        slice->low = low;
        slice->high = high;
        return slice;
    }

    expectPeek(TokenType::RBRACKET);
    auto* idx = makeNode<ast::IndexExpr>();
    idx->left = left;
    idx->index = low;
    return idx;
}

ast::Expr Parser::parseAwaitExpression()
{
    auto* aw = makeNode<ast::AwaitExpr>();
    aw->pos = curToken_.pos;
    nextToken();
    aw->value = parseExpression(PREFIX);
    return aw;
}

ast::Expr Parser::parseSpawnExpression()
{
    auto* sp = makeNode<ast::SpawnExpr>();
    sp->pos = curToken_.pos;
    nextToken();
    ast::Expr val = parseExpression(PREFIX);
    if (auto* call = std::get_if<ast::CallExpr*>(&val)) {
        sp->value = *call;
    } else {
        addError(curToken_.pos, "can only spawn a call expression");
    }
    return sp;
}

ast::Expr Parser::parseArrayTypePrefix()
{
    auto* tw = makeNode<ast::TypeExprWrapper>();
    tw->pos = curToken_.pos;
    tw->typeExpr = parseTypeExpr();
    return tw;
}

// =============================================================================
// Types
// =============================================================================

ast::TypeExpr Parser::parseTypeExpr()
{
    Position pos = curToken_.pos;

    if (curToken_.type == TokenType::LBRACKET) {
        if (!expectPeek(TokenType::INT))
            return std::monostate {};
        int size = 0;
        std::from_chars(
            curToken_.literal.data(), curToken_.literal.data() + curToken_.literal.size(), size);
        if (!expectPeek(TokenType::RBRACKET))
            return std::monostate {};
        nextToken();
        auto* arr = makeNode<ast::ArrayTypeExpr>();
        arr->size = size;
        arr->base = parseTypeExpr();
        return arr;
    }

    if (curToken_.type == TokenType::IDENT) {
        SymID name = sym_.intern(curToken_.literal);
        if (peekToken_.type == TokenType::LT) {
            nextToken();
            return parseGenericTypeExpr(name, pos);
        }
        auto* named = makeNode<ast::NamedTypeExpr>();
        named->pos = pos;
        named->name = makeNode<ast::Identifier>();
        named->name->name = name;
        named->name->pos = pos;
        return named;
    }
    addError(curToken_.pos, "expected a type");
    return std::monostate {};
}

ast::GenericTypeExpr* Parser::parseGenericTypeExpr(SymID name, Position pos)
{
    auto* gen = makeNode<ast::GenericTypeExpr>();
    gen->pos = pos;
    gen->name = makeNode<ast::Identifier>();
    gen->name->name = name;

    nextToken();
    gen->args.push_back(parseTypeExpr());

    while (peekToken_.type == TokenType::COMMA) {
        nextToken();
        nextToken();
        gen->args.push_back(parseTypeExpr());
    }
    expectPeek(TokenType::GT);
    gen->end = curToken_.pos;
    return gen;
}

ast::TypeExpr Parser::extractTypeExpr(ast::Expr expr)
{
    if (auto* w = std::get_if<ast::TypeExprWrapper*>(&expr))
        return (*w)->typeExpr;
    if (auto* id = std::get_if<ast::Identifier*>(&expr)) {
        auto* named = makeNode<ast::NamedTypeExpr>();
        named->name = *id;
        return named;
    }
    return std::monostate {};
}

} // namespace maml