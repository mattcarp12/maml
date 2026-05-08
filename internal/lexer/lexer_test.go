package lexer

import (
	"testing"

	"github.com/mattcarp12/maml/internal/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []token.TokenType
	}{
		{
			name:  "operators and delimiters",
			input: `:= = => == != < > <= >= + - * / % && || ! |> | : . ,`,
			expected: []token.TokenType{
				token.DECLARE, token.ASSIGN, token.YIELD, token.EQ, token.NOT_EQ,
				token.LT, token.GT, token.LTE, token.GTE,
				token.PLUS, token.MINUS, token.MULTIPLY, token.DIVIDE, token.MODULO,
				token.AND, token.OR, token.NOT, token.PIPE, token.SEPARATOR,
				token.COLON, token.DOT, token.COMMA, token.EOF,
			},
		},
		{
			name:  "keywords and literals",
			input: `fn match case type struct async await if else true false mut return`,
			expected: []token.TokenType{
				token.FN, token.MATCH, token.CASE, token.TYPE, token.STRUCT,
				token.ASYNC, token.AWAIT, token.IF, token.ELSE,
				token.BOOL, token.BOOL, token.MUT, token.RETURN, token.EOF,
			},
		},
		{
			name:  "delimiters and brackets",
			input: `( ) { } [ ]`,
			expected: []token.TokenType{
				token.LPAREN, token.RPAREN, token.LBRACE, token.RBRACE,
				token.LBRACKET, token.RBRACKET, token.EOF,
			},
		},
		{
			name:  "identifiers and assignment",
			input: `five = 5 ten = 10`,
			expected: []token.TokenType{
				token.IDENT, token.ASSIGN, token.INT,
				token.IDENT, token.ASSIGN, token.INT, token.EOF,
			},
		},
		{
			name:  "function definition",
			input: `add = fn(x, y) { x + y }`,
			expected: []token.TokenType{
				token.IDENT, token.ASSIGN, token.FN, token.LPAREN, token.IDENT,
				token.COMMA, token.IDENT, token.RPAREN, token.LBRACE,
				token.IDENT, token.PLUS, token.IDENT, token.RBRACE, token.EOF,
			},
		},
		{
			name:  "strings",
			input: `"foobar" "foo bar" "unterminated`,
			expected: []token.TokenType{
				token.STRING, token.STRING, token.ILLEGAL, token.EOF,
			},
		},
		{
			name:  "numbers",
			input: `42 3.14 0.5`,
			expected: []token.TokenType{
				token.INT, token.FLOAT, token.FLOAT, token.EOF,
			},
		},
		{
			name: "comments and whitespace",
			input: `// this is a comment
x = 5`,
			expected: []token.TokenType{
				token.IDENT, token.ASSIGN, token.INT, token.EOF,
			},
		},
		{
			name:  "pipe operator",
			input: `id |> fetch_user |> validate`,
			expected: []token.TokenType{
				token.IDENT, token.PIPE, token.IDENT, token.PIPE, token.IDENT, token.EOF,
			},
		},
		{
			name:  "illegal character",
			input: `@ # $`,
			expected: []token.TokenType{
				token.ILLEGAL, token.ILLEGAL, token.ILLEGAL, token.EOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			var got []token.TokenType

			for {
				tok := l.NextToken()
				got = append(got, tok.Type)
				if tok.Type == token.EOF {
					break
				}
			}

			assert.Equal(t, tt.expected, got, "token sequence mismatch")
		})
	}
}

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

		// Line 3: => inside block
		{token.YIELD, 3, 2, "=>"},
		{token.IDENT, 3, 5, "x"},
		{token.PLUS, 3, 7, "+"},
		{token.IDENT, 3, 9, "y"},

		{token.NEWLINE, 3, 10, "\\n"},

		{token.RBRACE, 4, 1, "}"},
		{token.NEWLINE, 4, 2, "\\n"},

		{token.IDENT, 6, 1, "z"},
		{token.DECLARE, 6, 3, ":="},
		{token.FLOAT, 6, 6, "42.5"},
		{token.NEWLINE, 6, 10, "\\n"},

		{token.EOF, 7, 1, ""},
	}

	l := New(input)

	for i, exp := range expected {
		tok := l.NextToken()

		require.Equal(t, exp.typ, tok.Type,
			"token %d type mismatch", i)

		assert.Equal(t, exp.line, tok.Line,
			"token %d (%s) line wrong", i, tok.Literal)

		assert.Equal(t, exp.col, tok.Col,
			"token %d (%s) column wrong", i, tok.Literal)

		if exp.lit != "" {
			assert.Equal(t, exp.lit, tok.Literal,
				"token %d literal mismatch", i)
		}
	}
}

// Additional focused tests

func TestAutomaticSemicolonInsertion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []token.TokenType
	}{
		{
			name:  "newline after statement ends it",
			input: "x := 5\ny := 10",
			expected: []token.TokenType{
				token.IDENT, token.DECLARE, token.INT,
				token.NEWLINE,
				token.IDENT, token.DECLARE, token.INT,
				token.EOF,
			},
		},
		{
			name:  "newline inside brackets is ignored",
			input: "fn add(x, y) {\nx + y\n}",
			expected: []token.TokenType{
				token.FN, token.IDENT, token.LPAREN, token.IDENT, token.COMMA,
				token.IDENT, token.RPAREN, token.LBRACE,
				token.IDENT, token.PLUS, token.IDENT, token.NEWLINE,
				token.RBRACE, token.EOF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			var got []token.TokenType

			for {
				tok := l.NextToken()
				if tok.Type == token.EOF {
					got = append(got, tok.Type)
					break
				}
				got = append(got, tok.Type)
			}

			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestStringAndCommentEdgeCases(t *testing.T) {
	input := `// comment with "quote"
"hello world"
"unterminated string
"escaped \"quote\""
`

	l := New(input)
	tokens := []token.TokenType{}

	for {
		tok := l.NextToken()
		tokens = append(tokens, tok.Type)
		if tok.Type == token.EOF {
			break
		}
	}

	require.Contains(t, tokens, token.STRING)
	require.Contains(t, tokens, token.ILLEGAL) // unterminated string
}
