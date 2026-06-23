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

	// If followed by '<', it's a generic literal configuration (e.g., Vec<int>{)
	if p.peekToken.Type == token.LT && p.looksLikeGenericInstantiation() {
		p.nextToken() // Step onto '<'
		typeExpr := p.parseGenericTypeExpr(name, startPos)
		wrapper := &ast.TypeExprWrapper{
			Pos_:     startPos,
			TypeExpr: typeExpr,
		}
		wrapper.End_ = p.curEndPos()
		return wrapper
	}

	// If followed by '{', it's a standard named composite type (e.g., User{)
	if p.peekToken.Type == token.LBRACE && p.allowStructLiterals {
		typeExpr := &ast.NamedTypeExpr{
			Name: &ast.Identifier{Value: name, Pos_: startPos, End_: p.curEndPos()},
			Pos_: startPos,
		}
		typeExpr.End_ = p.curEndPos()
		wrapper := &ast.TypeExprWrapper{
			Pos_:     startPos,
			TypeExpr: typeExpr,
		}
		wrapper.End_ = p.curEndPos()
		return wrapper
	}

	// Fallback: Just a standard variable/constant identifier lookup expression
	id := &ast.Identifier{
		Value: name,
		Pos_:  startPos,
	}
	id.End_ = p.curEndPos()
	return id
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
		End_:  ast.Position{Line: pos.Line, Col: pos.Col + len(lit)},
	}
}

func (p *Parser) parseBooleanLiteral() ast.Expr {
	return &ast.BoolLiteral{
		Value: p.curToken.Literal == "true",
		Pos_:  p.curPos(),
		End_:  p.curEndPos(),
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

	expression.End_ = p.curEndPos()
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
	expression.End_ = p.curEndPos()
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

		// Check for "else if" chaining
		if p.peekToken.Type == token.IF {
			p.nextToken() // step onto IF
			innerIf := p.parseIfExpression()
			if innerIf == nil {
				return nil
			}
			// Wrap the inner if inside a block that yields its value
			yieldStmt := &ast.YieldStmt{
				Value: innerIf,
				Pos_:  innerIf.Pos(),
				End_:  innerIf.End(),
			}
			altBlock := &ast.BlockStmt{
				Statements: []ast.Stmt{yieldStmt},
				Pos_:       yieldStmt.Pos(),
				End_:       yieldStmt.End(),
			}
			alternative = altBlock
		} else {
			// Normal else block
			if !p.expectPeek(token.LBRACE) {
				return nil
			}
			alternative = p.parseBlockStmt()
		}
	}

	ifExpr := &ast.IfExpr{
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
		Pos_:        pos,
	}
	ifExpr.End_ = p.curEndPos()
	return ifExpr
}

func (p *Parser) parseCallExpression(function ast.Expr) ast.Expr {
	callExpr := &ast.CallExpr{
		Function: function,
		Pos_:     p.curPos(),
	}

	// Parse the arguments safely
	callExpr.Arguments = p.parseCallArguments(token.RPAREN)

	callExpr.End_ = p.curEndPos()
	return callExpr
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

	// We only intercept 'mut'. We leave 'own' alone so that
	// parseExpression properly builds an ast.OwnExpr!
	if p.curToken.Type == token.MUT {
		arg.Mut = true
		p.nextToken()
	}

	// Parse the actual expression
	arg.Argument = p.parseExpression(LOWEST)
	arg.End_ = p.curEndPos()
	return arg
}

func (p *Parser) parseArrayTypePrefix() ast.Expr {
	typeExpr := p.parseTypeExpr()
	if typeExpr == nil {
		return nil
	}
	wrapper := &ast.TypeExprWrapper{
		TypeExpr: typeExpr,
		Pos_:     typeExpr.Pos(),
	}
	wrapper.End_ = p.curEndPos()
	return wrapper
}

