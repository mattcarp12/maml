package lexer

type TokenType string

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"
	NEWLINE = "NEWLINE"

	// Identifiers + Literals
	IDENT  = "IDENT"
	INT    = "INT"
	FLOAT  = "FLOAT"
	STRING = "STRING"
	BOOL   = "BOOL"

	// Operators
	DECLARE_IMMUTABLE = ":="
	DECLARE_MUTABLE   = "~="
	UPDATE            = "="
	YIELD             = "=>"
	SEPARATOR         = "|"
	PIPE              = "|>"
	DOT               = "."
	COLON             = ":"
	PLUS              = "+"
	MINUS             = "-"
	MULTIPLY          = "*"
	DIVIDE            = "/"
	MODULO            = "%"
	AND               = "&&"
	OR                = "||"
	NOT               = "!"
	EQ                = "=="
	NOT_EQ            = "!="
	LT                = "<"
	GT                = ">"
	LTE               = "<="
	GTE               = ">="

	//  Delimiters
	COMMA    = ","
	LPAREN   = "("
	RPAREN   = ")"
	LBRACE   = "{"
	RBRACE   = "}"
	LBRACKET = "["
	RBRACKET = "]"

	// Keywords
	FN     = "FN"
	MATCH  = "MATCH"
	CASE   = "CASE"
	TYPE   = "TYPE"
	STRUCT = "STRUCT"
	ASYNC  = "ASYNC"
	AWAIT  = "AWAIT"
	IF     = "IF"
	ELSE   = "ELSE"
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
	// TODO: Add position info for better error reporting
	Line    int
	Col     int
}
