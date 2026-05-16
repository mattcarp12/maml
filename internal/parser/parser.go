package parser

import (
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/token"
)

type (
	prefixParseFn func() ast.Expr
	infixParseFn  func(ast.Expr) ast.Expr
)

// defaultMaxErrors is the maximum number of errors the parser will collect
// before it stops recording new ones. This prevents cascading / duplicate
// errors from flooding the output after a single bad construct.
const defaultMaxErrors = 25

type Parser struct {
	l              *lexer.Lexer
	curToken       token.Token
	peekToken      token.Token
	parseErrors    []ParseError // replaces the old []string errors field
	maxErrors      int
	prefixParseFns map[token.TokenType]prefixParseFn
	infixParseFns  map[token.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:           l,
		parseErrors: []ParseError{},
		maxErrors:   defaultMaxErrors,
	}
	p.setParseFns()
	p.nextToken()
	p.nextToken()
	return p
}

// NewWithMaxErrors creates a Parser with a custom error cap. Useful in tests
// that deliberately produce many errors and want to verify exact counts.
func NewWithMaxErrors(l *lexer.Lexer, max int) *Parser {
	p := New(l)
	p.maxErrors = max
	return p
}

func (p *Parser) setParseFns() {
	p.prefixParseFns = make(map[token.TokenType]prefixParseFn)
	p.prefixParseFns[token.IDENT] = p.parseIdentifier
	p.prefixParseFns[token.INT] = p.parseIntegerLiteral
	p.prefixParseFns[token.BOOL] = p.parseBooleanLiteral
	p.prefixParseFns[token.LPAREN] = p.parseGroupedExpression
	p.prefixParseFns[token.IF] = p.parseIfExpression
	p.prefixParseFns[token.STRING] = p.parseStringLiteral
	p.prefixParseFns[token.NOT] = p.parsePrefixExpression
	p.prefixParseFns[token.MINUS] = p.parsePrefixExpression // unary minus (was missing)
	p.prefixParseFns[token.LBRACKET] = p.parseArrayLiteral

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
	p.infixParseFns[token.PLUS] = p.parseInfixExpression
	p.infixParseFns[token.MINUS] = p.parseInfixExpression
	p.infixParseFns[token.EQ] = p.parseInfixExpression
	p.infixParseFns[token.NOT_EQ] = p.parseInfixExpression
	p.infixParseFns[token.LT] = p.parseInfixExpression
	p.infixParseFns[token.LTE] = p.parseInfixExpression
	p.infixParseFns[token.GT] = p.parseInfixExpression
	p.infixParseFns[token.GTE] = p.parseInfixExpression
	p.infixParseFns[token.MULTIPLY] = p.parseInfixExpression
	p.infixParseFns[token.DIVIDE] = p.parseInfixExpression
	p.infixParseFns[token.MODULO] = p.parseInfixExpression
	p.infixParseFns[token.LPAREN] = p.parseCallExpression
	p.infixParseFns[token.LBRACE] = p.parseStructLiteral
	p.infixParseFns[token.DOT] = p.parseFieldAccess
	p.infixParseFns[token.AND] = p.parseInfixExpression
	p.infixParseFns[token.OR] = p.parseInfixExpression
	p.infixParseFns[token.LBRACKET] = p.parseIndexExpression
}
