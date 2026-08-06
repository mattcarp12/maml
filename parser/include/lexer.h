#pragma once

#include "token.h"

#include <cstddef>
#include <string_view>
#include <utility>

namespace maml {

class Lexer {
public:
    explicit Lexer(std::string_view input, std::string_view filename);

    Token nextToken();

private:
    void readChar();
    [[nodiscard]] char peekChar() const;
    void skipWhitespace();
    void skipComment();
    std::string_view readIdentifier();
    std::pair<std::string_view, TokenType> readNumber();
    std::string_view readString();

    static bool isLetter(char ch);
    static bool isDigit(char ch);

    Token newToken(TokenType type, size_t startLine, size_t startCol, size_t len = 1);
    Token twoCharToken(TokenType type, size_t startLine, size_t startCol);

    std::string_view input_;
    std::string_view filename_;
    size_t position_ {};
    size_t readPosition_ {};
    char ch_ {};
    uint32_t line_ {};
    uint32_t col_ {};
};

} // namespace maml