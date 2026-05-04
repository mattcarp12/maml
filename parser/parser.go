package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
	"github.com/mattcarp12/maml/token"
)

const (
	_ int = iota
	LOWEST
	EQUALS      // == or !=
	LESSGREATER // > or < or >= or <=
	SUM         // + or -
	PRODUCT     // * or / or %
	CALL        // Highest precedence!
)

var precedences = map[token.TokenType]int{
	token.EQ:       EQUALS,
	token.NOT_EQ:   EQUALS,
	token.LT:       LESSGREATER,
	token.LTE:      LESSGREATER,
	token.GT:       LESSGREATER,
	token.GTE:      LESSGREATER,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.MULTIPLY: PRODUCT,
	token.DIVIDE:   PRODUCT,
	token.MODULO:   PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACE:   CALL,
	token.DOT:      CALL,
}

type (
	prefixParseFn func() ast.Expr
	infixParseFn  func(ast.Expr) ast.Expr
)

type Parser struct {
	l              *lexer.Lexer
	curToken       token.Token
	peekToken      token.Token
	errors         []string
	prefixParseFns map[token.TokenType]prefixParseFn
	infixParseFns  map[token.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}
	p.setParseFns()
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) setParseFns() {
	p.prefixParseFns = make(map[token.TokenType]prefixParseFn)
	p.prefixParseFns[token.IDENT] = p.parseIdentifier
	p.prefixParseFns[token.INT] = p.parseIntegerLiteral
	p.prefixParseFns[token.BOOL] = p.parseBooleanLiteral
	p.prefixParseFns[token.LPAREN] = p.parseGroupedExpression
	p.prefixParseFns[token.IF] = p.parseIfExpression

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
}

// curPos captures the exact line and column of the token currently being parsed.
func (p *Parser) curPos() ast.Position {
	return ast.Position{Line: p.curToken.Line, Col: p.curToken.Col}
}

func (p *Parser) Errors() []string { return p.errors }

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead at line %d, col %d",
		t, p.peekToken.Type, p.peekToken.Line, p.peekToken.Col)
	p.errors = append(p.errors, msg)
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) skipNewlines() {
	for p.curToken.Type == token.NEWLINE {
		p.nextToken()
	}
}

// expectStatementEnd checks if the statement properly terminates.
// It allows NEWLINE, EOF, or a closing RBRACE (for one-liner blocks).
func (p *Parser) expectStatementEnd() {
	if p.peekToken.Type == token.NEWLINE || p.peekToken.Type == token.EOF || p.peekToken.Type == token.RBRACE {
		if p.peekToken.Type == token.NEWLINE {
			p.nextToken() // Consume the newline
		}
		return
	}
	p.errors = append(p.errors, fmt.Sprintf("expected end of statement (newline), got %s at line %d", p.peekToken.Type, p.peekToken.Line))
}
