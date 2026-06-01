package tast

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

type Position = ast.Position

type Node interface {
	Pos() Position
	End() Position
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

type TypeExpr interface {
	Node
	typeNode()
}

type Pattern interface {
	Node
	patternNode()
}

// ==========================================================================
// Declarations
// ==========================================================================

type Program struct {
	Decls []Decl
}

type Param struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Name   string
	Type   types.Type
	Symbol *types.Symbol
}

type FnDecl struct {
	Pos_       Position `json:"-"`
	End_       Position `json:"-"`
	Name       string
	Params     []*Param
	ReturnType types.Type
	Body       *BlockStmt
	IsAsync    bool
	Symbol     *types.Symbol
}

type TypeDecl struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Name   *Identifier
	Type   types.Type
	Symbol *types.Symbol
}

// ==========================================================================
// Statements
// ==========================================================================

type BlockStmt struct {
	Pos_       Position `json:"-"`
	End_       Position `json:"-"`
	Statements []Stmt
}

type DeclareStmt struct {
	Pos_   Position `json:"-"`
	Symbol *types.Symbol
	Value  Expr
}

type AssignStmt struct {
	Pos_   Position `json:"-"`
	LValue Expr
	RValue Expr
}

type ReturnStmt struct {
	Pos_  Position `json:"-"`
	Value Expr
}

type YieldStmt struct {
	Pos_  Position `json:"-"`
	Value Expr
}

type ExprStmt struct {
	Pos_  Position `json:"-"`
	Value Expr
}

type ForStmt struct {
	Pos_      Position `json:"-"`
	Init      Stmt
	Condition Expr
	Post      Stmt
	Body      *BlockStmt
}

type BreakStmt struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`
}

type ContinueStmt struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`
}

// ==========================================================================
// Expressions
// ==========================================================================

type Identifier struct {
	Pos_   Position `json:"-"`
	Value  string
	Type   types.Type
	Symbol *types.Symbol
}

type IntLiteral struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Value int64
	Type  types.Type
}

type BoolLiteral struct {
	Pos_  Position `json:"-"`
	Value bool
	Type  types.Type
}

type StringLiteral struct {
	Pos_  Position `json:"-"`
	Value string
	Type  types.Type
}

type PrefixExpr struct {
	Pos_     Position `json:"-"`
	Operator string
	Right    Expr
	Type     types.Type
}

type InfixExpr struct {
	Pos_     Position `json:"-"`
	Left     Expr
	Operator string
	Right    Expr
	Type     types.Type
}

type StructField struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Key   Expr
	Value Expr
}

type StructLiteral struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Fields []StructField
	Type   types.Type
}

type ArrayLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []Expr
	Type     types.Type
}

type MapLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []MapElement
	Type     types.MapType
}

type MapElement struct {
	Key   Expr
	Value Expr
}

type VecLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []Expr
	Type     types.VectorType
}

type VariantField struct {
	Name  string
	Value Expr
}

type VariantLiteral struct {
	Pos_      Position `json:"-"`
	End_      Position `json:"-"`
	Variant   *types.SumVariant
	Arguments []Expr
	Fields    []VariantField // Added for struct-style variants
	Type      types.Type
}

type CallArg struct {
	Pos_     Position `json:"-"`
	Argument Expr
	Mut      bool
	Own      bool
}

type CallExpr struct {
	Pos_      Position `json:"-"`
	Function  Expr
	Arguments []CallArg
	Type      types.Type
}

type MethodCallExpr struct {
	Pos_      Position `json:"-"`
	Object    Expr
	Method    *Identifier
	Arguments []CallArg
	Type      types.Type
}

type FieldAccess struct {
	Pos_   Position `json:"-"`
	Object Expr
	Field  *Identifier
	Type   types.Type
}

type IndexExpr struct {
	Pos_  Position `json:"-"`
	Left  Expr
	Index Expr
	Type  types.Type
}

type SliceExpr struct {
	Pos_ Position `json:"-"`
	Left Expr
	Low  Expr
	High Expr
	Type types.Type
}

type IfExpr struct {
	Pos_        Position `json:"-"`
	Condition   Expr
	Consequence *BlockStmt
	Alternative *BlockStmt
	Type        types.Type
}

