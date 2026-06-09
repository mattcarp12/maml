package ast

import "fmt"

// =============================================================================
// Source Positions
// =============================================================================

type Position struct {
	Line int
	Col  int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
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

// ==========================================================================
// AST Helper Nodes
// ==========================================================================

type Param struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`
	Name string   `json:"name"`
	Type TypeExpr `json:"type"`
	Mut  bool     `json:"mut"`
}

type CompositeElement struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Key   Expr     `json:"key,omitempty"` // Optional for struct/map literals
	Value Expr     `json:"value"`
}

type CallArg struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Argument Expr     `json:"value"`
	Mut      bool     `json:"mut"`
	Own      bool     `json:"own"`
}

type MatchArm struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Pattern Pattern `json:"pattern"`
	Body    Expr    `json:"body"`
}

type CompositePatternElement struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Key     Expr     `json:"key,omitempty"`
	Pattern Pattern  `json:"pattern"`
}

type StructTypeField struct {
	Name string   `json:"name"`
	Type TypeExpr `json:"type"`
}

type VariantTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name        string            `json:"name"`
	TupleFields []TypeExpr        `json:"tupleFields,omitempty"`
	Fields      []StructTypeField `json:"structFields,omitempty"`
}
