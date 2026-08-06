#pragma once

#include <cstdint>
#include <string_view>

namespace maml {

enum class TokenType : uint8_t {
    ILLEGAL,
    END_OF_FILE,

    // Identifiers & Literals
    IDENT,
    INT,
    FLOAT,
    STRING_LIT,
    BOOL_LIT,

    // Operators
    DECLARE, // :=
    ASSIGN, // =
    PLUS_EQ, // +=
    MINUS_EQ, // -=
    MUL_EQ, // *=
    DIV_EQ, // /=
    YIELD, // =>
    SEPARATOR, // |
    PIPE, // |>
    DOT, // .
    COLON, // :
    PLUS, // +
    MINUS, // -
    MULTIPLY, // *
    DIVIDE, // /
    MODULO, // %
    AND, // &&
    OR, // ||
    NOT, // !
    EQ, // ==
    NOT_EQ, // !=
    LT, // <
    GT, // >
    LTE, // <=
    GTE, // >=
    PUSH, // <<

    // Delimiters
    COMMA,
    LPAREN,
    RPAREN,
    LBRACE,
    RBRACE,
    LBRACKET,
    RBRACKET,
    SEMICOLON,

    // Keywords
    FN,
    MATCH,
    CASE,
    TYPE,
    STRUCT,
    ASYNC,
    AWAIT,
    SPAWN,
    IF,
    ELSE,
    MUT,
    RETURN,
    FOR,
    OWN,
    RO,
    BREAK,
    CONTINUE,
    EXTERN
};

struct Position {
    std::string_view filename = "";
    uint32_t line = 1;
    uint32_t col = 0;
};

struct Token {
    TokenType type;
    std::string_view literal;
    Position pos;
};

// Returns the specific keyword type, or IDENT if it's a generic identifier.
TokenType LookupIdent(std::string_view ident);

std::string_view TokenTypeToString(TokenType type);

} // namespace maml