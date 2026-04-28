package lexer

// Lexer turns source code (string) into a stream of Token objects.
type Lexer struct {
	input            string // the source code being tokenized
	position         int    // points to the current character (l.ch)
	readPosition     int    // points to the next character to read
	ch               byte   // current character under examination
	line             int    // current line number (1-based)
	col              int    // current column number (1-based)
	lastEmittedToken TokenType // Tracks the last token to power ASI
	bracketDepth     int       // Tracks open (), [], {} for ASI
}

// New creates and initializes a new Lexer.
func New(input string) *Lexer {
	l := &Lexer{
		input: input,
		line:  1,
		col:   0,
	}
	l.readChar() // prime the lexer with the first character
	return l
}

// NextToken returns the next token from the input.
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	// Capture the start position of this token
	startLine := l.line
	startCol := l.col

	var tok Token

	switch l.ch {
	// === Two-character operators ===
	case '=':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(EQ, startLine, startCol)
		} else if l.peekChar() == '>' {
			tok = l.twoCharToken(YIELD, startLine, startCol)
		} else {
			tok = l.newToken(UPDATE, l.ch, startLine, startCol)
		}
	case '~':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(DECLARE_MUTABLE, startLine, startCol)
		} else {
			tok = l.newToken(ILLEGAL, l.ch, startLine, startCol)
		}
	case ':':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(DECLARE_IMMUTABLE, startLine, startCol)
		} else {
			tok = l.newToken(COLON, l.ch, startLine, startCol)
		}
	case '|':
		if l.peekChar() == '>' {
			tok = l.twoCharToken(PIPE, startLine, startCol)
		} else if l.peekChar() == '|' {
			tok = l.twoCharToken(OR, startLine, startCol)
		} else {
			tok = l.newToken(SEPARATOR, l.ch, startLine, startCol)
		}
	case '&':
		if l.peekChar() == '&' {
			tok = l.twoCharToken(AND, startLine, startCol)
		} else {
			tok = l.newToken(ILLEGAL, l.ch, startLine, startCol)
		}
	case '!':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(NOT_EQ, startLine, startCol)
		} else {
			tok = l.newToken(NOT, l.ch, startLine, startCol)
		}
	case '<':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(LTE, startLine, startCol)
		} else {
			tok = l.newToken(LT, l.ch, startLine, startCol)
		}
	case '>':
		if l.peekChar() == '=' {
			tok = l.twoCharToken(GTE, startLine, startCol)
		} else {
			tok = l.newToken(GT, l.ch, startLine, startCol)
		}

	// === Single-character tokens & Delimiters ===
	case '+':
		tok = l.newToken(PLUS, l.ch, startLine, startCol)
	case '-':
		tok = l.newToken(MINUS, l.ch, startLine, startCol)
	case '*':
		tok = l.newToken(MULTIPLY, l.ch, startLine, startCol)
	case '/':
		if l.peekChar() == '/' {
			l.skipComment()
			return l.NextToken() // Recurse to get the next meaningful token
		}
		tok = l.newToken(DIVIDE, l.ch, startLine, startCol)
	case '%':
		tok = l.newToken(MODULO, l.ch, startLine, startCol)
	case '.':
		tok = l.newToken(DOT, l.ch, startLine, startCol)
	case ',':
		tok = l.newToken(COMMA, l.ch, startLine, startCol)
		
	// Track Bracket Depth for ASI
	case '{':
		l.bracketDepth++
		tok = l.newToken(LBRACE, l.ch, startLine, startCol)
	case '}':
		l.bracketDepth--
		tok = l.newToken(RBRACE, l.ch, startLine, startCol)
	case '(':
		l.bracketDepth++
		tok = l.newToken(LPAREN, l.ch, startLine, startCol)
	case ')':
		l.bracketDepth--
		tok = l.newToken(RPAREN, l.ch, startLine, startCol)
	case '[':
		l.bracketDepth++
		tok = l.newToken(LBRACKET, l.ch, startLine, startCol)
	case ']':
		l.bracketDepth--
		tok = l.newToken(RBRACKET, l.ch, startLine, startCol)

	// === Special cases ===
	case '"':
		str := l.readString()
		typ := STRING
		if l.ch != '"' {
			typ = ILLEGAL // unterminated string
		}
		tok = Token{Type: TokenType(typ), Literal: str, Line: startLine, Col: startCol}

	case '\n':
		tok = Token{Type: NEWLINE, Literal: "\\n", Line: startLine, Col: startCol}

	case 0: // end of input
		tok = Token{Type: EOF, Literal: "", Line: startLine, Col: startCol}

	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			tok = Token{
				Type:    LookupIdent(literal),
				Literal: literal,
				Line:    startLine,
				Col:     startCol,
			}
			l.lastEmittedToken = tok.Type // Store before early return
			return tok
		} else if isDigit(l.ch) {
			literal, typ := l.readNumber()
			tok = Token{
				Type:    typ,
				Literal: literal,
				Line:    startLine,
				Col:     startCol,
			}
			l.lastEmittedToken = tok.Type // Store before early return
			return tok
		} else {
			tok = l.newToken(ILLEGAL, l.ch, startLine, startCol)
		}
	}

	l.readChar()
	l.lastEmittedToken = tok.Type // Store for standard returns
	return tok
}

