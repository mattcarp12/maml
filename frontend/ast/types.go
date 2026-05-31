package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// =============================================================================
// Primitive & Named Types
// =============================================================================

// NamedTypeExpr represents a named type reference.
//
// Examples:
//
//	int
//	string
//	Point
//	Result<T>
type NamedTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name *Identifier `json:"name"`
}

// =============================================================================
// Container Types
// =============================================================================

// ArrayTypeExpr represents a fixed-size array type.
//
// Example:
//
//	[int; 4]
type ArrayTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Size int      `json:"size"`
	Base TypeExpr `json:"base"`
}

// SliceTypeExpr represents a dynamically-sized slice type.
//
// Example:
//
//	[]int
type SliceTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Base TypeExpr `json:"base"`
}

// =============================================================================
// Struct Types
// =============================================================================

// StructTypeField represents a struct field definition.
type StructTypeField struct {
	Name string   `json:"name"`
	Type TypeExpr `json:"type"`
}

// StructTypeExpr represents a struct type definition.
//
// Example:
//
//	struct {
//	    x: int,
//	    y: int,
//	}
type StructTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name   string            `json:"name"`
	Fields []StructTypeField `json:"fields"`
}

// =============================================================================
// Sum Types
// =============================================================================

// VariantTypeExpr represents a sum-type variant.
//
// Examples:
//
//	Some(value: int)
//	None
//	Error { code: int }
type VariantTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name        string            `json:"name"`
	TupleFields []TypeExpr        `json:"tupleFields,omitempty"`
	Fields      []StructTypeField `json:"structFields,omitempty"`
}

// SumTypeExpr represents an algebraic sum type.
//
// Example:
//
//	Some(int) | None
type SumTypeExpr struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name     string            `json:"name"`
	TypeArgs []TypeExpr        `json:"type_args,omitempty"`
	Variants []VariantTypeExpr `json:"variants"`
}

// --------------------------------------------------------------------------
// Generic Type Expressions
// --------------------------------------------------------------------------

type GenericTypeExpr struct {
	Pos_ Position
	End_ Position

	Name *Identifier
	Args []TypeExpr
}

// =============================================================================
// Interface Implementations
// =============================================================================

// -----------------------------------------------------------------------------
// NamedTypeExpr
// -----------------------------------------------------------------------------

func (n *NamedTypeExpr) Pos() Position { return n.Pos_ }

func (n *NamedTypeExpr) End() Position { return n.End_ }

func (n *NamedTypeExpr) String() string {
	return n.Name.String()
}

func (n *NamedTypeExpr) typeNode() {}

// -----------------------------------------------------------------------------
// ArrayTypeExpr
// -----------------------------------------------------------------------------

func (a *ArrayTypeExpr) Pos() Position { return a.Pos_ }

func (a *ArrayTypeExpr) End() Position { return a.End_ }

func (a *ArrayTypeExpr) String() string {
	return fmt.Sprintf(
		"[%d]%s",
		a.Size,
		a.Base.String(),
	)
}

func (a *ArrayTypeExpr) typeNode() {}

// -----------------------------------------------------------------------------
// SliceTypeExpr
// -----------------------------------------------------------------------------

func (s *SliceTypeExpr) Pos() Position { return s.Pos_ }

func (s *SliceTypeExpr) End() Position { return s.End_ }

func (s *SliceTypeExpr) String() string {
	return fmt.Sprintf("[]%s", s.Base.String())
}

func (s *SliceTypeExpr) typeNode() {}

// -----------------------------------------------------------------------------
// StructTypeExpr
// -----------------------------------------------------------------------------

func (s *StructTypeExpr) Pos() Position { return s.Pos_ }

func (s *StructTypeExpr) End() Position { return s.End_ }

func (s *StructTypeExpr) String() string {
	var out bytes.Buffer

	out.WriteString("struct")

	if s.Name != "" {
		out.WriteString(" ")
		out.WriteString(s.Name)
	}

	out.WriteString(" {\n")

	for _, f := range s.Fields {
		out.WriteString("\t")
		out.WriteString(f.String())
		out.WriteString(",\n")
	}

	out.WriteString("}")

	return out.String()
}

func (s *StructTypeExpr) typeNode() {}

// -----------------------------------------------------------------------------
// StructTypeField
// -----------------------------------------------------------------------------

func (s *StructTypeField) String() string {
	return fmt.Sprintf("%s: %s", s.Name, s.Type.String())
}

// -----------------------------------------------------------------------------
// SumTypeExpr
// -----------------------------------------------------------------------------

func (s *SumTypeExpr) Pos() Position { return s.Pos_ }

func (s *SumTypeExpr) End() Position { return s.End_ }

func (s *SumTypeExpr) String() string {
	var out bytes.Buffer

	out.WriteString("sum ")
	out.WriteString(s.Name)

	if len(s.TypeArgs) > 0 {
		var args []string

		for _, arg := range s.TypeArgs {
			args = append(args, arg.String())
		}

		out.WriteString("<")
		out.WriteString(strings.Join(args, ", "))
		out.WriteString(">")
	}

	out.WriteString(" {\n")

	for _, variant := range s.Variants {
		out.WriteString("\t")
		out.WriteString(variant.String())
		out.WriteString(",\n")
	}

	out.WriteString("}")

	return out.String()
}

func (s *SumTypeExpr) typeNode() {}

// -----------------------------------------------------------------------------
// VariantTypeExpr
// -----------------------------------------------------------------------------

func (v *VariantTypeExpr) String() string {
	var out bytes.Buffer

	out.WriteString(v.Name)

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

// --------------------------------------------------------------------------
// Generic Type Expressions
// --------------------------------------------------------------------------
func (g *GenericTypeExpr) Pos() Position { return g.Pos_ }
func (g *GenericTypeExpr) End() Position { return g.End_ }
func (g *GenericTypeExpr) String() string {
	var parts []string

	for _, arg := range g.Args {
		parts = append(parts, arg.String())
	}

	return fmt.Sprintf("%s<%s>",
		g.Name.String(),
		strings.Join(parts, ", "),
	)
}
func (g *GenericTypeExpr) typeNode() {}
func (g *GenericTypeExpr) exprNode() {}
