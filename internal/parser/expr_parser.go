package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/token"
)

func (p *Parser) parseExpression(precedence int) ast.Expr {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.AddError(fmt.Sprintf("no prefix parse function for %s found at line %d, col %d",
			p.curToken.Type, p.curToken.Line, p.curToken.Col))
		return nil
	}

	leftExp := prefix()
	if leftExp == nil {
		// prefix() already recorded an error; bail so we don't attempt
		// infix parsing on a nil left-hand side.
		return nil
	}

	for p.peekToken.Type != token.EOF && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}

		p.nextToken()
		leftExp = infix(leftExp)

		// If an infix parser failed (e.g. missing operand), stop the loop
		// rather than continuing to apply further infix operators to nil.
		if leftExp == nil {
			return nil
		}
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
	lit := p.curToken.Literal
	value, err := strconv.ParseInt(lit, 0, 64)
	if err != nil {
		p.AddError(fmt.Sprintf("could not parse %q as integer at line %d, col %d",
			lit, pos.Line, pos.Col))
		return nil
	}

	return &ast.IntLiteral{
		Value: value,
		Pos_:  pos,
		// End_ points to the column just past the last digit.
		End_: ast.Position{Line: pos.Line, Col: pos.Col + len(lit)},
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
		Pos_:     p.curPos(),
	}

	precedence := p.curPrecedence()
	p.nextToken()
	expression.Right = p.parseExpression(precedence)

	// If the right-hand side failed to parse, propagate nil so callers know
	// this expression is incomplete rather than silently returning a node
	// with a nil Right field that will panic in later compiler passes.
	if expression.Right == nil {
		return nil
	}

	return expression
}

func (p *Parser) parsePrefixExpression() ast.Expr {
	expression := &ast.PrefixExpr{
		Operator: p.curToken.Literal,
		Pos_:     p.curPos(),
	}

	p.nextToken()
	expression.Right = p.parseExpression(PREFIX)

	if expression.Right == nil {
		return nil
	}

	return expression
}

