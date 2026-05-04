package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/token"
)

func (p *Parser) parseExpression(precedence int) ast.Expr {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.errors = append(p.errors, fmt.Sprintf("no prefix parse function for %s found", p.curToken.Type))
		return nil
	}

	leftExp := prefix()

	for p.peekToken.Type != token.EOF && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}

		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

func (p *Parser) parseIdentifier() ast.Expr {
	return &ast.Identifier{
		Value: p.curToken.Literal,
		Pos_:  p.curPos(),
	}
}

func (p *Parser) parseIntegerLiteral() ast.Expr {
	pos := p.curPos()
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.errors = append(p.errors, fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
		return nil
	}

	return &ast.IntLiteral{
		Value: value,
		Pos_:  pos,
		// End_ could be calculated by adding the string length of the token literal
		End_: ast.Position{Line: pos.Line, Col: pos.Col + len(p.curToken.Literal)},
	}
}

func (p *Parser) parseBooleanLiteral() ast.Expr {
	return &ast.BoolLiteral{
		Value: p.curToken.Literal == "true",
		Pos_:  p.curPos(),
	}
}

func (p *Parser) parseInfixExpression(left ast.Expr) ast.Expr {
	expression := &ast.InfixExpr{
		Left:     left,
		Operator: p.curToken.Literal,
	}

	precedence := p.curPrecedence()
	p.nextToken()
	expression.Right = p.parseExpression(precedence)

	return expression
}

func (p *Parser) parseGroupedExpression() ast.Expr {
	p.nextToken() // skip the '('

	// Parse the expression inside the parentheses starting at the lowest precedence
	exp := p.parseExpression(LOWEST)

	// Ensure the expression is properly closed
	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return exp
}

func (p *Parser) parseIfExpression() ast.Expr {
	pos := p.curPos()

	p.nextToken() // skip the 'if' keyword

	// 1. Parse the condition (e.g., x > 5)
	condition := p.parseExpression(LOWEST)
	if condition == nil {
		return nil
	}

	// 2. We expect a block to follow the condition
	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	// 3. Parse the 'true' block
	consequence := p.parseBlockStmt()

	var alternative *ast.BlockStmt

	// 4. Check if there is an 'else' block attached
	if p.peekToken.Type == token.ELSE {
		p.nextToken() // move onto 'else'

		if !p.expectPeek(token.LBRACE) {
			return nil
		}

		// Parse the 'false' block
		alternative = p.parseBlockStmt()
	}

	return &ast.IfExpr{
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
		Pos_:        pos,
	}
}

func (p *Parser) parseCallExpression(function ast.Expr) ast.Expr {
	callExpr := &ast.CallExpr{
		Function: function,
		Pos_:     p.curPos(), // Captures where the '(' is
	}

	callExpr.Arguments = p.parseExpressionList(token.RPAREN)
	return callExpr
}

// parseExpressionList reads a comma-separated list of expressions until it hits the 'end' token.
func (p *Parser) parseExpressionList(end token.TokenType) []ast.Expr {
	var list []ast.Expr

	// Case 1: The list is completely empty (e.g., `add()`)
	if p.peekToken.Type == end {
		p.nextToken()
		return list
	}

	// Case 2: There is at least one argument
	p.nextToken() // step into the first argument
	list = append(list, p.parseExpression(LOWEST))

	// While the next token is a comma, consume it and parse the next argument
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // move current token to the ','
		p.nextToken() // move current token to the next argument
		list = append(list, p.parseExpression(LOWEST))
	}

	// Ensure the list properly closes with the expected end token (e.g., ')')
	if !p.expectPeek(end) {
		return nil
	}

	return list
}

func (p *Parser) parseStructLiteral(left ast.Expr) ast.Expr {
	// verify that 'left' is an identifier
	leftIdent, ok := left.(*ast.Identifier)
	if !ok {
		return nil
	}

	sl := &ast.StructLiteral{
		Type: leftIdent,
		Pos_: p.curPos(),
	}

	// Case 1: No fields specified (empty struct)
	if p.peekToken.Type == token.RBRACE {
		p.nextToken()
		sl.End_ = p.curPos()
		return sl
	}

	// Case 2: There is at least one field
	p.nextToken() // step onto first field identifier

	field := p.parseStructField()
	if field == nil {
		return nil
	}
	sl.Fields = append(sl.Fields, *field)

	// While the next token is a comma, consume it and parse the next argument
	for p.peekToken.Type == token.COMMA {
		p.nextToken() // move current token to the ','
		p.nextToken() // move current token to the next argument
		field := p.parseStructField()
		if field == nil {
			return nil
		}
		sl.Fields = append(sl.Fields, *field)
	}

	if !p.expectPeek(token.RBRACE) {
		return nil
	}

	return sl
}

func (p *Parser) parseStructField() *ast.StructField {
	sf := &ast.StructField{
		Name: &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()},
	}

	if !p.expectPeek(token.COLON) {
		p.errors = append(p.errors, "a ':' is required between the struct field identifier and its value")
		return nil
	}
	p.nextToken() // step onto value expression

	valExpr := p.parseExpression(LOWEST)
	if valExpr == nil {
		p.errors = append(p.errors, "unable to parse struct field value expression")
	}

	sf.Value = valExpr

	return sf
}

func (p *Parser) parseFieldAccess(left ast.Expr) ast.Expr {
	fa := &ast.FieldAccess{
		Object: left,
		Pos_:   p.curPos(),
	}

	if !p.expectPeek(token.IDENT) {
		p.errors = append(p.errors, "expected identifier")
		return nil
	}

	fa.Field = &ast.Identifier{
		Value: p.curToken.Literal,
		Pos_:  p.curPos(),
	}

	return fa
}
