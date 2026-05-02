package ast

import "fmt"

// -----------------------------------------------------------------------------
// Literals & Identifiers
// -----------------------------------------------------------------------------

type Identifier struct {
	Value string
	Pos_  Position
}

func (i *Identifier) Pos() Position  { return i.Pos_ }
func (i *Identifier) End() Position  { return Position{Line: i.Pos_.Line, Col: i.Pos_.Col + len(i.Value)} }
func (i *Identifier) String() string { return i.Value }
func (i *Identifier) exprNode()      {}

type IntLiteral struct {
	Value int64
	Pos_  Position
	End_  Position
}

func (il *IntLiteral) Pos() Position  { return il.Pos_ }
func (il *IntLiteral) End() Position  { return il.End_ }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (il *IntLiteral) exprNode()      {}

// -----------------------------------------------------------------------------
// Operations
// -----------------------------------------------------------------------------

type InfixExpr struct {
	Left     Expr
	Operator string
	Right    Expr
}

func (ie *InfixExpr) Pos() Position  { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position  { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode() {}