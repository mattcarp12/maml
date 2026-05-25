package tast

import "github.com/mattcarp12/maml/frontend/ast"

// =============================================================================
// Source Positions
// =============================================================================

// Position tracks a location within a source file exactly as it was written.
// type Position struct {
// 	Line int
// 	Col  int
// }

// func (p Position) String() string {
// 	return fmt.Sprintf("%d:%d", p.Line, p.Col)
// }

type Position = ast.Position

// =============================================================================
// Core Node Interfaces
// =============================================================================

// Node is the base contract implemented by every TAST node.
// Architectural Invariant: TAST nodes are strictly read-only.
type Node interface {
	Pos() Position
	End() Position
	String() string
}

// Decl represents a top-level declaration.
type Decl interface {
	Node
	declNode()
}

// Stmt represents an executable statement.
type Stmt interface {
	Node
	stmtNode()
}

// Expr represents a value-producing expression.
type Expr interface {
	Node
	exprNode()
}

// TypeExpr represents a type-level expression.
type TypeExpr interface {
	Node
	typeNode()
}

// Pattern represents a match-pattern node.
type Pattern interface {
	Node
	patternNode()
}
