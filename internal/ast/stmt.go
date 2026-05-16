package ast

import (
	"bytes"
	"fmt"
	"go/token"
)

// -----------------------------------------------------------------------------
// Block Scope
// -----------------------------------------------------------------------------

type BlockStmt struct {
	Statements []Stmt
	Pos_       Position
	End_       Position
}

func (b *BlockStmt) Pos() Position { return b.Pos_ }
func (b *BlockStmt) End() Position { return b.End_ }
func (b *BlockStmt) String() string {
	var out bytes.Buffer
	out.WriteString("{\n")
	for _, s := range b.Statements {
		out.WriteString("\t" + s.String() + "\n")
	}
	out.WriteString("}")
	return out.String()
}
func (b *BlockStmt) stmtNode() {}

// -----------------------------------------------------------------------------
// Declare and Assign Statements
// -----------------------------------------------------------------------------

type DeclareStmt struct {
	Name    string
	Mutable bool
	Value   Expr
	Pos_    Position
}

func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) End() Position { return d.Value.End() }
func (d *DeclareStmt) String() string {
	op := token.DEFINE.String()
	format := "%s %s %s"
	if d.Mutable {
		format += "mut "
	}
	return fmt.Sprintf(format, d.Name, op, d.Value.String())
}
func (d *DeclareStmt) stmtNode() {}

type AssignStmt struct {
	LValue Expr
	RValue Expr
	Pos_   Position
}

func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) End() Position { return a.RValue.End() }
func (a *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", a.LValue.String(), a.RValue.String())
}
func (a *AssignStmt) stmtNode() {}

// -----------------------------------------------------------------------------
// Function Return and Yield
// -----------------------------------------------------------------------------

type ReturnStmt struct {
	Value Expr
	Pos_  Position
}

func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) End() Position { return r.Value.End() }
func (r *ReturnStmt) String() string {
	return fmt.Sprintf(token.RETURN.String()+" %s", r.Value.String())
}
func (r *ReturnStmt) stmtNode() {}

// YieldStmt represents yielding a value out of a block using '=>'
type YieldStmt struct {
	Value Expr
	Pos_  Position
}

func (ys *YieldStmt) Pos() Position { return ys.Pos_ }
func (ys *YieldStmt) End() Position { return ys.Value.End() }
func (ys *YieldStmt) String() string {
	return fmt.Sprintf("=> %s", ys.Value.String())
}
func (ys *YieldStmt) stmtNode() {}

// -----------------------------------------------------------------------------
// ExprStmt - an expression that acts as a standalone statement.
// -----------------------------------------------------------------------------

type ExprStmt struct {
	Value Expr
	Pos_  Position
}

func (es *ExprStmt) Pos() Position { return es.Pos_ }
func (es *ExprStmt) End() Position { return es.Value.End() }
func (es *ExprStmt) String() string {
	return es.Value.String()
}
func (es *ExprStmt) stmtNode() {}

// -----------------------------------------------------------------------------
// For loop
// -----------------------------------------------------------------------------

type ForStmt struct {
	Init      Stmt
	Condition Expr
	Post      Stmt
	Body      *BlockStmt
	Pos_      Position
}

func (f *ForStmt) Pos() Position { return f.Pos_ }
func (f *ForStmt) End() Position { return f.Body.End() }
func (f *ForStmt) String() string {
	return fmt.Sprintf("for (%s; %s; %s) %s", f.Init.String(), f.Condition.String(), f.Post.String(), f.Body.String())
}
func (f *ForStmt) stmtNode() {}
