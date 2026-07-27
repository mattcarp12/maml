#include "lexer.h"
#include <string_view>
#include <utility>

namespace maml {

Token Lexer::nextToken()
{
    skipWhitespace();

    size_t startLine = line_;
    size_t startCol = col_;

    Token tok;

    switch (ch_) {
    case '=':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::EQ, startLine, startCol);
        else if (peekChar() == '>')
            tok = twoCharToken(TokenType::YIELD, startLine, startCol);
        else
            tok = newToken(TokenType::ASSIGN, startLine, startCol);
        break;
    case ':':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::DECLARE, startLine, startCol);
        else
            tok = newToken(TokenType::COLON, startLine, startCol);
        break;
    case '|':
        if (peekChar() == '>')
            tok = twoCharToken(TokenType::PIPE, startLine, startCol);
        else if (peekChar() == '|')
            tok = twoCharToken(TokenType::OR, startLine, startCol);
        else
            tok = newToken(TokenType::SEPARATOR, startLine, startCol);
        break;
    case '&':
        if (peekChar() == '&')
            tok = twoCharToken(TokenType::AND, startLine, startCol);
        else
            tok = newToken(TokenType::ILLEGAL, startLine, startCol);
        break;
    case '!':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::NOT_EQ, startLine, startCol);
        else
            tok = newToken(TokenType::NOT, startLine, startCol);
        break;
    case '<':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::LTE, startLine, startCol);
        else if (peekChar() == '<') {
            tok = twoCharToken(TokenType::PUSH, startLine, startCol);
            readChar(); // Consume the second '<'[cite: 1]
        } else
            tok = newToken(TokenType::LT, startLine, startCol);
        break;
    case '>':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::GTE, startLine, startCol);
        else
            tok = newToken(TokenType::GT, startLine, startCol);
        break;
    case '+':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::PLUS_EQ, startLine, startCol);
        else
            tok = newToken(TokenType::PLUS, startLine, startCol);
        break;
    case '-':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::MINUS_EQ, startLine, startCol);
        else
            tok = newToken(TokenType::MINUS, startLine, startCol);
        break;
    case '*':
        if (peekChar() == '=')
            tok = twoCharToken(TokenType::MUL_EQ, startLine, startCol);
        else
            tok = newToken(TokenType::MULTIPLY, startLine, startCol);
        break;
    case '/':
        if (peekChar() == '/') {
            skipComment();
            return nextToken();
        } else if (peekChar() == '=')
            tok = twoCharToken(TokenType::DIV_EQ, startLine, startCol);
        else
            tok = newToken(TokenType::DIVIDE, startLine, startCol);
        break;
    case '%':
        tok = newToken(TokenType::MODULO, startLine, startCol);
        break;
    case '.':
        tok = newToken(TokenType::DOT, startLine, startCol);
        break;
    case ',':
        tok = newToken(TokenType::COMMA, startLine, startCol);
        break;
    case ';':
        tok = newToken(TokenType::SEMICOLON, startLine, startCol);
        break;
    case '{':
        tok = newToken(TokenType::LBRACE, startLine, startCol);
        break;
    case '}':
        tok = newToken(TokenType::RBRACE, startLine, startCol);
        break;
    case '(':
        tok = newToken(TokenType::LPAREN, startLine, startCol);
        break;
    case ')':
        tok = newToken(TokenType::RPAREN, startLine, startCol);
        break;
    case '[':
        tok = newToken(TokenType::LBRACKET, startLine, startCol);
        break;
    case ']':
        tok = newToken(TokenType::RBRACKET, startLine, startCol);
        break;
    case '"':
        tok.literal = readString();
        tok.pos = { static_cast<uint32_t>(startLine), static_cast<uint32_t>(startCol) };
        tok.type = (ch_ == '"') ? TokenType::STRING_LIT : TokenType::ILLEGAL;
        break;
    case 0:
        tok.type = TokenType::END_OF_FILE;
        tok.literal = "";
        tok.pos = { static_cast<uint32_t>(startLine), static_cast<uint32_t>(startCol) };
        break;
    default:
        if (isLetter(ch_)) {
            tok.literal = readIdentifier();
            tok.type = LookupIdent(tok.literal);
            tok.pos = { static_cast<uint32_t>(startLine), static_cast<uint32_t>(startCol) };
            return tok; // Early return because readIdentifier advanced ch_
        } else if (isDigit(ch_)) {
            auto [literal, type] = readNumber();
            tok.literal = literal;
            tok.type = type;
            tok.pos = { static_cast<uint32_t>(startLine), static_cast<uint32_t>(startCol) };
            return tok; // Early return because readNumber advanced ch_
        } else {
            tok = newToken(TokenType::ILLEGAL, startLine, startCol);
        }
    }

    readChar();
    return tok;
}

void Lexer::readChar()
{
    if (ch_ == '\n') {
        line_++;
        col_ = 0;
    }

    if (readPosition_ >= input_.length()) {
        ch_ = 0;
    } else {
        ch_ = input_[readPosition_];
    }

    position_ = readPosition_;
    readPosition_++;
    col_++;
}

char Lexer::peekChar() const
{
    if (readPosition_ >= input_.length()) {
        return 0;
    }
    return input_[readPosition_];
}

void Lexer::skipWhitespace()
{
    // Newlines are now skipped entirely just like spaces and tabs
    while (ch_ == ' ' || ch_ == '\t' || ch_ == '\r' || ch_ == '\n') {
        readChar();
    }
}

void Lexer::skipComment()
{
    while (ch_ != '\n' && ch_ != 0) {
        readChar();
    }
}

std::string_view Lexer::readIdentifier()
{
    size_t startPos = position_;
    while (isLetter(ch_) || isDigit(ch_)) {
        readChar();
    }
    return input_.substr(startPos, position_ - startPos);
}

std::pair<std::string_view, TokenType> Lexer::readNumber()
{
    size_t startPos = position_;
    while (isDigit(ch_)) {
        readChar();
    }
    if (ch_ == '.' && isDigit(peekChar())) {
        readChar(); // consume '.'
        while (isDigit(ch_)) {
            readChar();
        }
        return { input_.substr(startPos, position_ - startPos), TokenType::FLOAT };
    }
    return { input_.substr(startPos, position_ - startPos), TokenType::INT };
}

std::string_view Lexer::readString()
{
    // Note: C++ string_views do not handle escape character resolution automatically.
    // If you need \n to resolve to actual newlines in memory rather than literally,
    // we will need a separate unescape pass later. For zero-allocation parsing,
    // we capture the raw boundaries.
    readChar(); // Step off opening '"'
    size_t startPos = position_;
    while (ch_ != '"' && ch_ != 0) {
        if (ch_ == '\\')
            readChar(); // skip escape
        readChar();
    }
    std::string_view result = input_.substr(startPos, position_ - startPos);
    return result;
}

bool Lexer::isLetter(char ch)
{
    return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ch == '_';
}

bool Lexer::isDigit(char ch) { return '0' <= ch && ch <= '9'; }

Token Lexer::newToken(TokenType type, size_t startLine, size_t startCol, size_t len)
{
    return { type, input_.substr(position_, len),
        { static_cast<uint32_t>(startLine), static_cast<uint32_t>(startCol) } };
}

Token Lexer::twoCharToken(TokenType type, size_t startLine, size_t startCol)
{
    Token tok = newToken(type, startLine, startCol, 2);
    readChar(); // Consume second char
    return tok;
}

} // namespace maml