package parser

import (
	"fmt"
	"strconv"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
)

func (p *Parser) parseMatchExpression() ast.Expr {
	pos := p.curPos()
	p.nextToken() // Step past 'match'

	// Protect the match expression's opening brace from being misread
	prevAllow := p.allowStructLiterals
	p.allowStructLiterals = false
	subject := p.parseExpression(LOWEST)
	p.allowStructLiterals = prevAllow

	if subject == nil {
		return nil
	}

	p.skipNewlines()

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	p.nextToken() // Step onto the first token inside '{' (ideally 'case')
	p.skipNewlines()

	var arms []ast.MatchArm
	for p.curToken.Type != token.RBRACE && p.curToken.Type != token.EOF {
		// Cleanly skip any leading layout newlines between match arms
		p.skipNewlines()

		// Double check we haven't hit the closing brace after skipping newlines
		if p.curToken.Type == token.RBRACE {
			break
		}

		arm := p.parseMatchArm()
		if arm == nil {
			p.synchronize()
			p.skipNewlines()
			continue
		}
		arms = append(arms, *arm)
		p.nextToken()
		p.skipNewlines()
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

	p.nextToken() // Step onto the first token of the pattern
	pat := p.parsePattern()
	if pat == nil {
		return nil
	}

	// Consume the required colon ':' separating pattern from arm body
	if !p.expectPeek(token.COLON) {
		return nil
	}
	p.nextToken() // Step onto the token immediately following the colon
	p.skipNewlines()

	var body *ast.BlockStmt

	switch p.curToken.Type {
	case token.YIELD:
		yieldPos := p.curPos()
		p.nextToken()
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
	case token.LBRACE:
		body = p.parseBlockStmt()
	default:
		p.addError(fmt.Sprintf("expected '{' or '=>' after match arm colon, got %s", p.curToken.Type))
		return nil
	}

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

		// Catch wildcards immediately
		if name == "_" {
			return &ast.WildcardPattern{Pos_: pos}
		}

		// Create a clean NamedTypeExpr for the pattern's type head
		typeExpr := &ast.NamedTypeExpr{
			Name: &ast.Identifier{Value: name, Pos_: pos},
			Pos_: pos,
		}

		// Case A: Tuple Variant Pattern -> Circle(x, y)
		if p.peekToken.Type == token.LPAREN {
			p.nextToken() // step onto '('
			cp := &ast.CompositePattern{TypeExpr: typeExpr, Pos_: pos}

			if p.peekToken.Type != token.RPAREN {
				for {
					p.nextToken() // step onto the binding identifier
					startPos := p.curPos()

					// Recursively parse the pattern (allows nested variant matching!)
					innerPat := p.parsePattern()
					if innerPat == nil {
						return nil
					}

					cp.Elements = append(cp.Elements, ast.CompositePatternElement{
						Pos_:    startPos,
						Key:     nil, // Positional fields have no key name
						Pattern: innerPat,
						End_:    p.curPos(),
					})

					if p.peekToken.Type == token.COMMA {
						p.nextToken() // step onto ','
						if p.peekToken.Type == token.RPAREN {
							break // Handle trailing comma safely
						}
					} else {
						break
					}
				}
			}

			if p.curToken.Type != token.RPAREN && !p.expectPeek(token.RPAREN) {
				return nil
			}
			cp.End_ = p.curPos()
			return cp
		}

		// Case B: Struct Variant Pattern -> Circle{radius: r}
		if p.peekToken.Type == token.LBRACE {
			p.nextToken() // step onto '{'
			cp := &ast.CompositePattern{TypeExpr: typeExpr, Pos_: pos}

			if p.peekToken.Type != token.RBRACE {
				for {
					if !p.expectPeek(token.IDENT) {
						return nil
					}
					keyIdent := &ast.Identifier{Value: p.curToken.Literal, Pos_: p.curPos()}

					if !p.expectPeek(token.COLON) {
						return nil
					}
					p.nextToken() // step past ':' onto the binding/sub-pattern
					startPos := p.curPos()

					innerPat := p.parsePattern()
					if innerPat == nil {
						return nil
					}

					cp.Elements = append(cp.Elements, ast.CompositePatternElement{
						Pos_:    startPos,
						Key:     keyIdent,
						Pattern: innerPat,
						End_:    p.curPos(),
					})

					if p.peekToken.Type == token.COMMA {
						p.nextToken() // step onto ','
						if p.peekToken.Type == token.RBRACE {
							break // Handle trailing comma safely
						}
					} else {
						break
					}
				}
			}

			if !p.expectPeek(token.RBRACE) {
				return nil
			}
			cp.End_ = p.curPos()
			return cp
		}

		// Case C: Unit Variant Pattern or plain variable identifier binding capture
		// Returns an empty CompositePattern representing an instantiation with 0 elements
		return &ast.IdentifierPattern{Name: name, Pos_: pos}

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
