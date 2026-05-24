package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// =============================================================================
// Program Root
// =============================================================================

// Program is the root AST node for a source file or compilation unit.
type Program struct {
	Decls []Decl `json:"decls"`
}

// =============================================================================
// Function Declarations
// =============================================================================

// Param represents a function parameter.
type Param struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name string   `json:"name"`
	Type TypeExpr `json:"type"`

	Mut bool `json:"mut"`
	Own bool `json:"own"`
}

// FnDecl represents a function declaration.
//
// Example:
//
//	fn add(a: int, b: int) -> int {
//	    return a + b
//	}
type FnDecl struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name       string     `json:"name"`
	IsAsync    bool       `json:"is_async"`
	Params     []*Param   `json:"params"`
	ReturnType TypeExpr   `json:"return_type,omitempty"`
	Body       *BlockStmt `json:"body"`
}

// =============================================================================
// Type Declarations
// =============================================================================

// TypeDecl represents a named type declaration.
//
// Example:
//
//	type Point = struct {
//	    x: int,
//	    y: int,
//	}
type TypeDecl struct {
	Pos_ Position `json:"-"`
	End_ Position `json:"-"`

	Name *Identifier `json:"name"`
	Rhs  TypeExpr    `json:"rhs"`
}

// =============================================================================
// Interface Implementations
// =============================================================================

// -----------------------------------------------------------------------------
// Program
// -----------------------------------------------------------------------------

func (p *Program) Pos() Position {
	if len(p.Decls) == 0 {
		return Position{}
	}

	return p.Decls[0].Pos()
}

func (p *Program) End() Position {
	if len(p.Decls) == 0 {
		return Position{}
	}

	return p.Decls[len(p.Decls)-1].End()
}

func (p *Program) String() string {
	var out bytes.Buffer

	for i, d := range p.Decls {
		out.WriteString(d.String())

		if i != len(p.Decls)-1 {
			out.WriteString("\n\n")
		}
	}

	return out.String()
}

// -----------------------------------------------------------------------------
// Param
// -----------------------------------------------------------------------------

func (p *Param) String() string {
	var prefixes []string

	if p.Mut {
		prefixes = append(prefixes, "mut")
	}

	if p.Own {
		prefixes = append(prefixes, "own")
	}

	prefix := ""

	if len(prefixes) > 0 {
		prefix = strings.Join(prefixes, " ") + " "
	}

	if p.Type == nil {
		return prefix + p.Name
	}

	return fmt.Sprintf(
		"%s%s: %s",
		prefix,
		p.Name,
		p.Type.String(),
	)
}

func (p *Param) Pos() Position { return p.Pos_ }
func (p *Param) End() Position { return p.End_ }

// -----------------------------------------------------------------------------
// FnDecl
// -----------------------------------------------------------------------------

func (f *FnDecl) Pos() Position { return f.Pos_ }

func (f *FnDecl) End() Position { return f.End_ }

func (f *FnDecl) String() string {
	var out bytes.Buffer

	if f.IsAsync {
		out.WriteString("async ")
	}

	out.WriteString("fn ")
	out.WriteString(f.Name)
	out.WriteString("(")

	for i, p := range f.Params {
		out.WriteString(p.String())

		if i != len(f.Params)-1 {
			out.WriteString(", ")
		}
	}

	out.WriteString(")")

	if f.ReturnType != nil {
		out.WriteString(" -> ")
		out.WriteString(f.ReturnType.String())
	}

	out.WriteString(" ")
	out.WriteString(f.Body.String())

	return out.String()
}

func (f *FnDecl) declNode() {}

// -----------------------------------------------------------------------------
// TypeDecl
// -----------------------------------------------------------------------------

func (t *TypeDecl) Pos() Position { return t.Pos_ }

func (t *TypeDecl) End() Position { return t.End_ }

func (t *TypeDecl) String() string {
	return fmt.Sprintf(
		"type %s = %s",
		t.Name.String(),
		t.Rhs.String(),
	)
}

func (t *TypeDecl) declNode() {}