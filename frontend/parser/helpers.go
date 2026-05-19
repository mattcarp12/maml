package parser

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/token"
)

// ParseError holds a human-readable message plus the source position where
// the error was detected. Storing position here means downstream tools
// (IDEs, error formatters) don't have to re-parse the message string.
type ParseError struct {
	Msg  string
	Line int
	Col  int
}

func (e ParseError) string() string {
	return fmt.Sprintf("[line %d, col %d] %s", e.Line, e.Col, e.Msg)
}

// --- position helpers --------------------------------------------------------

// curPos captures the exact line and column of the token currently being parsed.
func (p *Parser) curPos() ast.Position {
	return ast.Position{Line: p.curToken.Line, Col: p.curToken.Col}
}

// --- public error API --------------------------------------------------------

// Errors returns all collected error messages as plain strings so that call
// sites that already use p.Errors() continue to work without changes.
func (p *Parser) Errors() []string {
	out := make([]string, len(p.parseErrors))
	for i, e := range p.parseErrors {
		out[i] = e.string()
	}
	return out
}

// ParseErrors returns the structured error slice for callers that want
// position information (e.g. an IDE language server).
func (p *Parser) ParseErrors() []ParseError {
	return p.parseErrors
}

// addError records a new error at the current token's position.
// Once maxErrors is reached no further errors are appended; a single
// sentinel message is added so the caller knows the cap was hit.
func (p *Parser) addError(msg string) {
	if len(p.parseErrors) >= p.maxErrors {
		// Only add the sentinel once (exactly when we hit the cap).
		if len(p.parseErrors) == p.maxErrors {
			p.parseErrors = append(p.parseErrors, ParseError{
				Msg:  fmt.Sprintf("too many errors (capped at %d); stopping error collection", p.maxErrors),
				Line: p.curToken.Line,
				Col:  p.curToken.Col,
			})
		}
		return
	}
	p.parseErrors = append(p.parseErrors, ParseError{
		Msg:  msg,
		Line: p.curToken.Line,
		Col:  p.curToken.Col,
	})
}

// hadTooManyErrors reports whether the error cap was exceeded. The parser
// uses this to bail out of recovery loops early.
func (p *Parser) hadTooManyErrors() bool {
	return len(p.parseErrors) > p.maxErrors
}

// --- token navigation --------------------------------------------------------

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.peek2Token
	p.peek2Token = p.l.NextToken()
}

// --- expectPeek / peekError --------------------------------------------------

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead at line %d, col %d",
		t, p.peekToken.Type, p.peekToken.Line, p.peekToken.Col)
	p.addError(msg)
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

// --- precedence helpers ------------------------------------------------------

func (p *Parser) peekPrecedence() int {
	if pr, ok := precedences[p.peekToken.Type]; ok {
		return pr
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if pr, ok := precedences[p.curToken.Type]; ok {
		return pr
	}
	return LOWEST
}

// --- whitespace / statement-end helpers --------------------------------------

func (p *Parser) skipNewlines() {
	for p.curToken.Type == token.NEWLINE {
		p.nextToken()
	}
}

// synchronize discards tokens until it lands on a safe point to resume
// parsing. It consumes the synchronisation token itself (NEWLINE /
// SEMICOLON) so the next parseStmt call starts on a fresh token, but it
// leaves RBRACE and EOF in place so the block/program loop can see them.
//
// Call this immediately after recording an error inside a statement so
// that the enclosing block loop can try the next statement cleanly.
func (p *Parser) synchronize() {
	for p.curToken.Type != token.EOF {
		if p.curToken.Type == token.RBRACE {
			// Leave '}' for the block loop — don't consume it.
			return
		}
		if p.curToken.Type == token.NEWLINE || p.curToken.Type == token.SEMICOLON {
			p.nextToken() // step past the terminator
			return
		}
		// Also stop just before a closing brace so the block loop exits.
		if p.peekToken.Type == token.RBRACE {
			p.nextToken() // land ON the '}'
			return
		}
		p.nextToken()
	}
}

// synchronizeToDecl is like synchronize but used at the top-level program
// loop. It skips tokens until it finds the start of a new top-level
// declaration (FN or TYPE) or EOF, so that a broken function body doesn't
// swallow the next function.
func (p *Parser) synchronizeToDecl() {
	for p.curToken.Type != token.EOF {
		switch p.curToken.Type {
		case token.FN, token.TYPE, token.ASYNC: // NEW: Stop recovering if we see ASYNC
			return // ready to try parsing the next declaration
		}
		p.nextToken()
	}
}

func (p *Parser) expectStatementEnd() {
	switch p.peekToken.Type {
	case token.NEWLINE, token.SEMICOLON:
		p.nextToken() // consume the terminator
		return
	case token.RBRACE, token.LBRACE, token.RPAREN, token.EOF:
		// These are valid terminators but belong to the enclosing
		// construct — leave them in place for the caller to handle.
		return
	default:
		p.addError(fmt.Sprintf(
			"expected end of statement, got %s at line %d, col %d",
			p.peekToken.Type, p.peekToken.Line, p.peekToken.Col,
		))
		p.nextToken()
		p.synchronize()
	}
}

// isNilDecl safely handles typed nils (common Go interface gotcha).
func isNilDecl(d ast.Decl) bool {
	if d == nil {
		return true
	}
	switch v := d.(type) {
	case *ast.FnDecl:
		return v == nil
	case *ast.TypeDecl:
		return v == nil
	default:
		return false
	}
}
