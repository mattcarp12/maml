package lexer

import (
	"testing"
)

func TestNextToken(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{`:= ~= = => == != < > <= >= + - * / % && || ! |> |`, []TokenType{DECLARE_IMMUTABLE, DECLARE_MUTABLE, UPDATE, YIELD, EQ, NOT_EQ, LT, GT, LTE, GTE, PLUS, MINUS, MULTIPLY, DIVIDE, MODULO, AND, OR, NOT, PIPE, SEPARATOR, EOF}},
		{`( ) { } [ ] . , :`, []TokenType{LPAREN, RPAREN, LBRACE, RBRACE, LBRACKET, RBRACKET, DOT, COMMA, COLON, EOF}},
		{`five = 5 ten = 10`, []TokenType{IDENT, UPDATE, INT, IDENT, UPDATE, INT, EOF}},
		{`add = fn(x, y) { x + y }`, []TokenType{IDENT, UPDATE, FN, LPAREN, IDENT, COMMA, IDENT, RPAREN, LBRACE, IDENT, PLUS, IDENT, RBRACE, EOF}},
		{`"foobar"`, []TokenType{STRING, EOF}},
		{`"foo bar"`, []TokenType{STRING, EOF}},
		{`"unterminated string`, []TokenType{ILLEGAL, EOF}},
		{`3.14`, []TokenType{FLOAT, EOF}},
		{`42`, []TokenType{INT, EOF}},
		{`true false`, []TokenType{BOOL, BOOL, EOF}},
		{`// this is a comment
		  x = 5`, []TokenType{IDENT, UPDATE, INT, EOF}}, // Newline skipped: previous state (start) can't end a statement
		{`id |> fetch_user |> validate`, []TokenType{IDENT, PIPE, IDENT, PIPE, IDENT, EOF}},
		{`@`, []TokenType{ILLEGAL, EOF}},
		{
			`fn add(x: int, y: int) int {
        		=> x + y
    		}`, []TokenType{FN, IDENT, LPAREN, IDENT, COLON, IDENT, COMMA, IDENT, COLON, IDENT, RPAREN, IDENT, LBRACE, YIELD, IDENT, PLUS, IDENT, RBRACE, EOF}}, // Newlines inside {} are skipped!
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
		typ  TokenType
		line int
		col  int
		lit  string
	}{
		// Line 1
		{FN, 1, 1, "fn"},
		{IDENT, 1, 4, "add"},
		{LPAREN, 1, 7, "("},
		{IDENT, 1, 8, "x"},
		{COLON, 1, 9, ":"},
		{IDENT, 1, 11, "int"},
		{COMMA, 1, 14, ","},
		{IDENT, 1, 16, "y"},
		{COLON, 1, 17, ":"},
		{IDENT, 1, 19, "int"},
		{RPAREN, 1, 22, ")"},
		{IDENT, 1, 24, "int"},
		{LBRACE, 1, 28, "{"},
		// The \n after { is skipped due to bracketDepth > 0

		// Line 2: tab + comment + \n -> skipped entirely

		// Line 3: tab + =>
		{YIELD, 3, 2, "=>"}, // col 2 because of leading \t
		{IDENT, 3, 5, "x"},
		{PLUS, 3, 7, "+"},
		{IDENT, 3, 9, "y"},
		// The \n after y is skipped due to bracketDepth > 0

		// Line 4: }
		{RBRACE, 4, 1, "}"},
		// bracketDepth is now 0, and RBRACE can end a statement.
		// So the \n after } IS emitted!
		{NEWLINE, 4, 2, "\\n"},

		// Line 5: blank line (\n) -> skipped! NEWLINE cannot end a statement.

		// Line 6
		// {IDENT, 6, 1, "let"},
		{IDENT, 6, 1, "z"},
		{DECLARE_IMMUTABLE, 6, 3, ":="},
		{FLOAT, 6, 6, "42.5"},
		// FLOAT can end a statement, so the final \n IS emitted!
		{NEWLINE, 6, 10, "\\n"},

		// End of file
		{EOF, 7, 1, ""},
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