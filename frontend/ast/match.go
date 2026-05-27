package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// =============================================================================
// Match Expressions
// =============================================================================

// MatchExpr represents a pattern matching expression.
//
// Example:
//
//	match value {
//	    Some(x) => x,
//	    None    => 0,
//	}
type MatchExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Subject Expr       `json:"subject"`
	Arms    []MatchArm `json:"arms"`
}

// MatchArm represents a single match arm.
type MatchArm struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Pattern Pattern `json:"pattern"`
	Body    Expr    `json:"body"`
}

// =============================================================================
// Pattern Nodes
// =============================================================================

// -----------------------------------------------------------------------------
// Wildcard Pattern
// -----------------------------------------------------------------------------

// WildcardPattern represents:
//
//	_
type WildcardPattern struct {
	Pos_ Position `json:"-"`
}

// -----------------------------------------------------------------------------
// Literal Pattern
// -----------------------------------------------------------------------------

// LiteralPattern represents:
//
//	42
//	"hello"
//	true
type LiteralPattern struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Value Expr `json:"value"`
}

// -----------------------------------------------------------------------------
// Variant Pattern
// -----------------------------------------------------------------------------

// VariantPatternField represents a destructured variant field binding.
//
// Example:
//
//	x in:
//	    Some { x }
type VariantPatternField struct {
	Field   string      `json:"field"`
	Binding *Identifier `json:"binding"`
}

// VariantPattern represents a sum-type variant pattern.
//
// Examples:
//
//	Some(x)
//	Ok(value)
//	Error { code, message }
type VariantPattern struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name          string                `json:"name"`
	TupleBindings []*Identifier         `json:"binding,omitempty"`
	Fields        []VariantPatternField `json:"fields,omitempty"`
}

// =============================================================================
// Interface Implementations
// =============================================================================

// -----------------------------------------------------------------------------
// MatchExpr
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

// -----------------------------------------------------------------------------
// MatchArm
// -----------------------------------------------------------------------------

func (m *MatchArm) String() string {
	return fmt.Sprintf(
		"%s => %s,",
		m.Pattern.String(),
		m.Body.String(),
	)
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

func (w *WildcardPattern) String() string {
	return "_"
}

func (w *WildcardPattern) patternNode() {}

// -----------------------------------------------------------------------------
// LiteralPattern
// -----------------------------------------------------------------------------

func (l *LiteralPattern) Pos() Position { return l.Pos_ }

func (l *LiteralPattern) End() Position { return l.End_ }

func (l *LiteralPattern) String() string {
	return l.Value.String()
}

func (l *LiteralPattern) patternNode() {}

// -----------------------------------------------------------------------------
// VariantPattern
// -----------------------------------------------------------------------------

func (v *VariantPattern) Pos() Position { return v.Pos_ }

func (v *VariantPattern) End() Position { return v.End_ }

func (v *VariantPattern) String() string {
	var out bytes.Buffer

	out.WriteString(v.Name)

	if len(v.TupleBindings) > 0 {
		var bindings []string

		for _, f := range v.TupleBindings {
			bindings = append(bindings, f.String())
		}

		out.WriteString(" ( ")
		out.WriteString(strings.Join(bindings, ", "))
		out.WriteString(" )")
	}

	if len(v.Fields) > 0 {
		var fields []string

		for _, f := range v.Fields {
			fields = append(fields, f.String())
		}

		out.WriteString(" { ")
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

	if v.Field == v.Binding.Value {
		return v.Field
	}

	return fmt.Sprintf("%s: %s", v.Field, v.Binding.String())
}
