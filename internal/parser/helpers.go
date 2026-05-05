package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/token"
)

// curPos captures the exact line and column of the token currently being parsed.
func (p *Parser) curPos() ast.Position {
	return ast.Position{Line: p.curToken.Line, Col: p.curToken.Col}
}

func (p *Parser) Errors() []string { return p.errors }

func (p *Parser) AddError(err string) { p.errors = append(p.errors, err) }

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead at line %d, col %d",
		t, p.peekToken.Type, p.peekToken.Line, p.peekToken.Col)
	p.AddError(msg)
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
	p.AddError(fmt.Sprintf("expected end of statement (newline), got %s at line %d", p.peekToken.Type, p.peekToken.Line))
}
