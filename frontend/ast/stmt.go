package ast

import (
	"bytes"
	"fmt"

	"github.com/mattcarp12/maml/frontend/token"
)

// =============================================================================
// Block Scope
// =============================================================================

type BlockStmt struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Statements []Stmt `json:"statements"`
}

// =============================================================================
// Variable Statements
// =============================================================================

type DeclareStmt struct {
	Pos_ Position `json:"-"`

	Name    string `json:"name"`
	Mutable bool   `json:"mutable"`
	Value   Expr   `json:"value"`
}

type AssignStmt struct {
	Pos_ Position `json:"-"`

	LValue Expr `json:"lvalue"`
	RValue Expr `json:"rvalue"`
}

// =============================================================================
// Control Flow Statements
// =============================================================================

type ReturnStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

// YieldStmt represents:
//
//	=> <expr>
type YieldStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

// ExprStmt represents an expression used as a statement.
type ExprStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

type ForStmt struct {
	Pos_ Position `json:"-"`

	Init      Stmt       `json:"init"`
	Condition Expr       `json:"condition"`
	Post      Stmt       `json:"post"`
	Body      *BlockStmt `json:"body"`
}

// =============================================================================
// Loop Control
// =============================================================================

type BreakStmt struct {
	Token token.Token `json:"-"`
}

type ContinueStmt struct {
	Token token.Token `json:"-"`
}

// =============================================================================
// MIR Loop Construct
// =============================================================================

// LoopStmt represents an unconditional infinite loop.
//
// This acts as the primitive iterative construct used during lowering.
type LoopStmt struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Body *BlockStmt `json:"body"`
}

// =============================================================================
// Explicit ARC Statements
// =============================================================================

type RetainStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

type ReleaseStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

// =============================================================================
// Interface Implementations
// =============================================================================

func (b *BlockStmt) Pos() Position { return b.Pos_ }
func (b *BlockStmt) End() Position { return b.End_ }

func (b *BlockStmt) String() string {
	var out bytes.Buffer

	out.WriteString("{\n")

	for _, s := range b.Statements {
		out.WriteString("\t")
		out.WriteString(s.String())
		out.WriteString("\n")
	}

	out.WriteString("}")

	return out.String()
}

func (b *BlockStmt) stmtNode() {}
func (b *BlockStmt) exprNode() {}

func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) End() Position { return d.Value.End() }

func (d *DeclareStmt) String() string {
	op := token.DECLARE.String()

	format := "%s %s %s"

	if d.Mutable {
		format = "mut " + format
	}

	return fmt.Sprintf(format, d.Name, op, d.Value.String())
}

func (d *DeclareStmt) stmtNode() {}

func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) End() Position { return a.RValue.End() }

func (a *AssignStmt) String() string {
	return fmt.Sprintf(
		"%s = %s",
		a.LValue.String(),
		a.RValue.String(),
	)
}

func (a *AssignStmt) stmtNode() {}

func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) End() Position { return r.Value.End() }

func (r *ReturnStmt) String() string {
	return fmt.Sprintf(
		token.RETURN.String()+" %s",
		r.Value.String(),
	)
}

func (r *ReturnStmt) stmtNode() {}

func (ys *YieldStmt) Pos() Position { return ys.Pos_ }
func (ys *YieldStmt) End() Position { return ys.Value.End() }

func (ys *YieldStmt) String() string {
	return fmt.Sprintf("=> %s", ys.Value.String())
}

func (ys *YieldStmt) stmtNode() {}

func (es *ExprStmt) Pos() Position { return es.Pos_ }
func (es *ExprStmt) End() Position { return es.Value.End() }

func (es *ExprStmt) String() string {
	return es.Value.String()
}

func (es *ExprStmt) stmtNode() {}

func (f *ForStmt) Pos() Position { return f.Pos_ }
func (f *ForStmt) End() Position { return f.Body.End() }

func (f *ForStmt) String() string {
	return fmt.Sprintf(
		"for (%s; %s; %s) %s",
		f.Init.String(),
		f.Condition.String(),
		f.Post.String(),
		f.Body.String(),
	)
}

func (f *ForStmt) stmtNode() {}

func (s *BreakStmt) Pos() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col,
	}
}

func (s *BreakStmt) End() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col + len(s.Token.Literal),
	}
}

func (s *BreakStmt) String() string {
	return s.Token.Literal
}

func (s *BreakStmt) stmtNode() {}

func (s *ContinueStmt) Pos() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col,
	}
}

func (s *ContinueStmt) End() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col + len(s.Token.Literal),
	}
}

func (s *ContinueStmt) String() string {
	return s.Token.Literal
}

func (s *ContinueStmt) stmtNode() {}

func (l *LoopStmt) Pos() Position { return l.Pos_ }
func (l *LoopStmt) End() Position { return l.End_ }

func (l *LoopStmt) String() string {
	return fmt.Sprintf("loop %s", l.Body.String())
}

func (l *LoopStmt) stmtNode() {}

func (r *RetainStmt) Pos() Position { return r.Pos_ }
func (r *RetainStmt) End() Position { return r.Value.End() }

func (r *RetainStmt) String() string {
	return fmt.Sprintf("retain(%s)", r.Value.String())
}

func (r *RetainStmt) stmtNode() {}

func (r *ReleaseStmt) Pos() Position { return r.Pos_ }
func (r *ReleaseStmt) End() Position { return r.Value.End() }

func (r *ReleaseStmt) String() string {
	return fmt.Sprintf("release(%s)", r.Value.String())
}

func (r *ReleaseStmt) stmtNode() {}