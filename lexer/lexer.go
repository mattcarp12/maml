package lexer

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // current reading position in input (after current char)
	ch           byte // current char under examination
	line         int  // current line number for error reporting
	col          int  // current column number for error reporting
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1, col: 0}
	l.readChar()
	return l
}

func (l *Lexer) NextToken() Token {
	var tok Token

	l.skipWhitespace()

	startLine, startCol := l.line, l.col

	switch l.ch {
	case '=':
		switch l.peekChar() {
		case '=':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: EQ, Literal: literal, Line: startLine, Col: startCol}
		case '>':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: YIELD, Literal: literal, Line: startLine, Col: startCol}
		default:
			tok = newToken(UPDATE, l.ch)
		}
	case '~':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: DECLARE_MUTABLE, Literal: literal, Line: startLine, Col: startCol}
		} else {
			tok = newToken(ILLEGAL, l.ch)
		}
	case ':':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: DECLARE_IMMUTABLE, Literal: literal, Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: COLON, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '|':
		switch l.peekChar() {
		case '|':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: OR, Literal: literal, Line: startLine, Col: startCol}
		case '>':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: PIPE, Literal: literal, Line: startLine, Col: startCol}
		default:
			tok = Token{Type: SEPARATOR, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '.':
		tok = Token{Type: DOT, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '"':
		str := l.readString()
		if l.ch == '"' {
			tok = Token{Type: STRING, Literal: str, Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: ILLEGAL, Literal: str, Line: startLine, Col: startCol}
		}
	case '+':
		tok = Token{Type: PLUS, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '-':
		tok = Token{Type: MINUS, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: NOT_EQ, Literal: literal, Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: NOT, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '/':
		if l.peekChar() == '/' {
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
			// Skip the newline character at the end of the comment
			if l.ch == '\n' {
				l.readChar()
			}
			return l.NextToken()
		} else {
			tok = Token{Type: DIVIDE, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '*':
		tok = Token{Type: MULTIPLY, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '%':
		tok = Token{Type: MODULO, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: AND, Literal: literal, Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '<':
		switch l.peekChar() {
		case '=':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: LTE, Literal: literal, Line: startLine, Col: startCol}
		default:
			tok = Token{Type: LT, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '>':
		switch l.peekChar() {
		case '=':
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: GTE, Literal: literal, Line: startLine, Col: startCol}
		default:
			tok = Token{Type: GT, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case ',':
		tok = Token{Type: COMMA, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '{':
		tok = Token{Type: LBRACE, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '}':
		tok = Token{Type: RBRACE, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '(':
		tok = Token{Type: LPAREN, Literal: string(l.ch), Line: startLine, Col: startCol}
	case ')':
		tok = Token{Type: RPAREN, Literal: string(l.ch), Line: startLine, Col: startCol}
	case '[':
		tok = Token{Type: LBRACKET, Literal: string(l.ch), Line: startLine, Col: startCol}
	case ']':
		tok = Token{Type: RBRACKET, Literal: string(l.ch), Line: startLine, Col: startCol}
	case 0:
		tok.Literal = ""
		tok.Type = EOF
	case '\n':
		tok = Token{Type: NEWLINE, Literal: "\\n", Line: startLine, Col: startCol}
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = LookupIdent(tok.Literal)
			tok.Line = startLine
			tok.Col = startCol
			return tok
		} else if isDigit(l.ch) {
			tok.Literal, tok.Type = l.readNumber()
			tok.Line = startLine
			tok.Col = startCol
			return tok
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	}

	l.readChar()
	tok.Line = startLine
	tok.Col = startCol
	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
	}
}

// readChar gives us the next character and advance our position in the input string
func (l *Lexer) readChar() {
	if l.ch == '\n' {
		l.line++
		l.col = 0
	} else {
		l.col++
	}
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition += 1
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	} else {
		return l.input[l.readPosition]
	}
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() (string, TokenType) {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar() // consume '.'
		for isDigit(l.ch) {
			l.readChar()
		}
		return l.input[position:l.position], FLOAT
	}
	return l.input[position:l.position], INT
}

func (l *Lexer) readString() string {
	position := l.position + 1
	for {
		l.readChar()
		if l.ch == '"' || l.ch == 0 {
			break
		}
	}
	return l.input[position:l.position]
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func newToken(tokenType TokenType, ch byte) Token {
	return Token{Type: tokenType, Literal: string(ch)}
}
