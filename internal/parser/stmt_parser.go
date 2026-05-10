package parser

import (
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/token"
)

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Pos_: p.curPos()}

	// curToken is '{', move past it
	p.nextToken()
	p.skipNewlines()

	for p.curToken.Type != token.RBRACE && p.curToken.Type != token.EOF {
		stmt := p.parseStmt()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}

		// This is the tricky part. If parseStmt() ended on a NEWLINE
		// because of expectStatementEnd, we are already ready to check
		// the next token or the RBRACE.
		if p.peekToken.Type == token.RBRACE {
			p.nextToken() // Move to RBRACE to trigger loop exit
			break
		}

		p.nextToken()
		p.skipNewlines()
	}

	if p.curToken.Type == token.RBRACE {
		block.End_ = p.curPos()
		// p.nextToken() // consume '}'
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

	// We expect := for declarations. (We will handle standard = for updates later)
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
	}
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	pos := p.curPos()

	p.nextToken() // skip 'return'

	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.ReturnStmt{
		Value: value,
		Pos_:  pos,
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
	}
}

func (p *Parser) parseExpressionStmt() ast.Stmt {
	pos := p.curPos()

	expr := p.parseExpression(LOWEST)
	if expr == nil {
		return nil
	}

	// Handle the case where an expression is followed by an assignment, e.g., 'x = 5'
	if p.peekToken.Type == token.ASSIGN {
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
		}
	}

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.ExprStmt{
		Value: expr,
		Pos_:  pos,
	}
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	pos := p.curPos()
	// p.nextToken() // skip 'for'

	// Infinite loop `for { ... }`
	if p.curToken.Type == token.LBRACE {
		return &ast.ForStmt{Body: p.parseBlockStmt(), Pos_: pos}
	}

	// Expect the opening parenthesis
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	p.nextToken() // get inside the parens

	// Parse the first piece (either Init or Condition)
	first := p.parseStmt()

	var condition ast.Expr
	var post ast.Stmt
	var init ast.Stmt

	// 4. Was that a Condition or an Init?
	if p.peekToken.Type == token.RPAREN {
		// It was a While loop! e.g. for (x < 10)
		condition = first.(*ast.ExprStmt).Value
		p.nextToken() // step on the RPAREN to prepare for parsing the body
	} else {
		// It was a C-style loop! e.g. for (mut i := 0; i < 10; i = i + 1)
		init = first

		// p.curToken should now be on the condition (because expectStatementEnd consumed the ';')
		condition = p.parseExpression(LOWEST)

		if !p.expectPeek(token.SEMICOLON) {
			return nil
		}
		p.nextToken() // move past ';'

		post = p.parseStmt()
		// parseStmt should leave us on the RPAREN due to expectStatementEnd
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
	}
}
