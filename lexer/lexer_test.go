package lexer

import (
	"testing"
)

func TestNextToken(t *testing.T) {
	tests := []struct {
		input     string
		exptected []TokenType
	}{
		{`:= ~= = => == != < > <= >= + - * / % && || ! |> |`, []TokenType{DECLARE_IMMUTABLE, DECLARE_MUTABLE, UPDATE, YIELD, EQ, NOT_EQ, LT, GT, LTE, GTE, PLUS, MINUS, MULTIPLY, DIVIDE, MODULO, AND, OR, NOT, PIPE, SEPARATOR, EOF}},
		{`( ) { } [ ] . , :`, []TokenType{LPAREN, RPAREN, LBRACE, RBRACE, LBRACKET, RBRACKET, DOT, COMMA, COLON, EOF}},
		{`five = 5 ten = 10`, []TokenType{IDENT, UPDATE, INT, IDENT, UPDATE, INT, EOF}},
		{`add = fn(x, y) { x + y }`, []TokenType{IDENT, UPDATE, FN, LPAREN, IDENT, COMMA, IDENT, RPAREN, LBRACE, IDENT, PLUS, IDENT, RBRACE, EOF}},
		{`"foobar"`, []TokenType{STRING, EOF}},
		{`"foo bar"`, []TokenType{STRING, EOF}},
		{`3.14`, []TokenType{FLOAT, EOF}},
		{`42`, []TokenType{INT, EOF}},
		{`nil`, []TokenType{NIL, EOF}},
		{`true false`, []TokenType{BOOL, BOOL, EOF}},
		{`// this is a comment
		  x = 5`, []TokenType{IDENT, UPDATE, INT, EOF}},
		{`id |> fetch_user |> validate`, []TokenType{IDENT, PIPE, IDENT, PIPE, IDENT, EOF}},
		{`@`, []TokenType{ILLEGAL, EOF}},
		{
			`
    		fn add(x: int, y: int) int {
        		=> x + y
    		}
    		`, []TokenType{FN, IDENT, LPAREN, IDENT, COLON, IDENT, COMMA, IDENT, COLON, IDENT, RPAREN, IDENT, LBRACE, YIELD, IDENT, PLUS, IDENT, RBRACE, EOF}},
	}

	for _, tt := range tests {
		l := New(tt.input)

		for i, expectedType := range tt.exptected {
			tok := l.NextToken()
			if tok.Type != expectedType {
				t.Fatalf("tests[%d] - token [%v] type wrong. expected=%v, got=%v",
					i, tok.Literal, expectedType, tok.Type)
			}
		}
	}
}