type AwaitExpr struct {
	Pos_  Position `json:"-"`
	Value Expr
	Type  types.Type
}

// ==========================================================================
// Match Expressions
// ==========================================================================

type MatchExpr struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Subject Expr
	Arms    []MatchArm
	Type    types.Type
}

type MatchArm struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Pattern Pattern
	Body    Expr
}

type WildcardPattern struct {
	Pos_ Position `json:"-"`
}

type LiteralPattern struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Value Expr
}

type VariantPatternField struct {
	Field   string
	Binding *Identifier
}

type VariantPattern struct {
	Pos_          Position `json:"-"`
	End_          Position `json:"-"`
	Variant       *Identifier
	TupleBindings []*Identifier
	Fields        []VariantPatternField
	Type          types.Type
	Symbol        *types.Symbol
}

// ==========================================================================
// New Desugaring Helper Nodes
// ==========================================================================

// VariantDiscriminantExpr reads the discriminant integer from a sum type value.
type VariantDiscriminantExpr struct {
	Pos_   Position `json:"-"`
	Object Expr
	Type   types.Type
}

// VariantReadExpr extracts a payload field from a specific variant.
type VariantReadExpr struct {
	Pos_         Position `json:"-"`
	Object       Expr
	VariantName  string
	PayloadIndex int
	Type         types.Type
}

// MapReadExpr desugars map[key] into a runtime call (maml_map_get)
type MapReadExpr struct {
	Pos_ Position `json:"-"`
	Map  Expr
	Key  Expr
	Type types.Type
}

// MapInsertStmt desugars map[key] = value into a runtime call (maml_map_put).
type MapInsertStmt struct {
	Pos_  Position `json:"-"`
	Map   Expr
	Key   Expr
	Value Expr
}

// VecReadExpr desugars vec[i] into a runtime call (maml_vec_get)
type VecReadExpr struct {
	Pos_  Position `json:"-"`
	Vec   Expr
	Index Expr
	Type  types.Type
}

// ==========================================================================
// Operand Type
// ==========================================================================

// Operand represents a fully flattened, atomic expression that can be safely
// consumed by a MIR instruction (e.g., identifiers and primitive literals).
type Operand interface {
	Expr
	isOperand()
}

// Implement the marker method on the safe types:
func (*Identifier) isOperand()    {}
func (*IntLiteral) isOperand()    {}
func (*BoolLiteral) isOperand()   {}
func (*StringLiteral) isOperand() {}

// =============================================================================
// Helper Methods
// =============================================================================

// TypeOf cleanly extracts the bound type from any TAST expression interface.
func TypeOf(expr Expr) types.Type {
	if expr == nil {
		return types.UnknownType{}
	}
	switch e := expr.(type) {
	case *IntLiteral:
		return e.Type
	case *BoolLiteral:
		return e.Type
	case *StringLiteral:
		return e.Type
	case *Identifier:
		return e.Type
	case *InfixExpr:
		return e.Type
	case *PrefixExpr:
		return e.Type
	case *IfExpr:
		return e.Type
	case *CallExpr:
		return e.Type
	case *FieldAccess:
		return e.Type
	case *IndexExpr:
		return e.Type
	case *SliceExpr:
		return e.Type
	case *StructLiteral:
		return e.Type
	case *ArrayLiteral:
		return e.Type
	case *VariantLiteral:
		return e.Type
	case *AwaitExpr:
		return e.Type
	case *MatchExpr:
		return e.Type
	case *MethodCallExpr:
		return e.Type
	case *MapLiteral:
		return e.Type
	case *VecLiteral:
		return e.Type
	case *MapReadExpr:
		return e.Type
	case *VecReadExpr:
		return e.Type
	case *BlockStmt:
		return TypeOfBlock(e)
	}
	return types.UnknownType{}
}

// TypeOfBlock evaluates the unified return type of a block by checking its final Yield statement.
func TypeOfBlock(block *BlockStmt) types.Type {
	if block == nil || len(block.Statements) == 0 {
		return types.UnitType{}
	}
	last := block.Statements[len(block.Statements)-1]
	if yield, ok := last.(*YieldStmt); ok {
		return TypeOf(yield.Value)
	}
	return types.UnitType{}
}
