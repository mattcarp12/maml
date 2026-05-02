// parser.go defines the Parser struct and its methods for parsing MAML source code into an AST.

package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
)

type Parser struct {
	l              *lexer.Lexer
	curToken       lexer.Token
	peekToken      lexer.Token
	errors         []string
	prefixParseFns map[lexer.TokenType]prefixParseFn
	infixParseFns  map[lexer.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}

	// Register Prefix Functions (Things that stand alone or start an expression)
	p.prefixParseFns = make(map[lexer.TokenType]prefixParseFn)
	p.prefixParseFns[lexer.IDENT] = p.parseIdentifier
	p.prefixParseFns[lexer.INT] = p.parseIntegerLiteral

	// Register Infix Functions (Things that sit between two expressions)
	p.infixParseFns = make(map[lexer.TokenType]infixParseFn)
	p.infixParseFns[lexer.PLUS] = p.parseInfixExpression

	p.nextToken()
	p.nextToken()
	return p
}

const (
	_ int = iota
	LOWEST
	SUM     // + or -
	PRODUCT // * or /
)

// Precedence map tells the parser how tightly operators bind
var precedences = map[lexer.TokenType]int{
	lexer.PLUS:  SUM,
	lexer.MINUS: SUM,
	// ... add multiply/divide later
}

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead at line %d",
		t, p.peekToken.Type, p.peekToken.Line)
	p.errors = append(p.errors, msg)
}

// expectPeek checks the next token. If it matches, it advances the lexer and returns true.
// If it fails, it records an error and returns false.
func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}

	for p.curToken.Type != lexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case lexer.FN:
		return p.parseFunctionDecl()
	case lexer.IDENT:
		return p.parseDeclareStatement()
	case lexer.YIELD:
		return p.parseReturnStatement()
	default:
		return nil
	}
}

func (p *Parser) parseFunctionDecl() *ast.FunctionDecl {
	// We enter this function with curToken == lexer.FN
	funcDecl := &ast.FunctionDecl{}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	funcDecl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	// TODO - parse parameters

	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	funcDecl.ReturnType = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	funcDecl.Body = p.parseBlockStatement()
	return funcDecl
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{}
	block.Statements = []ast.Statement{}

	p.nextToken() // skip '{'

	for p.curToken.Type != lexer.RBRACE && p.curToken.Type != lexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	return block
}

func (p *Parser) parseDeclareStatement() *ast.DeclareStatement {
	declare := &ast.DeclareStatement{Name: p.curToken.Literal}

	p.nextToken() // skip variable name

	if p.curToken.Type != lexer.DECLARE_IMMUTABLE && p.curToken.Type != lexer.DECLARE_MUTABLE {
		return nil // TODO: handle error
	}

	declare.Mutable = p.curToken.Type == lexer.DECLARE_MUTABLE

	p.nextToken()

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil // TODO: handle error
	}

	declare.Value = value

	return declare
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	returnStmt := &ast.ReturnStatement{}

	p.nextToken() // skip 'yield'

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil // TODO: handle error
	}

	returnStmt.Value = value

	return returnStmt
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.errors = append(p.errors, fmt.Sprintf("no prefix parse function for %s found", p.curToken.Type))
		return nil
	}

	leftExp := prefix()

	// While the next token isn't a semicolon/newline/EOF AND it has higher precedence
	for p.peekToken.Type != lexer.EOF && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}

		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

// Helper to check precedence of the upcoming token
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

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Value: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntLiteral{}
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as integer", p.curToken.Literal)
		p.errors = append(p.errors, msg)
		return nil
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	expression := &ast.InfixExpression{
		Left:     left,
		Operator: p.curToken.Literal,
	}

	precedence := p.curPrecedence() 
	p.nextToken()
	expression.Right = p.parseExpression(precedence)

	return expression
}

