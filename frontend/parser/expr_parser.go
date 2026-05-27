package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
)

func (p *Parser) parseExpression(precedence int) ast.Expr {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.addError(fmt.Sprintf("no prefix parse function for %s found at line %d, col %d",
			p.curToken.Type, p.curToken.Line, p.curToken.Col))
		return nil
	}

	leftExp := prefix()
	if leftExp == nil {
		return nil
	}

	for p.peekToken.Type != token.EOF && precedence < p.peekPrecedence() {
		// If we see a '{' but struct literals are forbidden in this context,
		// break the loop so the '{' can be consumed as a block opener instead.
		if p.peekToken.Type == token.LBRACE && !p.allowStructLiterals {
			break
		}

		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}

		p.nextToken()
		leftExp = infix(leftExp)

		if leftExp == nil {
			return nil
		}
	}

	return leftExp
}

func (p *Parser) parseIdentifier() ast.Expr {
	name := p.curToken.Literal
	startPos := p.curPos()

	if p.peekToken.Type == token.LT {
		// Only intercept if it is a compiler-known intrinsic!
		switch name {
		case "Map", "Vec", "Option", "Result":
			p.nextToken() // Step onto '<'

			switch name {
			case "Map":
				keyType := p.parseTypeExpr()
				if !p.expectPeek(token.COMMA) {
					return nil
				}
				valType := p.parseTypeExpr()
				if !p.expectPeek(token.GT) {
					return nil
				}
				return &ast.MapTypeExpr{Key: keyType, Value: valType, Pos_: startPos}

			case "Vec", "Option":
				baseType := p.parseTypeExpr()
				if !p.expectPeek(token.GT) {
					return nil
				}
				if name == "Vec" {
					return &ast.VectorTypeExpr{Base: baseType, Pos_: startPos}
				}
				return &ast.OptionTypeExpr{Base: baseType, Pos_: startPos}

			case "Result":
				okType := p.parseTypeExpr()
				if !p.expectPeek(token.COMMA) {
					return nil
				}
				errType := p.parseTypeExpr()
				if !p.expectPeek(token.GT) {
					return nil
				}
				return &ast.ResultTypeExpr{Ok: okType, Err: errType, Pos_: startPos}
			}
		default:
			// Do nothing! Let it fall through so `x < 10` evaluates correctly as an InfixExpr.
		}
	}

	// Unit variant constructors: None
	if name == "None" && p.peekToken.Type != token.LPAREN {
		return &ast.CallExpr{
			Function:  &ast.Identifier{Value: "None", Pos_: startPos},
			Arguments: nil,
			Pos_:      startPos,
		}
	}

	return &ast.Identifier{Value: name, Pos_: startPos}
}

