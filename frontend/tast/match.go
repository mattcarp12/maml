package tast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Match Expressions
// =============================================================================

type MatchExpr struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Subject Expr
	Arms    []MatchArm
	Type    types.Type // Permanently bound unified return type of the match
}

type MatchArm struct {
	Pos_    Position `json:"-"`
	End_    Position `json:"-"`
	Pattern Pattern
	Body    Expr
}

// =============================================================================
// Pattern Nodes
// =============================================================================

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
	Binding *Identifier // The bound variable (inherits the permanently bound types.Symbol from expr.go)
}

type VariantPattern struct {
	Pos_          Position    `json:"-"`
	End_          Position    `json:"-"`
	Variant       *Identifier // The AST representation of the variant name
	TupleBindings []*Identifier
	Fields        []VariantPatternField
	Type          types.Type    // Permanently bound SumType being matched against
	Symbol        *types.Symbol // Permanently bound VariantSymbol (contains discriminant)
}

// =============================================================================
// Interface Implementations
// =============================================================================

// -----------------------------------------------------------------------------
// MatchExpr & MatchArm
// -----------------------------------------------------------------------------

func (m *MatchExpr) Pos() Position { return m.Pos_ }
func (m *MatchExpr) End() Position { return m.End_ }
func (m *MatchExpr) String() string {
	var out bytes.Buffer
	out.WriteString("match ")
	out.WriteString(m.Subject.String())
	out.WriteString(" {\n")
	for _, arm := range m.Arms {
		out.WriteString("\t")
		out.WriteString(arm.String())
		out.WriteString("\n")
	}
	out.WriteString("}")
	return out.String()
}
func (m *MatchExpr) exprNode() {}

func (m *MatchArm) String() string {
	return fmt.Sprintf("%s => %s,", m.Pattern.String(), m.Body.String())
}

// -----------------------------------------------------------------------------
// WildcardPattern
// -----------------------------------------------------------------------------

func (w *WildcardPattern) Pos() Position { return w.Pos_ }
func (w *WildcardPattern) End() Position {
	return Position{
		Line: w.Pos_.Line,
		Col:  w.Pos_.Col + 1,
	}
}
func (w *WildcardPattern) String() string { return "_" }
func (w *WildcardPattern) patternNode()   {}

// -----------------------------------------------------------------------------
// LiteralPattern
// -----------------------------------------------------------------------------

func (l *LiteralPattern) Pos() Position  { return l.Pos_ }
func (l *LiteralPattern) End() Position  { return l.End_ }
func (l *LiteralPattern) String() string { return l.Value.String() }
func (l *LiteralPattern) patternNode()   {}

// -----------------------------------------------------------------------------
// VariantPattern
// -----------------------------------------------------------------------------

func (v *VariantPattern) Pos() Position { return v.Pos_ }
func (v *VariantPattern) End() Position { return v.End_ }
func (v *VariantPattern) String() string {
	var out bytes.Buffer
	out.WriteString(v.Variant.String())
	if len(v.Fields) > 0 {
		out.WriteString(" { ")
		var fields []string
		for _, f := range v.Fields {
			fields = append(fields, f.String())
		}
		out.WriteString(strings.Join(fields, ", "))
		out.WriteString(" }")
	}
	return out.String()
}
func (v *VariantPattern) patternNode() {}

// -----------------------------------------------------------------------------
// VariantPatternField
// -----------------------------------------------------------------------------

func (v *VariantPatternField) String() string {
	if v.Binding == nil {
		return v.Field
	}
	return fmt.Sprintf("%s: %s", v.Field, v.Binding.String())
}
