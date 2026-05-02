package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/token"
)

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{
		Pos_:       p.curPos(),
		Statements: []ast.Stmt{},
	}

	p.nextToken() // skip '{'

	for p.curToken.Type != token.RBRACE && p.curToken.Type != token.EOF {
		stmt := p.parseStmt()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.skipNewlines()
		p.nextToken()
	}

	block.End_ = p.curPos() // Capture where the '}' is
	return block
}

func (p *Parser) parseStmt() ast.Stmt {
	p.skipNewlines()
	switch p.curToken.Type {
	case token.IDENT:
		if p.peekToken.Type == token.DECLARE_IMMUTABLE || p.peekToken.Type == token.DECLARE_MUTABLE {
			return p.parseDeclareStmt()
		}
		fallthrough
	case token.YIELD:
		return p.parseReturnStmt()
	default:
		err := fmt.Sprintf("unrecognized statement inside block - %+v", p.curToken)
		p.errors = append(p.errors, err)
		return nil
	}
}

func (p *Parser) parseDeclareStmt() *ast.DeclareStmt {
	pos := p.curPos()
	name := p.curToken.Literal

	p.nextToken() // skip variable name

	if p.curToken.Type != token.DECLARE_IMMUTABLE && p.curToken.Type != token.DECLARE_MUTABLE {
		p.errors = append(p.errors, "expected := or ~= after identifier")
		return nil
	}

	mutable := p.curToken.Type == token.DECLARE_MUTABLE

	p.nextToken() // skip the := or ~= operator

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	return &ast.DeclareStmt{
		Name:    name,
		Mutable: mutable,
		Value:   value,
		Pos_:    pos,
	}
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	pos := p.curPos()

	p.nextToken() // skip '=>'

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	return &ast.ReturnStmt{
		Value: value,
		Pos_:  pos,
	}
}
