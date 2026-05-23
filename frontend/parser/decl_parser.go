package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
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

		// Only advance if we're not already positioned at a decl keyword.
		// parseSumType (and similar) may leave curToken on 'fn'/'type'.
		if p.curToken.Type != token.FN && p.curToken.Type != token.TYPE && p.curToken.Type != token.ASYNC {
			p.nextToken()
		}
	}

	return program
}

func (p *Parser) parseDecl() ast.Decl {
	p.skipNewlines()
	if p.curToken.Type == token.EOF {
		return nil
	}
	switch p.curToken.Type {
	case token.FN, token.ASYNC:
		return p.parseFnDecl()
	case token.TYPE:
		return p.parseTypeDecl()
	default:
		err := fmt.Sprintf(
			"found %+v. only function and type declarations are supported at the top level",
			p.curToken,
		)
		p.addError(err)
		return nil
	}
}

func (p *Parser) parseFnDecl() *ast.FnDecl {
	pos := p.curPos()
	isAsync := false

	// NEW: Check if this is an async function
	if p.curToken.Type == token.ASYNC {
		isAsync = true
		if !p.expectPeek(token.FN) {
			return nil
		}
	}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	name := p.curToken.Literal

	// expectPeek moves curToken onto the '('
	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	params := p.parseFnParams()
	if params == nil && p.curToken.Type != token.RPAREN {
		return nil
	}

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
		IsAsync:    isAsync, // NEW: Apply the flag to the AST node
		Pos_:       pos,
	}
}

func (p *Parser) parseFnParams() []*ast.Param {
	var params []*ast.Param

	success := p.parseCommaSeparatedList(token.RPAREN, func() {
		params = append(params, p.parseParam())
	})

	// If we didn't find the closing ')', bail out so error recovery can take over!
	if !success {
		return nil
	}

	return params
}

func (p *Parser) parseParam() *ast.Param {
	param := &ast.Param{
		Pos_: p.curPos(),
	}

	// Check for mut or own modifiers
	switch p.curToken.Type {
	case token.MUT:
		param.Mut = true
		p.nextToken() // step off 'mut'
	case token.OWN:
		param.Own = true
		p.nextToken() // step off 'own'
	}

	// We expect the token to now sit on the parameter Name (e.g., 'x')
	if p.curToken.Type != token.IDENT {
		p.addError(fmt.Sprintf("expected parameter name, got %s", p.curToken.Type))
		return param
	}
	param.Name = p.curToken.Literal

	// REPLACED: Hand off to parseTypeExpr
	param.Type = p.parseTypeExpr()

	return param
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	td := &ast.TypeDecl{Pos_: p.curPos()}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	td.Name = &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()}

	if !p.expectPeek(token.ASSIGN) {
		return nil
	}

	p.nextToken()
	p.skipNewlines()

	switch p.curToken.Type {
	case token.LBRACE:
		td.Rhs = p.parseProductType()
	case token.SEPARATOR: // '|'
		td.Rhs = p.parseSumType()
	default:
		p.addError(fmt.Sprintf("expected '{' or '|' in type declaration, got %s", p.curToken.Type))
		return nil
	}

	p.skipNewlines()
	return td
}

func (p *Parser) parseSumType() *ast.SumTypeExpr {
	st := &ast.SumTypeExpr{Pos_: p.curPos()}

	for p.curToken.Type == token.SEPARATOR {
		variant := p.parseSumVariant()
		if variant == nil {
			return nil
		}
		st.Variants = append(st.Variants, *variant)
		p.skipNewlines()
	}

	st.End_ = p.curPos()
	return st
}

func (p *Parser) parseSumVariant() *ast.VariantTypeExpr {
	// curToken is '|'

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	name := p.curToken.Literal

	variant := &ast.VariantTypeExpr{Name: name}

	// Optional payload block: { field type, ... }
	if p.peekToken.Type == token.LBRACE {
		p.nextToken() // step onto '{'
		// Reuse parseProductType which handles the { field type, ... } syntax.
		pt := p.parseProductType()
		if pt == nil {
			return nil
		}
		variant.Fields = pt.Fields
	}

	p.nextToken() // step past variant (onto next '|' or end)
	p.skipNewlines()

	return variant
}
func (p *Parser) parseProductType() *ast.StructTypeExpr {
	pt := &ast.StructTypeExpr{
		Pos_: p.curPos(),
	}

	// Case 1: Empty type members `{}`
	// curToken is '{'. If the NEXT token is '}', we are empty.
	if p.peekToken.Type == token.RBRACE {
		p.nextToken() // step onto '}'
		pt.End_ = p.curPos()
		return pt
	}

	// parseField converts a Param (name + type) into a StructTypeField.
	parseField := func() ast.StructTypeField {
		param := p.parseParam()
		return ast.StructTypeField{Name: param.Name, Type: param.Type}
	}

	// Case 2: At least one field
	p.nextToken() // step onto the first field's name
	pt.Fields = append(pt.Fields, parseField())

	// While the NEXT token is a comma...
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // step onto ','
		p.nextToken() // step onto the next field's name
		pt.Fields = append(pt.Fields, parseField())
	}
	p.nextToken()
	p.skipNewlines()

	// Ensure we close with '}'
	if p.curToken.Type != token.RBRACE {
		if p.curToken.Type == token.EOF {
			p.addError("expected '}' to close type definition, got EOF")
		} else {
			p.addError(fmt.Sprintf(
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
			return &ast.SliceTypeExpr{Base: baseType, Pos_: startPos}
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
		return &ast.ArrayTypeExpr{Size: int(size), Base: baseType, Pos_: startPos}
	}

	// Case 2: Standard Named Types like 'int', 'string', 'User'
	if p.peekToken.Type == token.IDENT {
		p.nextToken() // Step onto the identifier
		name := p.curToken.Literal

		// NEW: If followed by '<', it's a compiler-known generic type!
		if p.peekToken.Type == token.LT {
			p.nextToken() // Step onto '<'

			var params []ast.TypeExpr
			// Parse first type argument
			params = append(params, p.parseTypeExpr())

			// Parse subsequent comma-separated arguments (e.g., Result<T, E>)
			for p.peekToken.Type == token.COMMA {
				p.nextToken() // Step onto ','
				params = append(params, p.parseTypeExpr())
			}

			if !p.expectPeek(token.GT) { // Expect '>'
				return nil
			}

			return &ast.GenericType{
				Name:   name,
				Params: params,
				Pos_:   startPos,
			}
		}

		// Standard named type (int, string, etc.)
		return &ast.NamedTypeExpr{
			Name: &ast.Identifier{Value: name},
			Pos_: startPos,
		}
	}

	p.addError(fmt.Sprintf("expected a type, got %s", p.peekToken.Type))
	return nil
}
