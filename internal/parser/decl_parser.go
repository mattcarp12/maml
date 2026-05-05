package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/token"
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
	case token.TYPE:
		return p.parseTypeDecl()
	default:
		err := fmt.Sprintf("found %+v. only function declarations are supported at the top level for now", p.curToken)
		p.AddError(err)
		return nil
	}
}

func (p *Parser) parseFnDecl() *ast.FnDecl {
	pos := p.curPos()

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	name := p.curToken.Literal

	// expectPeek moves curToken onto the '('
	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	// parseFnParams assumes curToken is '(' and will return with curToken on ')'
	params := p.parseFnParams()

	// Parse the return type
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
		Params:     params,
		Pos_:       pos,
	}
}

func (p *Parser) parseFnParams() []ast.Param {
	var params []ast.Param

	// Case 1: Empty parameters `()`
	// curToken is '('. If the NEXT token is ')', we are empty.
	if p.peekToken.Type == token.RPAREN {
		p.nextToken() // step onto ')'
		return params
	}

	// Case 2: At least one parameter
	p.nextToken() // step onto the first parameter's name
	params = append(params, p.parseParam())

	// While the NEXT token is a comma...
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // step onto ','
		p.nextToken() // step onto the next parameter's name
		params = append(params, p.parseParam())
	}

	// Ensure we close with ')'
	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return params
}

func (p *Parser) parseParam() ast.Param {
	// We enter with curToken sitting on the parameter Name (e.g., 'x')
	param := ast.Param{
		Name: p.curToken.Literal,
		Pos_: p.curPos(),
	}

	// We expect the very next token to be the Type (e.g., 'int')
	if !p.expectPeek(token.IDENT) {
		return param // Return what we have, the parser will register the error
	}

	// Note: Later, when you add Generic types like Result<A, B>,
	// you will replace this block with a dedicated `p.parseTypeExpr()` function.
	param.Type = &ast.NamedType{
		Name: p.curToken.Literal,
		Pos_: p.curPos(),
	}

	// We return with curToken sitting exactly on the Type identifier.
	return param
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	td := &ast.TypeDecl{
		Pos_: p.curPos(),
	}

	if !p.expectPeek(token.IDENT) {
		return nil
	}

	td.Name = &ast.NamedType{Name: p.curToken.Literal, Pos_: p.curPos()}

	p.nextToken() // step onto '='
	p.nextToken() // step onto next token

	switch p.curToken.Type {
	case token.LBRACE:
		td.Rhs = p.parseProductType()
	default:
		err := fmt.Sprintf("found %+v. only product types are supported for now", p.curToken)
		p.AddError(err)
		return nil
	}
	p.skipNewlines()

	return td
}

func (p *Parser) parseProductType() *ast.ProductType {
	pt := &ast.ProductType{
		Pos_: p.curPos(),
	}

	// Case 1: Empty type members `{}`
	// curToken is '{'. If the NEXT token is '}', we are empty.
	if p.peekToken.Type == token.RBRACE {
		p.nextToken() // step onto '}'
		pt.End_ = p.curPos()
		return pt
	}

	// Case 2: At least one parameter
	p.nextToken() // step onto the first parameter's name
	pt.Fields = append(pt.Fields, p.parseParam())

	// While the NEXT token is a comma...
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // step onto ','
		p.nextToken() // step onto the next parameter's name
		pt.Fields = append(pt.Fields, p.parseParam())
	}
	p.nextToken()
	p.skipNewlines()

	// Ensure we close with '}'
	if p.curToken.Type != token.RBRACE {
		return nil
	}

	return pt
}
