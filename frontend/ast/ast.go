package ast

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/token"
)

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

// ==========================================================================
// Declarations
// ==========================================================================

type Program struct {
	Decls []Decl `json:"decls"`
}

type Param struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name string   `json:"name"`
	Type TypeExpr `json:"type"`

	Mut bool `json:"mut"`
	Own bool `json:"own"`
}

type FnDecl struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name       string     `json:"name"`
	IsAsync    bool       `json:"is_async"`
	Params     []*Param   `json:"params"`
	ReturnType TypeExpr   `json:"return_type,omitempty"`
	Body       *BlockStmt `json:"body"`
}

type TypeDecl struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name *Identifier `json:"name"`
	Rhs  TypeExpr    `json:"rhs"`
}

// ==========================================================================
// Statements
// ==========================================================================

type BlockStmt struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Statements []Stmt `json:"statements"`
}

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

type ReturnStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

type YieldStmt struct {
	Pos_  Position `json:"-"`
	Value Expr     `json:"value"`
}

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

type BreakStmt struct {
	Token token.Token `json:"-"`
}

type ContinueStmt struct {
	Token token.Token `json:"-"`
}

// ==========================================================================
// Expressions
// ==========================================================================

type Identifier struct {
	Pos_  Position `json:"-"`
	Value string   `json:"value"`
}

type IntLiteral struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Value int64    `json:"value"`
}

type BoolLiteral struct {
	Pos_  Position `json:"-"`
	Value bool     `json:"value"`
}

type StringLiteral struct {
	Pos_  Position `json:"-"`
	Value string   `json:"value"`
}

type CompositeElement struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Key   Expr     `json:"key,omitempty"` // Optional for struct/map literals
	Value Expr     `json:"value"`
}

type CompositeLiteral struct {
	Pos_     Position           `json:"-"`
	End_     Position           `json:"-"`
	TypeExpr TypeExpr           `json:"type"`
	Elements []CompositeElement `json:"elements"`
}

type CallExpr struct {
	Pos_ Position `json:"-"`

	Function  Expr      `json:"function"`
	Arguments []CallArg `json:"arguments"`
}

type MethodCallExpr struct {
	Object    Expr        // e.g., `a.b` in `a.b.foo()`
	Method    *Identifier // e.g., `foo`
	Arguments []CallArg
	Pos_      Position `json:"-"`
	End_      Position `json:"-"`
}

type CallArg struct {
	Pos_ Position `json:"-"`

	Argument Expr `json:"value"`
	Mut      bool `json:"mut"`
	Own      bool `json:"own"`
}

type InfixExpr struct {
	Pos_ Position `json:"-"`

	Left     Expr   `json:"left"`
	Operator string `json:"operator"`
	Right    Expr   `json:"right"`
}

type PrefixExpr struct {
	Pos_ Position `json:"-"`

	Operator string `json:"operator"`
	Right    Expr   `json:"right"`
}

type FieldAccess struct {
	Pos_ Position `json:"-"`

	Object Expr        `json:"object"`
	Field  *Identifier `json:"field"`
}

type IndexExpr struct {
	Pos_  Position `json:"-"`
	Left  Expr     `json:"left"`
	Index Expr     `json:"index"`
}

type SliceExpr struct {
	Pos_ Position `json:"-"`
	Left Expr     `json:"left"`
	Low  Expr     `json:"low"`
	High Expr     `json:"high"`
}

type IfExpr struct {
	Pos_        Position   `json:"-"`
	Condition   Expr       `json:"condition"`
	Consequence *BlockStmt `json:"consequence"`
	Alternative *BlockStmt `json:"alternative,omitempty"`
}

// ==========================================================================
// Match Expressions
// ==========================================================================

type MatchExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Subject Expr       `json:"subject"`
	Arms    []MatchArm `json:"arms"`
}

type MatchArm struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Pattern Pattern `json:"pattern"`
	Body    Expr    `json:"body"`
}

type WildcardPattern struct {
	Pos_ Position `json:"-"`
}

type IdentifierPattern struct {
    Pos_ Position `json:"-"`
    Name string   `json:"name"`
}




type LiteralPattern struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Value Expr `json:"value"`
}

type CompositePatternElement struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Key     Expr     `json:"key,omitempty"` // Optional: field name for struct patterns, nil for tuples
	Pattern Pattern  `json:"pattern"`       // The variable binding or a sub-pattern
}

type CompositePattern struct {
	Pos_     Position                  `json:"-"`
	End_     Position                  `json:"-"`
	TypeExpr TypeExpr                  `json:"type"` // Holds the Name (e.g., NamedTypeExpr for 'Circle')
	Elements []CompositePatternElement `json:"elements"`
}

// ==========================================================================
// Types
// ==========================================================================

// TypeExprWrapper is used for parsing literals
type TypeExprWrapper struct {
	Pos_     Position
	TypeExpr TypeExpr
}

type NamedTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name *Identifier `json:"name"`
}

type ArrayTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Size int      `json:"size"`
	Base TypeExpr `json:"base"`
}

type StructTypeField struct {
	Name string   `json:"name"`
	Type TypeExpr `json:"type"`
}

type StructTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name   string            `json:"name"`
	Fields []StructTypeField `json:"fields"`
}

type VariantTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name        string            `json:"name"`
	TupleFields []TypeExpr        `json:"tupleFields,omitempty"`
	Fields      []StructTypeField `json:"structFields,omitempty"`
}

type SumTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name     string            `json:"name"`
	TypeArgs []TypeExpr        `json:"type_args,omitempty"`
	Variants []VariantTypeExpr `json:"variants"`
}

type GenericTypeExpr struct {
	Pos_ Position
	End_ Position

	Name *Identifier
	Args []TypeExpr
}

// ==========================================================================
// Async
// ==========================================================================

type AwaitExpr struct {
	Value Expr     `json:"value"`
	Pos_  Position `json:"-"`
}
