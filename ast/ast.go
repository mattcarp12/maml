// ast.go defines the abstract syntax tree (AST) structures for the MAML language.

package ast

import "fmt"

// The base Node interface
type Node interface {
	TokenLiteral() string
}

// All statement nodes implement this
type Statement interface {
	Node
	statementNode()
}

// All expression nodes implement this
type Expression interface {
	Node
	expressionNode()
}

type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

type FunctionDecl struct {
	Name string
	// Parameters []string // TODO: implement parameters
	ReturnType string
	Body       *BlockStatement
}

func (fd *FunctionDecl) TokenLiteral() string {
	return fd.Name
}

func (fd *FunctionDecl) statementNode() {}

type BlockStatement struct {
	Statements []Statement
}

func (bs *BlockStatement) TokenLiteral() string {
	if len(bs.Statements) > 0 {
		return bs.Statements[0].TokenLiteral()
	}
	return ""
}

func (bs *BlockStatement) statementNode() {}

type DeclareStatement struct {
	Name    string
	Mutable bool
	Value   Expression
}

func (ds *DeclareStatement) TokenLiteral() string {
	return ds.Name
}

func (ds *DeclareStatement) statementNode() {}

type ReturnStatement struct {
	Value Expression
}

func (rs *ReturnStatement) TokenLiteral() string {
	return "return"
}

func (rs *ReturnStatement) statementNode() {}

type Identifier struct {
	Value string
}

func (i *Identifier) TokenLiteral() string {
	return i.Value
}

func (i *Identifier) expressionNode() {}

type IntLiteral struct {
	Value int64
}

func (il *IntLiteral) TokenLiteral() string {
	return fmt.Sprintf("%d", il.Value)
}

func (il *IntLiteral) expressionNode() {}

type InfixExpression struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) TokenLiteral() string {
	return ie.Operator // e.g., "+"
}

func (ie *InfixExpression) expressionNode() {}