package hir

import (
	"bytes"
	"fmt"

	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Block Scope
// =============================================================================

type BlockStmt struct {
	Pos_       Position
	Statements []Stmt
}

// =============================================================================
// Variable Statements
// =============================================================================

type DeclareStmt struct {
	Pos_   Position
	Symbol *types.Symbol // Permanently bound semantic identity
	Value  Expr
}

type AssignStmt struct {
	Pos_   Position
	LValue Expr
	RValue Expr
}

// =============================================================================
// Control Flow Evaluation
// =============================================================================

type ReturnStmt struct {
	Pos_  Position
	Value Expr
}

type YieldStmt struct {
	Pos_  Position
	Value Expr
}

type ExprStmt struct {
	Pos_  Position
	Value Expr
}

// =============================================================================
// Primitive Loop Control (Desugared)
// =============================================================================

// LoopStmt is the ONLY looping construct allowed in the HIR.
// TAST ForStmts must be lowered into this.
type LoopStmt struct {
	Pos_ Position
	Body *BlockStmt
}

type BreakStmt struct {
	Pos_ Position
}

type ContinueStmt struct {
	Pos_ Position
}

// =============================================================================
// Interface Implementations
// =============================================================================

func (b *BlockStmt) Pos() Position { return b.Pos_ }
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

func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) String() string {
	modifier := ""
	if d.Symbol.Mutable {
		modifier = "mut "
	}
	return fmt.Sprintf("%s%s := %s", modifier, d.Symbol.Name, d.Value.String())
}
func (d *DeclareStmt) stmtNode() {}

func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", a.LValue.String(), a.RValue.String())
}
func (a *AssignStmt) stmtNode() {}

func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) String() string {
	if r.Value != nil {
		return fmt.Sprintf("return %s", r.Value.String())
	}
	return "return"
}
func (r *ReturnStmt) stmtNode() {}

func (ys *YieldStmt) Pos() Position  { return ys.Pos_ }
func (ys *YieldStmt) String() string { return fmt.Sprintf("=> %s", ys.Value.String()) }
func (ys *YieldStmt) stmtNode()      {}

func (es *ExprStmt) Pos() Position  { return es.Pos_ }
func (es *ExprStmt) String() string { return es.Value.String() }
func (es *ExprStmt) stmtNode()      {}

func (l *LoopStmt) Pos() Position  { return l.Pos_ }
func (l *LoopStmt) String() string { return fmt.Sprintf("loop %s", l.Body.String()) }
func (l *LoopStmt) stmtNode()      {}

func (s *BreakStmt) Pos() Position  { return s.Pos_ }
func (s *BreakStmt) String() string { return "break" }
func (s *BreakStmt) stmtNode()      {}

func (s *ContinueStmt) Pos() Position  { return s.Pos_ }
func (s *ContinueStmt) String() string { return "continue" }
func (s *ContinueStmt) stmtNode()      {}
