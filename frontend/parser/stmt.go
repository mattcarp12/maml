package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
)

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Pos_: p.curPos()}

	// curToken is '{', move past it
	p.nextToken()
	p.skipNewlines()

	for p.curToken.Type != token.RBRACE && p.curToken.Type != token.EOF {
		// Stop collecting if the error cap was exceeded.
		if p.hadTooManyErrors() {
			break
		}

		errorsBefore := len(p.parseErrors)
		stmt := p.parseStmt()

		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		} else if len(p.parseErrors) > errorsBefore {
			// parseStmt recorded at least one error and returned nil.
			// Synchronise to the next statement boundary and keep going
			// so we can report further errors in this block.
			p.synchronize()
			p.skipNewlines()
			continue
		}

		// Happy path: check if the next thing closes the block.
		if p.peekToken.Type == token.RBRACE {
			p.nextToken() // move onto '}' to trigger loop exit
			break
		}

		p.nextToken()
		p.skipNewlines()
	}

	switch p.curToken.Type {
	case token.RBRACE:
		block.End_ = p.curEndPos()
	default:
		p.addError(fmt.Sprintf("expected '}' to close block, got %s", p.curToken.Type))
		block.End_ = p.curEndPos()
	}

	return block
}

func (p *Parser) parseStmt() ast.Stmt {
	switch p.curToken.Type {
	case token.MUT:
		return p.parseDeclareStmt()
	case token.IDENT:
		// Look ahead: if we see ':=', it's a declaration. Otherwise, it's an expression.
		if p.peekToken.Type == token.DECLARE {
			return p.parseDeclareStmt()
		}
		return p.parseExpressionStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.YIELD:
		return p.parseYieldStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.BREAK:
		return p.parseBreakStmt()
	case token.CONTINUE:
		return p.parseContinueStmt()
	default:
		// Any other token (e.g., '1 + 2', 'if true') can be evaluated as an expression statement
		return p.parseExpressionStmt()
	}
}

func (p *Parser) parseDeclareStmt() *ast.DeclareStmt {
	pos := p.curPos()
	mutable := false

	// Check if this is a mutable declaration (starts with 'mut')
	if p.curToken.Type == token.MUT {
		mutable = true
		if !p.expectPeek(token.IDENT) {
			return nil
		}
	}

	name := p.curToken.Literal

	// We expect := for declarations.
	if !p.expectPeek(token.DECLARE) {
		return nil
	}

	p.nextToken() // skip ':='

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.DeclareStmt{
		Name:    name,
		Mutable: mutable,
		Value:   value,
		Pos_:    pos,
		End_:    p.curEndPos(),
	}
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	pos := p.curPos()

	// Check if the NEXT token is a terminator BEFORE stepping off 'return'.
	// This correctly handles bare returns while leaving the token stream perfectly aligned.
	if p.peekToken.Type == token.NEWLINE || p.peekToken.Type == token.SEMICOLON || p.peekToken.Type == token.RBRACE || p.peekToken.Type == token.EOF {
		p.expectStatementEnd() // Consumes NEWLINE/SEMICOLON, or does nothing for RBRACE
		return &ast.ReturnStmt{
			Value: nil,
			Pos_:  pos,
			End_:  p.curEndPos(),
		}
	}

	p.nextToken() // skip 'return'

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	p.expectStatementEnd()

	return &ast.ReturnStmt{
		Value: value,
		Pos_:  pos,
		End_:  p.curEndPos(),
	}
}

func (p *Parser) parseYieldStmt() *ast.YieldStmt {
	pos := p.curPos()

	p.nextToken() // skip '=>'

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.YieldStmt{
		Value: value,
		Pos_:  pos,
		End_:  p.curEndPos(),
	}
}

func (p *Parser) parseExpressionStmt() ast.Stmt {
	pos := p.curPos()

	expr := p.parseExpression(LOWEST)
	if expr == nil {
		return nil
	}

	// Handle the case where an expression is followed by an assignment, e.g., 'x = 5'
	switch p.peekToken.Type {
	case token.ASSIGN:
		p.nextToken() // get onto '='
		p.nextToken() // move to the expression on the right side of '='
		value := p.parseExpression(LOWEST)
		if value == nil {
			return nil
		}
		p.expectStatementEnd() // Consume the newline after the assignment
		return &ast.AssignStmt{
			LValue: expr,
			RValue: value,
			Pos_:   pos,
			End_:   p.curEndPos(),
		}
	case token.PUSH:
		p.nextToken() // get onto '<<'
		p.nextToken() // move to the expression on the right side of '<<'
		value := p.parseExpression(LOWEST)
		if value == nil {
			return nil
		}
		p.expectStatementEnd() // Consume the newline after the assignment
		return &ast.VecPushStmt{
			LValue: expr,
			RValue: value,
			Pos_:   pos,
			End_:   p.curEndPos(),
		}
	}

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.ExprStmt{
		Value: expr,
		Pos_:  pos,
		End_:  p.curEndPos(),
	}
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	pos := p.curPos()

	if p.peekToken.Type == token.LBRACE {
		p.nextToken()
		body := p.parseBlockStmt()
		return &ast.ForStmt{
			Body: body,
			Pos_: pos,
			End_: p.curEndPos(),
		}
	}

	hasParens := p.peekToken.Type == token.LPAREN
	if hasParens {
		p.nextToken()
	}
	p.nextToken()

	// Disable struct literals for the first statement (which might be the while-condition)
	prevAllow := p.allowStructLiterals
	p.allowStructLiterals = false
	first := p.parseStmt()
	p.allowStructLiterals = prevAllow

	var condition ast.Expr
	var post ast.Stmt
	var init ast.Stmt

	if p.curToken.Type == token.SEMICOLON {
		init = first
		p.nextToken()

		// Disable struct literals for the C-style condition
		prevAllow = p.allowStructLiterals
		p.allowStructLiterals = false
		condition = p.parseExpression(LOWEST)
		p.allowStructLiterals = prevAllow

		if !p.expectPeek(token.SEMICOLON) {
			return nil
		}
		p.nextToken() // step off the ';' and onto the start of the post statement

		// FIX: Disable struct literals for the post statement too!
		prevAllow = p.allowStructLiterals
		p.allowStructLiterals = false
		post = p.parseStmt()
		p.allowStructLiterals = prevAllow

		if hasParens {
			if !p.expectPeek(token.RPAREN) {
				return nil
			}
		}

	} else {
		// --- WHILE LOOP ---
		exprStmt, ok := first.(*ast.ExprStmt)
		if !ok {
			p.addError("while-style for loop condition must be an expression")
			return nil
		}
		condition = exprStmt.Value

		if hasParens {
			if !p.expectPeek(token.RPAREN) {
				return nil
			}
		}
	}

	// 5. Expect '{' and parse body
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	body := p.parseBlockStmt()

	return &ast.ForStmt{
		Init:      init,
		Condition: condition,
		Post:      post,
		Body:      body,
		Pos_:      pos,
		End_:      p.curEndPos(),
	}
}

func (p *Parser) parseBreakStmt() ast.Stmt {
	stmt := &ast.BreakStmt{
		Token: p.curToken,
		Pos_:  p.curPos(),
		End_:  p.curEndPos(),
	}
	return stmt
}

func (p *Parser) parseContinueStmt() ast.Stmt {
	stmt := &ast.ContinueStmt{
		Token: p.curToken,
		Pos_:  p.curPos(),
		End_:  p.curEndPos(),
	}
	return stmt
}
