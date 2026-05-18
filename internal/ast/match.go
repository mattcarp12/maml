package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// MatchExpr represents: match subject { case Pat { body } ... }
type MatchExpr struct {
	Subject Expr
	Arms    []MatchArm
	Pos_    Position
	End_    Position
}

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

// MatchArm represents a single case arm: case Pat { body }
type MatchArm struct {
	Pattern Pattern
	Body    *BlockStmt
	Pos_    Position
}

func (a *MatchArm) String() string {
	return fmt.Sprintf("case %s %s", a.Pattern.String(), a.Body.String())
}

// Pattern is the interface all pattern nodes implement.
type Pattern interface {
	patternNode()
	String() string
	Pos() Position
}

// WildcardPattern: _
type WildcardPattern struct {
	Pos_ Position
}

func (w *WildcardPattern) patternNode()   {}
func (w *WildcardPattern) Pos() Position  { return w.Pos_ }
func (w *WildcardPattern) String() string { return "_" }

// LiteralPattern: 42, true, false
type LiteralPattern struct {
	Value Expr // IntLiteral or BoolLiteral
	Pos_  Position
}

func (l *LiteralPattern) patternNode()   {}
func (l *LiteralPattern) Pos() Position  { return l.Pos_ }
func (l *LiteralPattern) String() string { return l.Value.String() }

type VariantPattern struct {
	Name    string
	Binding *Identifier    // single binding: case Circle(c)
	Fields  []FieldBinding // field bindings: case Circle{radius: r}
	Pos_    Position
}

// FieldBinding is one field destructuring in a pattern: radius: r
type FieldBinding struct {
	Field   string
	Binding *Identifier
}

func (v *VariantPattern) patternNode()  {}
func (v *VariantPattern) Pos() Position { return v.Pos_ }
func (v *VariantPattern) String() string {
	if v.Binding != nil {
		return fmt.Sprintf("%s(%s)", v.Name, v.Binding.Value)
	}
	if len(v.Fields) > 0 {
		var parts []string
		for _, fb := range v.Fields {
			parts = append(parts, fmt.Sprintf("%s: %s", fb.Field, fb.Binding.Value))
		}
		return fmt.Sprintf("%s{%s}", v.Name, strings.Join(parts, ", "))
	}
	return v.Name
}

// VariantLiteral represents a sum type constructor expression:
//
//	Circle{radius: 5}    — variant with payload
//	Point                — unit variant (Fields is empty)
type VariantLiteral struct {
	TypeName    string // name of the sum type (filled in by sema)
	VariantName string // "Circle", "Point", etc.
	Fields      []StructField
	Pos_        Position
	End_        Position
}

func (vl *VariantLiteral) Pos() Position { return vl.Pos_ }
func (vl *VariantLiteral) End() Position { return vl.End_ }
func (vl *VariantLiteral) String() string {
	if len(vl.Fields) == 0 {
		return vl.VariantName
	}
	var fields []string
	for _, f := range vl.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name.String(), f.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", vl.VariantName, strings.Join(fields, ", "))
}
func (vl *VariantLiteral) exprNode() {}
