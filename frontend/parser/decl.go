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
	case token.FN, token.ASYNC, token.EXTERN:
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
	isExtern := false

	// Check for async OR extern modifiers
	switch p.curToken.Type {
	case token.ASYNC:
		isAsync = true
		if !p.expectPeek(token.FN) {
			return nil
		}
	case token.EXTERN:
		isExtern = true
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
		p.nextToken() // step onto the return type
		returnType = p.parseTypeExpr()
	}

	var body *ast.BlockStmt

	// Logic to skip body for extern functions
	if isExtern {
		// Extern functions have no body. We just consume an optional trailing semicolon.
		if p.peekToken.Type == token.SEMICOLON {
			p.nextToken()
		}
	} else {
		// Standard functions MUST have a body
		if !p.expectPeek(token.LBRACE) {
			return nil
		}
		body = p.parseBlockStmt()
	}

	return &ast.FnDecl{
		Name:       name,
		ReturnType: returnType,
		Body:       body,
		Params:     params,
		IsAsync:    isAsync,
		IsExtern:   isExtern,
		Pos_:       pos,
		End_:       p.curEndPos(),
	}
}

func (p *Parser) parseFnParams() []*ast.Param {
	params := []*ast.Param{}

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
		// Assuming your ast.Param struct has an 'Owned' boolean flag like 'Mut'!
		param.Own = true
		p.nextToken() // step off 'own'
	}

	// We expect the token to now sit on the parameter Name (e.g., 'x')
	if p.curToken.Type != token.IDENT {
		p.addError(fmt.Sprintf("expected parameter name, got %s", p.curToken.Type))
		param.End_ = p.curEndPos()
		return param
	}
	param.Name = p.curToken.Literal

	p.nextToken() // step off the parameter name
	param.Type = p.parseTypeExpr()

	param.End_ = p.curEndPos()
	return param
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	td := &ast.TypeDecl{Pos_: p.curPos()}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	id := &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()}
	id.End_ = p.curEndPos()
	td.Name = id

	if !p.expectPeek(token.ASSIGN) {
		return nil
	}

	p.nextToken()
	p.skipNewlines()

	switch p.curToken.Type {
	case token.LBRACE:
		td.Rhs = p.parseProductType()
	case token.SEPARATOR, token.IDENT: // '|'
		td.Rhs = p.parseSumType()
	default:
		p.addError(fmt.Sprintf("expected '{' or '|' in type declaration, got %s", p.curToken.Type))
		return nil
	}

	p.skipNewlines()
	td.End_ = p.curEndPos()
	return td
}

func (p *Parser) parseSumType() *ast.SumTypeExpr {
	st := &ast.SumTypeExpr{Pos_: p.curPos()}
	p.skipNewlines()

	// Handle optional leading '|'
	if p.curToken.Type == token.SEPARATOR {
		p.nextToken()
		p.skipNewlines()
	}

	for {
		if p.curToken.Type != token.IDENT {
			p.addError("expected variant name identifier in sum type declaration")
			return nil
		}

		variant := p.parseSumVariant()
		if variant == nil {
			return nil
		}
		st.Variants = append(st.Variants, *variant)

		// After a variant, we must advance past it.
		// parseSumVariant leaves curToken ON the last token of the variant
		// (the IDENT for unit, ')' for tuple, '}' for struct).
		// We need to move forward to see what comes next.
		p.nextToken() // ← THIS is the missing step
		p.skipNewlines()

		if p.curToken.Type == token.SEPARATOR {
			p.nextToken()
			p.skipNewlines()
		} else {
			break
		}
	}

	st.End_ = p.curEndPos()
	return st
}