func (p *Parser) parseGroupedExpression() ast.Expr {
	p.nextToken() // skip the '('

	exp := p.parseExpression(LOWEST)
	if exp == nil {
		return nil
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return exp
}

func (p *Parser) parseIfExpression() ast.Expr {
	pos := p.curPos()

	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	p.nextToken() // step onto the condition expression after '('

	condition := p.parseExpression(LOWEST)
	if condition == nil {
		return nil
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	consequence := p.parseBlockStmt()

	var alternative *ast.BlockStmt

	// After parseBlockStmt, curToken is '}', so peekToken is 'else'
	if p.peekToken.Type == token.ELSE {
		p.nextToken() // move curToken to 'else'

		if !p.expectPeek(token.LBRACE) {
			return nil
		}
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
		Pos_:     p.curPos(), // position of the '('
	}

	callExpr.Arguments = p.parseExpressionList(token.RPAREN)

	// parseExpressionList returns nil only when the closing token was
	// missing entirely (not when the list is merely empty). In that case
	// we still want to return a partial CallExpr so the caller has as much
	// information as possible, but we signal the failure with the nil slice.
	return callExpr
}

// parseExpressionList reads a comma-separated list of expressions until it
// hits `end`. On a missing-close-token error it returns whatever elements
// were successfully parsed (instead of nil) so that callers can still
// inspect the partial result — useful for error recovery and for reporting
// the right number of arguments in a type-checker error message.
func (p *Parser) parseExpressionList(end token.TokenType) []ast.Expr {
	var list []ast.Expr

	// Empty list, e.g. `add()`
	if p.peekToken.Type == end {
		p.nextToken()
		return list
	}

	// At least one element
	p.nextToken()
	if elem := p.parseExpression(LOWEST); elem != nil {
		list = append(list, elem)
	}

	for p.peekToken.Type == token.COMMA {
		p.nextToken() // onto ','
		p.nextToken() // onto next element
		if elem := p.parseExpression(LOWEST); elem != nil {
			list = append(list, elem)
		}
	}

	// Require the closing token. On failure we still return the partial
	// list rather than nil so callers have useful partial information.
	if !p.expectPeek(end) {
		return list
	}

	return list
}

func (p *Parser) parseStructLiteral(left ast.Expr) ast.Expr {
	leftIdent, ok := left.(*ast.Identifier)
	if !ok {
		p.AddError(fmt.Sprintf(
			"expected identifier before '{' in struct literal, got %T at line %d",
			left, p.curToken.Line,
		))
		return nil
	}

	sl := &ast.StructLiteral{
		Type: leftIdent,
		Pos_: leftIdent.Pos(),
	}

	// Empty struct: Point{}
	if p.peekToken.Type == token.RBRACE {
		p.nextToken()
		sl.End_ = p.curPos()
		return sl
	}

	p.nextToken() // step onto first field name

	field := p.parseStructField()
	if field == nil {
		return nil
	}
	sl.Fields = append(sl.Fields, *field)

	for p.peekToken.Type == token.COMMA {
		p.nextToken() // onto ','
		p.nextToken() // onto next field name
		field = p.parseStructField()
		if field == nil {
			return nil
		}
		sl.Fields = append(sl.Fields, *field)
	}

	if !p.expectPeek(token.RBRACE) {
		if p.curToken.Type == token.EOF || p.peekToken.Type == token.EOF {
			p.AddError("expected '}' to close struct literal, got EOF")
		}
		return nil
	}
	sl.End_ = p.curPos()
	return sl
}

func (p *Parser) parseStructField() *ast.StructField {
	sf := &ast.StructField{
		Name: &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()},
	}

	// Previously this called both expectPeek (which adds a "expected next
	// token" error via peekError) AND AddError with a duplicate message.
	// Now we only call expectPeek; its peekError message is sufficient and
	// already includes line/col information.
	if !p.expectPeek(token.COLON) {
		return nil
	}
	p.nextToken() // step onto the value expression

	valExpr := p.parseExpression(LOWEST)
	if valExpr == nil {
		// parseExpression already recorded the specific error; no need to
		// add a redundant "unable to parse struct field value expression".
		return nil
	}

	sf.Value = valExpr
	return sf
}

func (p *Parser) parseFieldAccess(left ast.Expr) ast.Expr {
	fa := &ast.FieldAccess{
		Object: left,
		Pos_:   p.curPos(),
	}

	// expectPeek already calls peekError which includes a descriptive
	// message; the redundant AddError("expected identifier") was removed.
	if !p.expectPeek(token.IDENT) {
		return nil
	}

	fa.Field = &ast.Identifier{
		Value: p.curToken.Literal,
		Pos_:  p.curPos(),
	}

	return fa
}

func (p *Parser) parseStringLiteral() ast.Expr {
	return &ast.StringLiteral{Value: p.curToken.Literal, Pos_: p.curPos()}
}

func (p *Parser) parseArrayLiteral() ast.Expr {
	start := p.curPos() // position of the '['
	elems := p.parseExpressionList(token.RBRACKET)

	// Always return a node — even on a parse error the partial element list
	// is more useful to callers than nil.  The missing ']' error was already
	// recorded by parseExpressionList → expectPeek.
	return &ast.ArrayLiteral{
		Elements: elems,
		Pos_:     start,
		End_:     p.curPos(),
	}
}

func (p *Parser) parseIndexExpression(left ast.Expr) ast.Expr {
	p.nextToken() // step inside '['

	var low ast.Expr
	if p.curToken.Type != token.COLON {
		low = p.parseExpression(LOWEST)
		// If we expected an index but got nothing, bail immediately.
		if low == nil && p.curToken.Type != token.COLON {
			return nil
		}
	}

	// Slice expression: arr[low:high], arr[:high], arr[low:], arr[:]
	if p.peekToken.Type == token.COLON || p.curToken.Type == token.COLON {
		if p.peekToken.Type == token.COLON {
			p.nextToken() // land on ':'
		}
		// curToken is now ':'

		var high ast.Expr
		if p.peekToken.Type != token.RBRACKET {
			p.nextToken() // step off ':' onto upper-bound expression
			high = p.parseExpression(LOWEST)
		}

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}

		return &ast.SliceExpr{
			Left: left,
			Low:  low,
			High: high,
			Pos_: left.Pos(),
		}
	}

	// Normal index expression: arr[index]
	if !p.expectPeek(token.RBRACKET) {
		return nil
	}

	return &ast.IndexExpr{
		Left:  left,
		Index: low,
		Pos_:  left.Pos(),
	}
}
