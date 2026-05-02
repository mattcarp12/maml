package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/token"
)

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{
		Decls: []ast.Decl{},
	}

	for p.curToken.Type != token.EOF {
		decl := p.parseDecl()
		if decl != nil {
			program.Decls = append(program.Decls, decl)
		}
		p.nextToken()
	}

	return program
}

func (p *Parser) parseDecl() ast.Decl {
	p.skipNewlines()
	if p.curToken.Type == token.EOF {
		return nil
	}
	switch p.curToken.Type {
	case token.FN:
		return p.parseFnDecl()
	default:
		err := fmt.Sprintf("found %+v. only function declarations are supported at the top level for now", p.curToken)
		p.errors = append(p.errors, err)
		return nil
	}
}

func (p *Parser) parseFnDecl() *ast.FnDecl {
	// Capture the start position of the 'fn' keyword
	pos := p.curPos()

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	name := p.curToken.Literal

	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	// TODO: parse parameters
	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	// Parse the return type as a formal TypeExpr
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	returnType := &ast.NamedType{
		Name: p.curToken.Literal,
		Pos_: p.curPos(),
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	body := p.parseBlockStmt()

	return &ast.FnDecl{
		Name:       name,
		ReturnType: returnType,
		Body:       body,
		Pos_:       pos,
	}
}
