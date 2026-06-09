package tast

import "github.com/mattcarp12/maml/frontend/types"

type Param struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Name   string
	Type   types.Type
	Symbol *types.Symbol
}

func (p *Param) Pos() Position { return p.Pos_ }
func (p *Param) End() Position { return p.End_ }

type CallArg struct {
	Pos_     Position `json:"-"`
	Argument Expr
	Mut      bool
}

type StructField struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Key   Expr
	Value Expr
}

type VariantField struct {
	Name  string
	Value Expr
}

type MapElement struct {
	Key   Expr
	Value Expr
}

type MatchArm struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Pattern Pattern
	Body    Expr
}

type CompositePatternElement struct {
	Key     string
	Pattern Pattern
}

type VariantPatternField struct {
	Field   string
	Binding *Identifier
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
