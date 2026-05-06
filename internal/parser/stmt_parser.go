package parser

import (
	"fmt"

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
		p.nextToken() // consume '}'
	}

	return block
}

func (p *Parser) parseStmt() ast.Stmt {
	switch p.curToken.Type {
	case token.MUT, token.IDENT:
		return p.parseDeclareStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.YIELD:
		return p.parseYieldStmt()
	default:
		p.AddError(fmt.Sprintf("unrecognized statement inside block: %s", p.curToken.Literal))
		return nil
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
