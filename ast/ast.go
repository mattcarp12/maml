package ast

import "fmt"

// Position tracks where exactly a node lives in the source code
type Position struct {
	Line int
	Col int
	// File string 
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// Node is the base contract for everything in the AST.
type Node interface {
	Pos() Position
	End() Position
	String() string
}

// -----------------------------------------------------------------------------
// Core Classifications
// -----------------------------------------------------------------------------

// Decl represents a top-level declaraton (Functions, Structs, Types).
// Declarations introduce names into the package scope.
type Decl interface {
	Node
	declNode()
}

// Stmt represents an action within a block (Assignment, Return, Ifs)
// Statements do not evaluate to a value
type Stmt interface {
	Node
	stmtNode()
}

// Expr represents a computation that evaluates to a value (Math, Variables, Function calls)
type Expr interface {
	Node 
	exprNode()
}