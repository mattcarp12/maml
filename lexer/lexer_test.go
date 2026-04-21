package lexer

import (
	"testing"

	"github.com/mattcarp12/maml/token"
)

func TestNextToken(t *testing.T) {
	input := `:=~==||>`

	tests := []struct {
		exptectedType token.TokenType
		expectedLiteral string
	}{
		{token.DECLARE_IMMUTABLE, ":="},
		{token.DECLARE_MUTABLE, "~="},
		{token.UPDATE, "="},
		{token.OR, "||"},
		{token.GT, ">"},
		{token.EOF, ""},
	}

	l := New(input)

	for i, tt := range tests {
		tok := l.NextToken()

		if tok.Type != tt.exptectedType {
			t.Fatalf("tests[%d] - tokentype wrong. expected=%q, got=%q", i, tt.exptectedType, tok.Type)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Fatalf("tests[%d] - literal wrong. expected=%q, got=%q", i, tt.expectedLiteral, tok.Literal)
		}
	}
}
