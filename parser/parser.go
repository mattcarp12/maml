package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
)

// Precedence levels
const (
	_ int = iota
	LOWEST
	PIPE        // |>
	EQUALS      // == !=
	LESSGREATER // < > <= >=
	SUM         // + -
	PRODUCT     // * / %
	PREFIX      // - ! ~
	CALL        // func()
	INDEX       // []
)

type Parser struct {
	l         *lexer.Lexer
	curToken  lexer.Token
	peekToken lexer.Token
	errors    []string

	prefixParseFns map[lexer.TokenType]prefixParseFn
	infixParseFns  map[lexer.TokenType]infixParseFn
}

type prefixParseFn func() ast.Expr
type infixParseFn func(ast.Expr) ast.Expr

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:              l,
		errors:         []string{},
		prefixParseFns: make(map[lexer.TokenType]prefixParseFn),
		infixParseFns:  make(map[lexer.TokenType]infixParseFn),
	}
	// p.registerPrefix(lexer.IDENT, p.parseIdentifier)
	// p.registerPrefix(lexer.INT, p.parseIntLiteral)
	p.advance()
	p.advance()
	return p
}

func (p *Parser) advance() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{Declarations: []ast.Declaration{}}

	for p.curToken.Type != lexer.EOF {
		if p.curToken.Type == lexer.NEWLINE {
			p.advance()
			continue
		}

		decl := p.parseDeclaration()
		if decl != nil {
			program.Declarations = append(program.Declarations, decl)
		}

		// Advance to next top-level item, skipping newlines
		for p.curToken.Type == lexer.NEWLINE && p.curToken.Type != lexer.EOF {
			p.advance()
		}
	}

	return program
}

func (p *Parser) parseDeclaration() ast.Declaration {
	switch p.curToken.Type {
	case lexer.FN:
		return p.parseFnDecl()
	case lexer.TYPE:
		return p.parseTypeDecl()
	case lexer.IDENT:
		// Look ahead to see if this is a declaration
		if p.peekToken.Type == lexer.DECLARE_IMMUTABLE {
			return p.parseDeclareStmt(false)
		}
		if p.peekToken.Type == lexer.DECLARE_MUTABLE {
			return p.parseDeclareStmt(true)
		}
		// Otherwise it's illegal at top level for now
		p.errors = append(p.errors, fmt.Sprintf("unexpected identifier '%s' at top level", p.curToken.Literal))
		p.advanceError()
		return nil
	default:
		p.errors = append(p.errors, fmt.Sprintf("unexpected token at top level: %s", p.curToken.Type))
		p.advanceError()
		return nil
	}
}

func (p *Parser) parseDeclareStmt(isMutable bool) ast.Declaration {
	stmt := &ast.DeclareStmt{Name: p.curToken.Literal, Mutable: isMutable}

	p.advance() // consume IDENT
	p.advance() // consume := or ~=

	// Parse the right side of the expression
	stmt.Value = p.parseExpression(LOWEST)

	// Because of ASI, the statement might end with a NEWLINE or an EOF.
	// If there's a NEWLINE, we consume it so the next statement starts fresh.
	if p.peekToken.Type == lexer.NEWLINE {
		p.advance()
	}

	return stmt
}

func (p *Parser) parseFnDecl() *ast.FnDecl {
	// TODO: implement
	return nil
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	// TODO: implement
	return nil
}

func (p *Parser) advanceError() {
	for p.curToken.Type != lexer.NEWLINE && p.curToken.Type != lexer.EOF {
		p.advance()
	}
}

// Add these registrations inside your New(l *lexer.Lexer) function, right before p.advance():

// --- The Pratt Parser Engine ---

func (p *Parser) parseExpression(precedence int) ast.Expr {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.errors = append(p.errors, fmt.Sprintf("no prefix parse function for %s found", p.curToken.Type))
		return nil
	}
	leftExp := prefix()

	// (We will add the infix loop here in the next step when we do math!)

	return leftExp
}

func (p *Parser) parseIdentifier() ast.Expr {
	return &ast.Identifier{Name: p.curToken.Literal}
}

func (p *Parser) parseIntLiteral() ast.Expr {
	lit := &ast.IntLiteral{}
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.errors = append(p.errors, fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
		return nil
	}
	lit.Value = value
	return lit
}
