package tast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Program Root
// =============================================================================

type Program struct {
	Decls []Decl
}

// =============================================================================
// Function Declarations
// =============================================================================

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
	ReturnType types.Type // Permanently bound semantic return type
	Body       *BlockStmt
	IsAsync    bool
	Symbol     *types.Symbol // Permanently bound function symbol
}

// =============================================================================
// Type Declarations
// =============================================================================

type TypeDecl struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Name   *Identifier
	Type   types.Type    // The fully resolved types.Type (replaces unresolved TypeExpr)
	Symbol *types.Symbol // Permanently bound semantic type symbol
}

// =============================================================================
// Interface Implementations
// =============================================================================

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
	for _, d := range p.Decls {
		out.WriteString(d.String())
		out.WriteString("\n")
	}
	return out.String()
}

func (p *Param) Pos() Position { return p.Pos_ }
func (p *Param) End() Position { return p.End_ }
func (p *Param) String() string {
	// Relying strictly on the Symbol's ParamMode (0=Borrow, 1=MutBorrow, 2=Owned)
	prefix := ""
	if p.Symbol != nil {
		switch p.Symbol.ParamMode {
		case types.ParamMutBorrow:
			prefix = "mut "
		case types.ParamOwned:
			prefix = "own "
		}
	}
	return fmt.Sprintf("%s%s %s", prefix, p.Name, p.Type.String())
}

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

	var params []string
	for _, p := range f.Params {
		params = append(params, p.String())
	}
	out.WriteString(strings.Join(params, ", "))
	out.WriteString(") ")

	if f.ReturnType != nil {
		out.WriteString(f.ReturnType.String())
		out.WriteString(" ")
	}
	if f.Body != nil {
		out.WriteString(f.Body.String())
	}
	return out.String()
}
func (f *FnDecl) declNode() {}

func (t *TypeDecl) Pos() Position { return t.Pos_ }
func (t *TypeDecl) End() Position { return t.End_ }
func (t *TypeDecl) String() string {
	return fmt.Sprintf("type %s = %s", t.Name.String(), t.Type.String())
}
func (t *TypeDecl) declNode() {}
