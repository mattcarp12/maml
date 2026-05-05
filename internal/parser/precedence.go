package parser

import "github.com/mattcarp12/maml/internal/token"

const (
	_ int = iota
	LOWEST
	EQUALS      // == or !=
	LESSGREATER // > or < or >= or <=
	SUM         // + or -
	PRODUCT     // * or / or %
	CALL        // Highest precedence!
)

var precedences = map[token.TokenType]int{
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
}
