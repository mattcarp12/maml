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
		// Stop collecting new nodes if errors are out of control — the
		// output would be too unreliable to be useful.
		if p.hadTooManyErrors() {
			break
		}

		decl := p.parseDecl()
		if decl != nil && !isNilDecl(decl) {
			program.Decls = append(program.Decls, decl)
		} else {
			// parseDecl returned nil, meaning it hit an error. Advance to
			// the next declaration boundary so we can keep going.
			p.synchronizeToDecl()
			// If synchronizeToDecl landed on a decl keyword, loop back and
			// try to parse it; if it hit EOF, the for-condition exits.
			continue
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
		err := fmt.Sprintf(
			"found %+v. only function declarations are supported at the top level for now",
			p.curToken,
		)
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
	if params == nil && p.curToken.Type != token.RPAREN {
		// parseFnParams already recorded an error; bail so ParseProgram
		// can synchronise to the next declaration.
		return nil
	}

	// // Parse the return type
	// if !p.expectPeek(token.IDENT) {
	// 	return nil
	// }
	// returnType := &ast.NamedType{
	// 	Name: p.curToken.Literal,
	// 	Pos_: p.curPos(),
	// }

	// allow for no return type (i.e., void) by making the return type optional
	var returnType *ast.NamedType
	if p.peekToken.Type == token.IDENT {
		p.nextToken() // step onto the return type
		returnType = &ast.NamedType{
			Name: p.curToken.Literal,
			Pos_: p.curPos(),
		}
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
	param := ast.Param{
		Pos_: p.curPos(),
	}

	// Check for mut or own modifiers
	if p.curToken.Type == token.MUT {
		param.Mut = true
		p.nextToken() // step off 'mut'
	} else if p.curToken.Type == token.OWN { // Assuming token.OWN exists
		param.Own = true
		p.nextToken() // step off 'own'
	}

	// We expect the token to now sit on the parameter Name (e.g., 'x')
	if p.curToken.Type != token.IDENT {
		p.AddError(fmt.Sprintf("expected parameter name, got %s", p.curToken.Type))
		return param
	}
	param.Name = p.curToken.Literal

	// We expect the very next token to be the Type (e.g., 'int')
	if !p.expectPeek(token.IDENT) {
		return param // Return what we have; the parser will register the error
	}

	param.Type = &ast.NamedType{
		Name: p.curToken.Literal,
		Pos_: p.curPos(),
	}

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

	// Explicitly require '=' so a malformed `type Point { ... }` (missing =)
	// gets a clear error instead of silently misparse.
	if !p.expectPeek(token.ASSIGN) {
		return nil
	}

	p.nextToken() // step onto the token after '='

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

	// Case 2: At least one field
	p.nextToken() // step onto the first field's name
	pt.Fields = append(pt.Fields, p.parseParam())

	// While the NEXT token is a comma...
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // step onto ','
		p.nextToken() // step onto the next field's name
		pt.Fields = append(pt.Fields, p.parseParam())
	}
	p.nextToken()
	p.skipNewlines()

	// Ensure we close with '}'
	if p.curToken.Type != token.RBRACE {
		if p.curToken.Type == token.EOF {
			p.AddError("expected '}' to close type definition, got EOF")
		} else {
			p.AddError(fmt.Sprintf(
				"expected '}' to close type definition, got %s at line %d",
				p.curToken.Type, p.curToken.Line,
			))
		}
		return nil
	}

	return pt
}
