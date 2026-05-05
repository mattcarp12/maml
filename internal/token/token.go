package token

//go:generate stringer -type=TokenType
type TokenType int

const (
	ILLEGAL TokenType = iota
	EOF
	NEWLINE // \n

	// Identifiers + Literals
	IDENT  // e.g., myVar, add, x
	INT    // e.g., 42, 100
	FLOAT  // e.g., 3.14, 0.5
	STRING // e.g., "hello world"
	BOOL   // true, false

	// Operators
	DECLARE   // :=
	ASSIGN    // =
	YIELD     // =>
	SEPARATOR // |
	PIPE      // |>
	DOT       // .
	COLON     // :
	PLUS      // +
	MINUS     // -
	MULTIPLY  // *
	DIVIDE    // /
	MODULO    // %
	AND       // &&
	OR        // ||
	NOT       // !
	EQ        // ==
	NOT_EQ    // !=
	LT        // <
	GT        // >
	LTE       // <=
	GTE       // >=

	// Delimiters
	COMMA     // ,
	LPAREN    // (
	RPAREN    // )
	LBRACE    // {
	RBRACE    // }
	LBRACKET  // [
	RBRACKET  // ]
	SEMICOLON // ;

	// Keywords
	FN     // fn
	MATCH  // match
	CASE   // case
	TYPE   // type
	STRUCT // struct
	ASYNC  // async
	AWAIT  // await
	IF     // if
	ELSE   // else
	MUT    // mut
	RETURN // return
)

var keywords = map[string]TokenType{
	"fn":     FN,
	"match":  MATCH,
	"case":   CASE,
	"type":   TYPE,
	"struct": STRUCT,
	"async":  ASYNC,
	"await":  AWAIT,
	"if":     IF,
	"else":   ELSE,
	"true":   BOOL,
	"false":  BOOL,
	"mut":    MUT,
	"return": RETURN,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}
