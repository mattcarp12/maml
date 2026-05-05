package lexer

import "github.com/mattcarp12/maml/internal/token"

// canEndStatement determines if the current token type legally ends a statement.
func canEndStatement(typ token.TokenType) bool {
	switch typ {
	case token.IDENT, token.INT, token.FLOAT, token.STRING, token.BOOL, token.RPAREN, token.RBRACE, token.RBRACKET:
		return true
	default:
		return false
	}
}
