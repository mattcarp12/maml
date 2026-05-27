package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
)

func (p *Parser) parseMatchExpression() ast.Expr {
	pos := p.curPos()
	p.nextToken()

	// FIX: Use LOWEST precedence so math/logic operators work,
	// but disable struct literals to protect the '{'
	prevAllow := p.allowStructLiterals
	p.allowStructLiterals = false
	subject := p.parseExpression(LOWEST)
	p.allowStructLiterals = prevAllow

	if subject == nil {
		return nil
	}

	for p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	p.nextToken() // step inside '{'
	p.skipNewlines()

	var arms []ast.MatchArm
	for p.curToken.Type != token.RBRACE && p.curToken.Type != token.EOF {
		arm := p.parseMatchArm()
		if arm == nil {
			p.synchronize()
			p.skipNewlines()
			continue
		}
		arms = append(arms, *arm)
	}

	end := p.curPos()
	if p.curToken.Type != token.RBRACE {
		p.addError("expected '}' to close match expression")
		return nil
	}

	return &ast.MatchExpr{
		Subject: subject,
		Arms:    arms,
		Pos_:    pos,
		End_:    end,
	}
}

func (p *Parser) parseMatchArm() *ast.MatchArm {
	if p.curToken.Type != token.CASE {
		p.addError(fmt.Sprintf("expected 'case' in match arm, got %s", p.curToken.Type))
		return nil
	}
	pos := p.curPos()

	p.nextToken() // step onto the pattern
	pat := p.parsePattern()
	if pat == nil {
		return nil
	}

	if !p.expectPeek(token.COLON) {
		return nil
	}

	for p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	var body *ast.BlockStmt

	if p.peekToken.Type == token.YIELD {
		p.nextToken() // step onto '=>'
		yieldPos := p.curPos()
		p.nextToken() // step onto expression

		expr := p.parseExpression(LOWEST)
		if expr == nil {
			return nil
		}
		p.expectStatementEnd()

		body = &ast.BlockStmt{
			Statements: []ast.Stmt{&ast.YieldStmt{Value: expr, Pos_: yieldPos}},
			Pos_:       yieldPos,
			End_:       p.curPos(),
		}

	} else if p.expectPeek(token.LBRACE) {
		body = p.parseBlockStmt()
		p.nextToken() // NEW: Step past the block's closing '}' so we sit correctly for skipNewlines!
	} else {
		p.addError(fmt.Sprintf("expected '{' or '=>' after match arm colon, got %s", p.peekToken.Type))
		return nil
	}

	p.skipNewlines()

	return &ast.MatchArm{
		Pattern: pat,
		Body:    body,
		Pos_:    pos,
	}
}

func (p *Parser) parsePattern() ast.Pattern {
	pos := p.curPos()

	switch p.curToken.Type {
	case token.IDENT:
		name := p.curToken.Literal

		if name == "_" {
			return &ast.WildcardPattern{Pos_: pos}
		}

		// case Tuple(x, y) — tuple destructuring
		if p.peekToken.Type == token.LPAREN {
			p.nextToken() // onto '('

			var bindings []*ast.Identifier

			// Parse comma-separated bindings if it's not empty ()
			if p.peekToken.Type != token.RPAREN {
				p.nextToken() // onto first binding
				if p.curToken.Type != token.IDENT {
					p.addError(fmt.Sprintf("expected binding name, got %s", p.curToken.Type))
					return nil
				}
				bindings = append(bindings, &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()})

				for p.peekToken.Type == token.COMMA {
					p.nextToken() // onto ','
					p.nextToken() // onto next binding name
					if p.curToken.Type != token.IDENT {
						p.addError(fmt.Sprintf("expected binding name, got %s", p.curToken.Type))
						return nil
					}
					bindings = append(bindings, &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()})
				}
			}

			if !p.expectPeek(token.RPAREN) {
				return nil
			}
			return &ast.VariantPattern{Name: name, TupleBindings: bindings, Pos_: pos}
		}

		// case Circle{radius: r} — field destructuring.
		// Distinguish from arm body '{...}' by checking that inside '{' there's
		// 'IDENT COLON', which is the field-binding syntax.
		if p.peekToken.Type == token.LBRACE && p.peek2Token.Type == token.IDENT {
			// Step inside and verify it really is 'field: binding'
			p.nextToken() // curToken = '{'
			p.nextToken() // curToken = first token inside (the IDENT we peeked)

			if p.peekToken.Type == token.COLON {
				// Confirmed field destructuring — parse all field bindings
				fieldBindings := []ast.VariantPatternField{}
				for {
					if p.curToken.Type != token.IDENT {
						p.addError(fmt.Sprintf("expected field name, got %s", p.curToken.Type))
						return nil
					}
					fieldName := p.curToken.Literal
					if !p.expectPeek(token.COLON) {
						return nil
					}
					if !p.expectPeek(token.IDENT) {
						return nil
					}
					bindingName := &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()}
					fieldBindings = append(fieldBindings, ast.VariantPatternField{
						Field:   fieldName,
						Binding: bindingName,
					})
					if p.peekToken.Type == token.COMMA {
						p.nextToken() // onto ','
						p.nextToken() // onto next field name
					} else {
						break
					}
				}
				if !p.expectPeek(token.RBRACE) {
					return nil
				}
				return &ast.VariantPattern{Name: name, Fields: fieldBindings, Pos_: pos}
			}
		}

		// Unit variant: case Point
		return &ast.VariantPattern{Name: name, TupleBindings: nil, Pos_: pos}
	case token.INT:
		intVal, _ := strconv.ParseInt(p.curToken.Literal, 10, 64)
		lit := &ast.IntLiteral{Value: intVal, Pos_: pos}
		return &ast.LiteralPattern{Value: lit, Pos_: pos}

	case token.BOOL:
		val := p.curToken.Literal == "true"
		lit := &ast.BoolLiteral{Value: val, Pos_: pos}
		return &ast.LiteralPattern{Value: lit, Pos_: pos}

	default:
		p.addError(fmt.Sprintf("unexpected token in pattern: %s", p.curToken.Type))
		return nil
	}
}