// twoCharToken consumes the current character + the next one and returns a token.
func (l *Lexer) twoCharToken(tokenType TokenType, startLine, startCol int) Token {
	first := l.ch
	l.readChar() // consume the second character
	literal := string(first) + string(l.ch)
	return Token{
		Type:    tokenType,
		Literal: literal,
		Line:    startLine,
		Col:     startCol,
	}
}

// newToken creates a single-character token.
func (l *Lexer) newToken(tokenType TokenType, ch byte, startLine, startCol int) Token {
	return Token{
		Type:    tokenType,
		Literal: string(ch),
		Line:    startLine,
		Col:     startCol,
	}
}

// skipWhitespace handles regular whitespace and implements ASI logic for newlines.
func (l *Lexer) skipWhitespace() {
	for {
		if l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
			l.readChar()
		} else if l.ch == '\n' {
			// ASI Logic: If we are inside brackets or the last token cannot end a statement,
			// treat the newline as regular whitespace and skip it.
			if l.bracketDepth > 0 || !canEndStatement(l.lastEmittedToken) {
				l.readChar()
			} else {
				// It's a significant newline. Stop skipping so NextToken can emit it.
				break
			}
		} else {
			break
		}
	}
}

// skipComment skips everything from // until the end of the line.
func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
	// DO NOT consume the newline here.
	// NextToken will recurse and hit skipWhitespace, which decides if the \n matters.
}

// readChar advances the lexer by one character and updates line/col correctly.
func (l *Lexer) readChar() {
	// 1. If the character we are LEAVING is a newline, update line and reset col
	if l.ch == '\n' {
		l.line++
		l.col = 0
	}

	// 2. Read the next character
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}

	l.position = l.readPosition
	l.readPosition++

	// 3. Advance the column for the new character we just ENTERED
	l.col++
}

// peekChar returns the next character without advancing the lexer.
func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// readIdentifier reads an identifier or keyword.
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

// readNumber reads integers or floats.
func (l *Lexer) readNumber() (string, TokenType) {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar() // consume the dot
		for isDigit(l.ch) {
			l.readChar()
		}
		return l.input[position:l.position], FLOAT
	}
	return l.input[position:l.position], INT
}

// readString reads the content inside "...".
func (l *Lexer) readString() string {
	position := l.position + 1 // skip the opening quote
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

// canEndStatement determines if the current token type legally ends a statement.
func canEndStatement(typ TokenType) bool {
	switch typ {
	case IDENT, INT, FLOAT, STRING, BOOL, RPAREN, RBRACE, RBRACKET:
		return true
	default:
		return false
	}
}