func (p *Parser) parseCompositeLiteral(left ast.Expr) ast.Expr {
	cl := &ast.CompositeLiteral{
		TypeExpr: extractTypeExpr(left),
		Pos_:     left.Pos(),
	}

	success := p.parseCommaSeparatedList(token.RBRACE, func() {
		startPos := p.curPos()
		firstExpr := p.parseExpression(LOWEST)
		if firstExpr == nil {
			return
		}

		var element ast.CompositeElement

		// If it's followed by a colon, it's a keyed field (e.g., Struct or Map)
		if p.peekToken.Type == token.COLON {
			p.nextToken() // step onto ':' and move past it to the value
			p.nextToken() // Now step onto the value expression after the colon

			valueExpr := p.parseExpression(LOWEST)
			if valueExpr == nil {
				return
			}

			element = ast.CompositeElement{
				Pos_:  startPos,
				Key:   firstExpr,
				Value: valueExpr,
				End_:  p.curEndPos(),
			}
		} else {
			// Otherwise, it's a purely positional element (e.g., Array or Vector)
			element = ast.CompositeElement{
				Pos_:  startPos,
				Key:   nil,
				Value: firstExpr,
				End_:  p.curEndPos(),
			}
		}

		cl.Elements = append(cl.Elements, element)
	})

	if !success {
		return nil
	}

	cl.End_ = p.curEndPos()
	return cl
}

// Unwraps the type node from our Pratt helper if necessary
func extractTypeExpr(expr ast.Expr) ast.TypeExpr {
	if wrapper, ok := expr.(*ast.TypeExprWrapper); ok {
		return wrapper.TypeExpr
	}
	// If it's a standard identifier like 'User', treat it as a NamedTypeExpr
	if id, ok := expr.(*ast.Identifier); ok {
		named := &ast.NamedTypeExpr{
			Name: id,
			Pos_: id.Pos(),
		}
		named.End_ = id.End() // reuse identifier's end position
		return named
	}
	// Fallback or pass-through for other type shapes (like GenericTypeExpr)
	if te, ok := expr.(ast.TypeExpr); ok {
		return te
	}
	return nil
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
		End_:  p.curEndPos(),
	}

	fa.End_ = p.curEndPos()
	return fa
}

func (p *Parser) parseStringLiteral() ast.Expr {
	return &ast.StringLiteral{
		Value: p.curToken.Literal,
		Pos_:  p.curPos(),
		End_:  p.curEndPos(),
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

		slice := &ast.SliceExpr{
			Left: left,
			Low:  low,
			High: high,
			Pos_: left.Pos(),
		}
		slice.End_ = p.curEndPos()
		return slice
	}

	// Normal index expression: arr[index]
	if !p.expectPeek(token.RBRACKET) {
		return nil
	}

	indexExpr := &ast.IndexExpr{
		Left:  left,
		Index: low,
		Pos_:  left.Pos(),
	}
	indexExpr.End_ = p.curEndPos()
	return indexExpr
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
		End_:  p.curEndPos(),
	}
}

func (p *Parser) parseSpawnExpression() ast.Expr {
	pos := p.curPos()
	p.nextToken() // step off 'spawn'

	value := p.parseExpression(PREFIX)
	if value == nil {
		return nil
	}

	var call *ast.CallExpr
	if callExpr, ok := value.(*ast.CallExpr); !ok {
		p.addError("can only `spawn` a call expression")
		return nil
	} else {
		call = callExpr
	}

	return &ast.SpawnExpr{
		Value: call,
		Pos_:  pos,
		End_:  p.curEndPos(),
	}
}

func (p *Parser) parseOwnExpression() ast.Expr {
	pos := p.curPos()
	p.nextToken() // step off 'own'

	// Use PREFIX precedence so `own x.y` binds tightly
	value := p.parseExpression(PREFIX)
	if value == nil {
		return nil
	}

	expr := &ast.OwnExpr{
		Value: value,
		Pos_:  pos,
	}
	expr.End_ = p.curEndPos()
	return expr
}

func (p *Parser) parseFreezeExpression() ast.Expr {
	pos := p.curPos()
	// p.nextToken() // step off 'freeze'
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	p.nextToken() // step off '('
	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	expr := &ast.FreezeExpr{
		Value: value,
		Pos_:  pos,
	}
	expr.End_ = p.curEndPos()
	return expr
}