func (p *Parser) parseSumVariant() *ast.VariantTypeExpr {
	if p.curToken.Type != token.IDENT {
		p.addError(fmt.Sprintf("expected variant name identifier, got %s", p.curToken.Type))
		return nil
	}

	name := p.curToken.Literal
	startPos := p.curPos()

	variant := &ast.VariantTypeExpr{
		Name:        name,
		TupleFields: make([]ast.TypeExpr, 0),
		Pos_:        startPos,
	}

	// Case 1: Struct Variant -> Enum{x string}
	if p.peekToken.Type == token.LBRACE {
		p.nextToken() // step onto '{'
		pt := p.parseProductType()
		if pt == nil {
			return nil
		}
		variant.Fields = pt.Fields
		// Assumes parseProductType ends with curToken on '}'

		// Case 2: Tuple Variant -> Enum(int, string)
	} else if p.peekToken.Type == token.LPAREN {
		p.nextToken() // step onto '('

		// If it's not an empty tuple '()', parse the types
		if p.peekToken.Type != token.RPAREN {
			for {
				p.nextToken() // Step onto the start of the TypeExpr
				typeExpr := p.parseTypeExpr()
				if typeExpr == nil {
					return nil
				}
				variant.TupleFields = append(variant.TupleFields, typeExpr)

				// If the next token is a comma, consume it and keep going
				if p.peekToken.Type == token.COMMA {
					p.nextToken() // step onto ','

					// Allow optional trailing comma: if ')' is after the comma, stop
					if p.peekToken.Type == token.RPAREN {
						break
					}
				} else {
					break
				}
			}
		}

		// Ensure the parenthetical parameter wrapper closes cleanly
		if !p.expectPeek(token.RPAREN) { // Moves curToken to ')'
			return nil
		}
	}

	// Case 3: Unit Variant -> Enum (No payload)
	// If neither block runs, curToken remains safely on the variant IDENT.

	variant.End_ = p.curEndPos()
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
		pt.End_ = p.curEndPos()
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

	pt.End_ = p.curEndPos()
	return pt
}

func (p *Parser) parseTypeExpr() ast.TypeExpr {
	startPos := p.curPos()

	// Case 1: Fixed-size array types starting with '[' (e.g., [5]int)
	if p.curToken.Type == token.LBRACKET {
		if !p.expectPeek(token.INT) {
			return nil
		}
		size, _ := strconv.ParseInt(p.curToken.Literal, 10, 64)

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}

		p.nextToken()                 // Step onto the first token of the base type
		baseType := p.parseTypeExpr() // Recursively parse the base type
		if baseType == nil {
			return nil
		}
		array := &ast.ArrayTypeExpr{Size: int(size), Base: baseType, Pos_: startPos}
		array.End_ = p.curEndPos()
		return array
	}

	// Case 2: Standard Named Types like 'int', 'string', 'User'
	if p.curToken.Type == token.IDENT {
		name := p.curToken.Literal

		// If followed by '<', it's a generic type argument block
		if p.peekToken.Type == token.LT {
			p.nextToken() // Step onto '<'
			return p.parseGenericTypeExpr(name, startPos)
		}

		named := &ast.NamedTypeExpr{
			Name: &ast.Identifier{Value: name, Pos_: startPos, End_: p.curEndPos()},
			Pos_: startPos,
		}
		named.End_ = p.curEndPos()
		return named
	}

	p.addError(fmt.Sprintf("expected a type, got %s", p.curToken.Type))
	return nil
}

func (p *Parser) parseGenericTypeExpr(name string, pos ast.Position) *ast.GenericTypeExpr {
	args := []ast.TypeExpr{}

	// We arrive here with curToken sitting on '<'
	p.nextToken() // Step onto the start of the first type argument!

	arg := p.parseTypeExpr()
	if arg == nil {
		return nil
	}
	args = append(args, arg)

	// Loop for remaining comma-separated type arguments
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // Step onto ','
		p.nextToken() // Step onto the start of the NEXT type argument!

		arg := p.parseTypeExpr()
		if arg == nil {
			return nil
		}
		args = append(args, arg)
	}

	// After the loop, the next token must be '>'
	if !p.expectPeek(token.GT) {
		return nil
	}

	generic := &ast.GenericTypeExpr{
		Name: &ast.Identifier{Value: name, Pos_: pos, End_: p.curEndPos()},
		Args: args,
		Pos_: pos,
	}
	generic.End_ = p.curEndPos()
	return generic
}
