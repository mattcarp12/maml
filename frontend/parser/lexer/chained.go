package lexer

import "github.com/mattcarp12/maml/frontend/parser/token"

// Scanner defines the interface required by the parser.
// If your parser currently takes a concrete *Lexer, you will need to
// update parser.New() to accept this interface instead.
type Scanner interface {
	NextToken() token.Token
}

// ChainedLexer seamlessly streams tokens from multiple lexers.
type ChainedLexer struct {
	lexers []*Lexer
	index  int
}

func Multi(lexers ...*Lexer) Scanner {
	return &ChainedLexer{
		lexers: lexers,
		index:  0,
	}
}

func (c *ChainedLexer) NextToken() token.Token {
	if c.index >= len(c.lexers) {
		return token.Token{Type: token.EOF, Literal: ""}
	}

	tok := c.lexers[c.index].NextToken()

	// When we hit the end of the current file, move to the next one
	if tok.Type == token.EOF {
		c.index++
		// Recursively call to get the first token of the next file
		// (or the final EOF if we are completely out of files)
		return c.NextToken()
	}

	return tok
}
