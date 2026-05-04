package ast

import (
	"bytes"
	"fmt"
	"strings"
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

// StructField represents a single field assignment:  x: 10
type StructField struct {
	Name  *Identifier
	Value Expr
}

// StructLiteral represents: Point{x: 10, y: 20}
type StructLiteral struct {
	Type   *Identifier
	Fields []StructField
	Pos_   Position
	End_   Position
}

func (sl *StructLiteral) Pos() Position { return sl.Pos_ }
func (sl *StructLiteral) End() Position { return sl.End_ }
func (sl *StructLiteral) String() string {
	var fields []string
	for _, f := range sl.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name.String(), f.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", sl.Type.String(), strings.Join(fields, ", "))
}
func (sl *StructLiteral) exprNode() {}

// -----------------------------------------------------------------------------
// Operations
// -----------------------------------------------------------------------------

type CallExpr struct {
	Function  Expr
	Arguments []Expr
	Pos_      Position
}

func (ce *CallExpr) Pos() Position { return ce.Pos_ }
func (ce *CallExpr) End() Position {
	if len(ce.Arguments) > 0 {
		return ce.Arguments[len(ce.Arguments)-1].End() // Rough estimation
	}
	return ce.Function.End()
}
func (ce *CallExpr) String() string {
	var args []string
	for _, a := range ce.Arguments {
		args = append(args, a.String())
	}
	return fmt.Sprintf("%s(%s)", ce.Function.String(), strings.Join(args, ", "))
}
func (ce *CallExpr) exprNode() {}

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

// FieldAccess represents: object.field
type FieldAccess struct {
	Object Expr        // Can be an Identifier (p), or another FieldAccess (user.address)
	Field  *Identifier // Must be an Identifier (x)
	Pos_   Position
}

func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) End() Position { return fa.Field.End() }
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s.%s)", fa.Object.String(), fa.Field.String())
}
func (fa *FieldAccess) exprNode() {}

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
