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

	// Consume the newline terminating this statement
	p.expectStatementEnd()

	return &ast.ExprStmt{ // Use ast.ExpressionStmt{} if that is your AST node's name
		Value: expr, // Or Expression: expr
		Pos_:  pos,
	}
}
