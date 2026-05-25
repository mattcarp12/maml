package hir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
)

// =============================================================================
// Source Positions
// =============================================================================

// Position tracks the original source location for error reporting in the backend.
type Position struct {
	Line int
	Col  int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

func (p Position) ToAST() ast.Position {
	return ast.Position{
		Line: p.Line,
		Col:  p.Col,
	}
}

// =============================================================================
// Core Node Interfaces
// =============================================================================

// Node is the base contract implemented by every HIR node.
// Architectural Invariant: HIR nodes are strictly read-only after translation.
type Node interface {
	Pos() Position
	String() string
}

type Decl interface {
	Node
	declNode()
}

type Stmt interface {
	Node
	stmtNode()
}

type Expr interface {
	Node
	exprNode()
}

// =============================================================================
// Program Root
// =============================================================================

type Program struct {
	Decls []Decl
}

func (p *Program) Pos() Position {
	if len(p.Decls) == 0 {
		return Position{}
	}
	return p.Decls[0].Pos()
}

func (p *Program) String() string {
	return "<HIR Program>"
}
