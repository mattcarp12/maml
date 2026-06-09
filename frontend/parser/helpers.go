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

func (p *Parser) curEndPos() ast.Position {
	// If the literal text exists, use its structural length; otherwise assume 1 char (e.g. ';', '}', ')')
	length := len(p.curToken.Literal)
	if length == 0 {
		length = 1
	}
	return ast.Position{Line: p.curToken.Line, Col: p.curToken.Col + length}
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
	if len(p.lookahead) > 0 {
		p.peek2Token = p.lookahead[0]
		p.lookahead = p.lookahead[1:]
	} else {
		p.peek2Token = p.l.NextToken()
	}
}

func (p *Parser) peekAhead(distance int) token.Token {
	if distance == 1 {
		return p.peekToken
	}
	if distance == 2 {
		return p.peek2Token
	}
	// distance >= 3: need tokens beyond peek2Token.
	// lookahead[0] = position 3, lookahead[1] = position 4, etc.
	idx := distance - 3
	needed := idx + 1 - len(p.lookahead)
	for i := 0; i < needed; i++ {
		p.lookahead = append(p.lookahead, p.l.NextToken())
	}
	return p.lookahead[idx]
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

const (
	_ int = iota
	LOWEST
	OR
	AND
	EQUALS      // == or !=
	LESSGREATER // > or < or >= or <=
	SUM         // + or -
	PRODUCT     // * or / or %
	PREFIX      // -X, !X, or await X  <-- Move this down!
	CALL        // fn() or struct literal or field access
	INDEX       // array[index]
)

var precedences = map[token.TokenType]int{
	token.AND:      AND,
	token.OR:       OR,
	token.EQ:       EQUALS,
	token.NOT_EQ:   EQUALS,
	token.LT:       LESSGREATER,
	token.LTE:      LESSGREATER,
	token.GT:       LESSGREATER,
	token.GTE:      LESSGREATER,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.MULTIPLY: PRODUCT,
	token.DIVIDE:   PRODUCT,
	token.MODULO:   PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACE:   CALL,
	token.DOT:      CALL,
	token.NOT:      PREFIX,
	token.LBRACKET: INDEX,
}

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

// parseCommaSeparatedList handles the boilerplate of parsing a comma-separated
// list of items until it hits the `end` token. The `parseElem` callback
// contains the logic to parse a single element and append it to the caller's list.
func (p *Parser) parseCommaSeparatedList(end token.TokenType, parseElem func()) bool {
	if p.peekToken.Type == end {
		p.nextToken()
		return true
	}

	p.nextToken()
	parseElem()

	for p.peekToken.Type == token.COMMA {
		p.nextToken()
		p.nextToken()
		parseElem()
	}

	// Returns true if it found the end token, false otherwise
	return p.expectPeek(end)
}

// looksLikeGenericInstantiation returns true when the token stream starting
// at peekToken looks like a generic instantiation:
//
//	< Ident [, Ident]* > followed by `{` or `(`
//
// It does NOT consume any tokens; it only peeks ahead.
// peekToken is already known to be LT when this is called.
func (p *Parser) looksLikeGenericInstantiation() bool {
	// Distances are relative to peekToken (distance 1).
	// peekToken (distance 1) == LT  ← already confirmed by caller
	// distance 2 must be an IDENT (first type argument)
	if p.peekAhead(2).Type != token.IDENT {
		return false
	}

	// Walk through comma-separated IDENT tokens looking for the closing >.
	// We support flat type arg lists: <A>, <A, B>, <A, B, C>, …
	// distance 2 is the first IDENT we already checked.
	dist := 2
	for {
		// After an IDENT, expect either ',' (another arg) or '>' (close).
		dist++ // step past the IDENT
		next := p.peekAhead(dist)
		switch next.Type {
		case token.GT:
			// Closing '>' found — check what follows it.
			after := p.peekAhead(dist + 1)
			return after.Type == token.LBRACE || after.Type == token.LPAREN
		case token.COMMA:
			// Another type argument — the token after ',' must be an IDENT.
			dist++ // step past ','
			if p.peekAhead(dist).Type != token.IDENT {
				return false
			}
			// loop continues; dist now points at the next IDENT
		default:
			return false
		}
	}
}
