package ast

import (
	"bytes"
	"fmt"
)

// -----------------------------------------------------------------------------
// Literals & Identifiers
// -----------------------------------------------------------------------------

type Identifier struct {
	Value string
	Pos_  Position
}

func (i *Identifier) Pos() Position { return i.Pos_ }
func (i *Identifier) End() Position {
	return Position{Line: i.Pos_.Line, Col: i.Pos_.Col + len(i.Value)}
}
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

type BoolLiteral struct {
	Value bool
	Pos_  Position
}

func (b *BoolLiteral) Pos() Position { return b.Pos_ }
func (b *BoolLiteral) End() Position {
	length := 4 // "true"
	if !b.Value {
		length = 5 // "false"
	}
	return Position{Line: b.Pos_.Line, Col: b.Pos_.Col + length}
}
func (b *BoolLiteral) String() string { return fmt.Sprintf("%t", b.Value) }
func (b *BoolLiteral) exprNode()      {}

// -----------------------------------------------------------------------------
// Operations
// -----------------------------------------------------------------------------

type InfixExpr struct {
	Left     Expr
	Operator string
	Right    Expr
}

func (ie *InfixExpr) Pos() Position { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode() {}

// -----------------------------------------------------------------------------
// Control Flow
// -----------------------------------------------------------------------------

type IfExpr struct {
	Condition   Expr
	Consequence *BlockStmt
	Alternative *BlockStmt // This can be nil if there is no 'else'
	Pos_        Position
}

func (ie *IfExpr) Pos() Position { return ie.Pos_ }
func (ie *IfExpr) End() Position {
	if ie.Alternative != nil {
		return ie.Alternative.End()
	}
	return ie.Consequence.End()
}
func (ie *IfExpr) String() string {
	var out bytes.Buffer
	out.WriteString("if ")
	out.WriteString(ie.Condition.String())
	out.WriteString(" ")
	out.WriteString(ie.Consequence.String())

	if ie.Alternative != nil {
		out.WriteString(" else ")
		out.WriteString(ie.Alternative.String())
	}

	return out.String()
}
func (ie *IfExpr) exprNode() {}
