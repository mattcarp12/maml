package ast

import "fmt"

// =============================================================================
// Source Positions
// =============================================================================

// Position tracks a location within a source file.
type Position struct {
	Line int
	Col  int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// =============================================================================
// Core Node Interfaces
// =============================================================================

// Node is the base contract implemented by every AST node.
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

// =============================================================================
// Error Handling
// =============================================================================

type CompileError struct {
	Stage string // "Lexer", "Parser", "Sema", or "Codegen"
	Pos   Position
	Msg   string
}

func (e CompileError) Error() string {
	return fmt.Sprintf("[%s Error] at %d:%d: %s", e.Stage, e.Pos.Line, e.Pos.Col, e.Msg)
}