func (p *Parser) parseIntegerLiteral() ast.Expr {
	pos := p.curPos()
	lit := p.curToken.Literal
	value, err := strconv.ParseInt(lit, 0, 64)
	if err != nil {
		p.addError(fmt.Sprintf("could not parse %q as integer at line %d, col %d",
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

	hasParens := p.peekToken.Type == token.LPAREN
	if hasParens {
		p.nextToken()
	}
	p.nextToken()

	// Disable struct literals while parsing the condition
	prevAllow := p.allowStructLiterals
	p.allowStructLiterals = false
	condition := p.parseExpression(LOWEST)
	p.allowStructLiterals = prevAllow

	if condition == nil {
		return nil
	}

	if hasParens {
		if !p.expectPeek(token.RPAREN) {
			return nil
		}
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	consequence := p.parseBlockStmt()

	var alternative *ast.BlockStmt

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
	pos := p.curPos() // Position of the '('

	// Intercept method calls: a.b() evaluates as FieldAccess(a, b) -> Call()
	if fa, ok := function.(*ast.FieldAccess); ok {
		methodCall := &ast.MethodCallExpr{
			Object: fa.Object, // The receiver (e.g., `a` or `a[10]`)
			Method: fa.Field,  // The method name (e.g., `b`)
			Pos_:   pos,
		}

		// Parse the arguments safely with your newly updated empty-slice logic
		methodCall.Arguments = p.parseCallArguments(token.RPAREN)

		return methodCall
	}

	// Standard fallback for regular function calls: foo()
	callExpr := &ast.CallExpr{
		Function: function,
		Pos_:     pos,
	}

	// Parse the arguments safely
	callExpr.Arguments = p.parseCallArguments(token.RPAREN)

	return callExpr
}

// parseExpressionList reads a comma-separated list of generic expressions.
func (p *Parser) parseExpressionList(end token.TokenType) []ast.Expr {
	list := []ast.Expr{}

	p.parseCommaSeparatedList(end, func() {
		if elem := p.parseExpression(LOWEST); elem != nil {
			list = append(list, elem)
		}
	})

	return list
}

// parseCallArguments reads a comma-separated list of function arguments,
// preserving 'mut' and 'own' modifiers.
func (p *Parser) parseCallArguments(end token.TokenType) []ast.CallArg {
	args := []ast.CallArg{}

	p.parseCommaSeparatedList(end, func() {
		args = append(args, p.parseCallArg())
	})

	return args
}

// Add this function to parse individual arguments with mut/own logic
func (p *Parser) parseCallArg() ast.CallArg {
	arg := ast.CallArg{
		Pos_: p.curPos(),
	}

	switch p.curToken.Type {
	case token.MUT:
		arg.Mut = true
		p.nextToken()
	case token.OWN:
		arg.Own = true
		p.nextToken()
	}

	// Parse the actual expression
	arg.Argument = p.parseExpression(LOWEST)
	return arg
}

func (p *Parser) parseStructLiteral(left ast.Expr) ast.Expr {
	// Validate that the left side is a valid type for instantiation
	switch left.(type) {
	case *ast.Identifier, *ast.MapTypeExpr, *ast.VectorTypeExpr:
		// Valid types for struct/map literals!
	default:
		p.addError(fmt.Sprintf(
			"expected identifier, Map, or Vec before '{' in literal, got %T at line %d",
			left, p.curToken.Line,
		))
		return nil
	}

	sl := &ast.StructLiteral{
		Type: left, // Assign the generic Expr directly
		Pos_: left.Pos(),
	}

	success := p.parseCommaSeparatedList(token.RBRACE, func() {
		if field := p.parseStructField(); field != nil {
			sl.Fields = append(sl.Fields, *field)
		}
	})

	if !success {
		return nil
	}

	sl.End_ = p.curPos()
	return sl
}

func (p *Parser) parseStructField() *ast.StructField {
	// 1. Parse the first expression (could be a Key or an unkeyed Value)
	firstExpr := p.parseExpression(LOWEST)
	if firstExpr == nil {
		return nil
	}

	sf := &ast.StructField{Pos_: firstExpr.Pos()}

	// 2. Is there a colon? If so, it's a Key: Value pair! (Structs & Maps)
	if p.peekToken.Type == token.COLON {
		p.nextToken() // step onto ':'
		p.nextToken() // step onto the Value expression

		sf.Key = firstExpr // The first thing we parsed was the key

		valExpr := p.parseExpression(LOWEST)
		if valExpr == nil {
			return nil
		}
		sf.Value = valExpr
		sf.End_ = valExpr.End()

	} else {
		// 3. No colon! It's an unkeyed element! (Vectors)
		sf.Key = nil
		sf.Value = firstExpr
		sf.End_ = firstExpr.End()
	}

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

func (p *Parser) parseAwaitExpression() ast.Expr {
	pos := p.curPos()
	p.nextToken() // step off 'await'

	// Use PREFIX precedence so `await fetch() + 1` parses as `(await fetch()) + 1`
	value := p.parseExpression(PREFIX)
	if value == nil {
		return nil
	}

	return &ast.AwaitExpr{
		Value: value,
		Pos_:  pos,
	}
}
