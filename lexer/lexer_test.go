package lexer

import (
	"testing"

	"github.com/mattcarp12/maml/token"
)

func TestNextToken(t *testing.T) {
	tests := []struct {
		input    string
		expected []token.TokenType
	}{
		{`:= = => == != < > <= >= + - * / % && || ! |> |`, []token.TokenType{token.DECLARE, token.ASSIGN, token.YIELD, token.EQ, token.NOT_EQ, token.LT, token.GT, token.LTE, token.GTE, token.PLUS, token.MINUS, token.MULTIPLY, token.DIVIDE, token.MODULO, token.AND, token.OR, token.NOT, token.PIPE, token.SEPARATOR, token.EOF}},
		{`fn match case type struct async await if else true false mut return`, []token.TokenType{token.FN, token.MATCH, token.CASE, token.TYPE, token.STRUCT, token.ASYNC, token.AWAIT, token.IF, token.ELSE, token.BOOL, token.BOOL, token.MUT, token.RETURN}},
		{`( ) { } [ ] . , :`, []token.TokenType{token.LPAREN, token.RPAREN, token.LBRACE, token.RBRACE, token.LBRACKET, token.RBRACKET, token.DOT, token.COMMA, token.COLON, token.EOF}},
		{`five = 5 ten = 10`, []token.TokenType{token.IDENT, token.ASSIGN, token.INT, token.IDENT, token.ASSIGN, token.INT, token.EOF}},
		{`add = fn(x, y) { x + y }`, []token.TokenType{token.IDENT, token.ASSIGN, token.FN, token.LPAREN, token.IDENT, token.COMMA, token.IDENT, token.RPAREN, token.LBRACE, token.IDENT, token.PLUS, token.IDENT, token.RBRACE, token.EOF}},
		{`"foobar"`, []token.TokenType{token.STRING, token.EOF}},
		{`"foo bar"`, []token.TokenType{token.STRING, token.EOF}},
		{`"unterminated string`, []token.TokenType{token.ILLEGAL, token.EOF}},
		{`3.14`, []token.TokenType{token.FLOAT, token.EOF}},
		{`42`, []token.TokenType{token.INT, token.EOF}},
		{`true false`, []token.TokenType{token.BOOL, token.BOOL, token.EOF}},
		{`// this is a comment
		  x = 5`, []token.TokenType{token.IDENT, token.ASSIGN, token.INT, token.EOF}}, // Newline skipped: previous state (start) can't end a statement
		{`id |> fetch_user |> validate`, []token.TokenType{token.IDENT, token.PIPE, token.IDENT, token.PIPE, token.IDENT, token.EOF}},
		{`@`, []token.TokenType{token.ILLEGAL, token.EOF}},
		{
			`fn add(x: int, y: int) int {
        		return x + y
    		}`, []token.TokenType{token.FN, token.IDENT, token.LPAREN, token.IDENT, token.COLON, token.IDENT, token.COMMA, token.IDENT, token.COLON, token.IDENT, token.RPAREN, token.IDENT, token.LBRACE, token.RETURN, token.IDENT, token.PLUS, token.IDENT, token.NEWLINE, token.RBRACE, token.EOF}},
	}

	for testNum, tt := range tests {
		l := New(tt.input)

		for _, expectedType := range tt.expected {
			tok := l.NextToken()
			if tok.Type != expectedType {
				t.Fatalf("tests[%d] - token [%v] type wrong. expected=%v, got=%v",
					testNum, tok.Literal, expectedType, tok.Type)
			}
		}
	}
}

// TestTokenPositions verifies accurate Line and Col reporting,
// as well as ensuring ASI logic correctly drops/keeps newlines.
func TestTokenPositions(t *testing.T) {
	input := `fn add(x: int, y: int) int {
	// comment
	=> x + y
}

z := 42.5
`

	expected := []struct {
		typ  token.TokenType
		line int
		col  int
		lit  string
	}{
		// Line 1
		{token.FN, 1, 1, "fn"},
		{token.IDENT, 1, 4, "add"},
		{token.LPAREN, 1, 7, "("},
		{token.IDENT, 1, 8, "x"},
		{token.COLON, 1, 9, ":"},
		{token.IDENT, 1, 11, "int"},
		{token.COMMA, 1, 14, ","},
		{token.IDENT, 1, 16, "y"},
		{token.COLON, 1, 17, ":"},
		{token.IDENT, 1, 19, "int"},
		{token.RPAREN, 1, 22, ")"},
		{token.IDENT, 1, 24, "int"},
		{token.LBRACE, 1, 28, "{"},
		// The \n after { is skipped due to bracketDepth > 0

		// Line 2: tab + comment + \n -> skipped entirely

		// Line 3: tab + =>
		{token.YIELD, 3, 2, "=>"}, // Note: You're still using => here instead of return, which is fine for the Lexer test!
		{token.IDENT, 3, 5, "x"},
		{token.PLUS, 3, 7, "+"},
		{token.IDENT, 3, 9, "y"},

		// ASI correctly catches the newline after the identifier 'y'
		{token.NEWLINE, 3, 10, "\\n"},

		// Line 4: }
		{token.RBRACE, 4, 1, "}"},
		// bracketDepth is now 0, and RBRACE can end a statement.
		// So the \n after } IS emitted!
		{token.NEWLINE, 4, 2, "\\n"},

		// Line 5: blank line (\n) -> skipped! NEWLINE cannot end a statement.

		// Line 6
		// {IDENT, 6, 1, "let"},
		{token.IDENT, 6, 1, "z"},
		{token.DECLARE, 6, 3, ":="},
		{token.FLOAT, 6, 6, "42.5"},
		// FLOAT can end a statement, so the final \n IS emitted!
		{token.NEWLINE, 6, 10, "\\n"},

		// End of file
		{token.EOF, 7, 1, ""},
	}

	l := New(input)

	for i, exp := range expected {
		tok := l.NextToken()

		if tok.Type != exp.typ {
			t.Errorf("token %d - type wrong. expected=%v, got=%v (literal=%q)",
				i, exp.typ, tok.Type, tok.Literal)
			continue
		}
		if tok.Line != exp.line {
			t.Errorf("token %d (%s) - line wrong. expected=%d, got=%d",
				i, tok.Literal, exp.line, tok.Line)
		}
		if tok.Col != exp.col {
			t.Errorf("token %d (%s) - column wrong. expected=%d, got=%d",
				i, tok.Literal, exp.col, tok.Col)
		}
	}
}
