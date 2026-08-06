#include "token.h"

#include <string_view>
#include <unordered_map>

namespace maml {

TokenType LookupIdent(std::string_view ident)
{
    static const std::unordered_map<std::string_view, TokenType> keywords
        = { { "fn", TokenType::FN }, { "match", TokenType::MATCH }, { "case", TokenType::CASE },
              { "type", TokenType::TYPE }, { "struct", TokenType::STRUCT },
              { "async", TokenType::ASYNC }, { "await", TokenType::AWAIT },
              { "spawn", TokenType::SPAWN }, { "if", TokenType::IF }, { "else", TokenType::ELSE },
              { "true", TokenType::BOOL_LIT }, { "false", TokenType::BOOL_LIT },
              { "mut", TokenType::MUT }, { "return", TokenType::RETURN }, { "for", TokenType::FOR },
              { "own", TokenType::OWN }, { "ro", TokenType::RO },
              { "continue", TokenType::CONTINUE }, { "break", TokenType::BREAK },
              { "extern", TokenType::EXTERN } };

    if (auto it = keywords.find(ident); it != keywords.end()) {
        return it->second;
    }
    return TokenType::IDENT;
}

std::string_view TokenTypeToString(TokenType type)
{
    switch (type) {
    case TokenType::ILLEGAL:
        return "ILLEGAL";
    case TokenType::END_OF_FILE:
        return "EOF";
    case TokenType::IDENT:
        return "IDENT";
    case TokenType::INT:
        return "INT";
    case TokenType::FLOAT:
        return "FLOAT";
    case TokenType::STRING_LIT:
        return "STRING";
    case TokenType::BOOL_LIT:
        return "BOOL";
    case TokenType::DECLARE:
        return ":=";
    case TokenType::ASSIGN:
        return "=";
    case TokenType::PLUS_EQ:
        return "+=";
    case TokenType::MINUS_EQ:
        return "-=";
    case TokenType::MUL_EQ:
        return "*=";
    case TokenType::DIV_EQ:
        return "/=";
    case TokenType::YIELD:
        return "=>";
    case TokenType::SEPARATOR:
        return "|";
    case TokenType::PIPE:
        return "|>";
    case TokenType::DOT:
        return ".";
    case TokenType::COLON:
        return ":";
    case TokenType::PLUS:
        return "+";
    case TokenType::MINUS:
        return "-";
    case TokenType::MULTIPLY:
        return "*";
    case TokenType::DIVIDE:
        return "/";
    case TokenType::MODULO:
        return "%";
    case TokenType::AND:
        return "&&";
    case TokenType::OR:
        return "||";
    case TokenType::NOT:
        return "!";
    case TokenType::EQ:
        return "==";
    case TokenType::NOT_EQ:
        return "!=";
    case TokenType::LT:
        return "<";
    case TokenType::LTE:
        return "<=";
    case TokenType::GT:
        return ">";
    case TokenType::GTE:
        return ">=";
    case TokenType::PUSH:
        return "<<";
    case TokenType::COMMA:
        return ",";
    case TokenType::LPAREN:
        return "(";
    case TokenType::RPAREN:
        return ")";
    case TokenType::LBRACE:
        return "{";
    case TokenType::RBRACE:
        return "}";
    case TokenType::LBRACKET:
        return "[";
    case TokenType::RBRACKET:
        return "]";
    case TokenType::SEMICOLON:
        return ";";
    case TokenType::FN:
        return "fn";
    case TokenType::MATCH:
        return "match";
    case TokenType::CASE:
        return "case";
    case TokenType::TYPE:
        return "type";
    case TokenType::STRUCT:
        return "struct";
    case TokenType::ASYNC:
        return "async";
    case TokenType::AWAIT:
        return "await";
    case TokenType::SPAWN:
        return "spawn";
    case TokenType::IF:
        return "if";
    case TokenType::ELSE:
        return "else";
    case TokenType::MUT:
        return "mut";
    case TokenType::RETURN:
        return "return";
    case TokenType::FOR:
        return "for";
    case TokenType::OWN:
        return "own";
    case TokenType::RO:
        return "ro";
    case TokenType::BREAK:
        return "break";
    case TokenType::CONTINUE:
        return "continue";
    case TokenType::EXTERN:
        return "extern";
    }
    return "UNKNOWN";
}

} // namespace maml