package lexer

import "github.com/mattcarp12/maml/internal/token"

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
func (l *Lexer) readNumber() (string, token.TokenType) {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar() // consume the dot
		for isDigit(l.ch) {
			l.readChar()
		}
		return l.input[position:l.position], token.FLOAT
	}
	return l.input[position:l.position], token.INT
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
