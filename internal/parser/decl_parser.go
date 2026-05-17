package parser

import (
	"fmt"
	"strconv"

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
		return nil
	}

	// REPLACED: allow for no return type, or use parseTypeExpr
	// Since parseFnParams leaves us sitting on ')', we look ahead to see
	// if the NEXT token starts a type.
	var returnType ast.TypeExpr
	if p.peekToken.Type == token.IDENT || p.peekToken.Type == token.LBRACKET {
		returnType = p.parseTypeExpr()
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
	} else if p.curToken.Type == token.OWN {
		param.Own = true
		p.nextToken() // step off 'own'
	}

	// We expect the token to now sit on the parameter Name (e.g., 'x')
	if p.curToken.Type != token.IDENT {
		p.AddError(fmt.Sprintf("expected parameter name, got %s", p.curToken.Type))
		return param
	}
	param.Name = p.curToken.Literal

	// REPLACED: Hand off to parseTypeExpr
	param.Type = p.parseTypeExpr()

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

// parseTypeExpr parses a type signature (e.g., int, []int, [5]int)
func (p *Parser) parseTypeExpr() ast.TypeExpr {
	startPos := p.curPos()

	// Case 1: Slice or Array types starting with '['
	if p.peekToken.Type == token.LBRACKET {
		p.nextToken() // Step onto '['

		// Is it a slice? `[]T`
		if p.peekToken.Type == token.RBRACKET {
			p.nextToken()                 // Step onto ']'
			baseType := p.parseTypeExpr() // Recursively parse the base type
			return &ast.SliceType{Base: baseType, Pos_: startPos}
		}

		// Or is it a fixed-size array? `[5]T`
		if !p.expectPeek(token.INT) {
			return nil // expectPeek will automatically log an error
		}

		size, _ := strconv.ParseInt(p.curToken.Literal, 10, 64)

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}

		baseType := p.parseTypeExpr() // Recursively parse the base type
		return &ast.ArrayType{Size: size, Base: baseType, Pos_: startPos}
	}

	// Case 2: Standard Named Types like 'int', 'string', 'User'
	if p.peekToken.Type == token.IDENT {
		p.nextToken() // Step onto the identifier
		return &ast.NamedType{
			Name: p.curToken.Literal,
			Pos_: startPos,
		}
	}

	p.AddError(fmt.Sprintf("expected a type, got %s", p.peekToken.Type))
	return nil
}
