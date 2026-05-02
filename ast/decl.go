package ast

import (
	"bytes"
	"fmt"
	"strings"
)

// -----------------------------------------------------------------------------
// Root Node
// -----------------------------------------------------------------------------

// Program is the root of the AST.
type Program struct {
	Decls []Decl
}

func (p *Program) Pos() Position {
	if len(p.Decls) > 0 {
		return p.Decls[0].Pos()
	}
	return Position{Line: 1, Col: 0}
}
func (p *Program) End() Position {
	if len(p.Decls) > 0 {
		return p.Decls[len(p.Decls)-1].End()
	}
	return Position{Line: 1, Col: 0}
}
func (p *Program) String() string {
	var out bytes.Buffer
	for _, d := range p.Decls {
		out.WriteString(d.String())
		out.WriteString("\n")
	}
	return out.String()
}

// -----------------------------------------------------------------------------
// Declarations
// -----------------------------------------------------------------------------

type Param struct {
	Name string
	Type TypeExpr
	Pos_ Position
}

func (p *Param) String() string { return fmt.Sprintf("%s: %s", p.Name, p.Type.String()) }

// FnDecl represents a function definition.
type FnDecl struct {
	Name       string
	Params     []Param
	ReturnType TypeExpr
	Body       *BlockStmt
	IsAsync    bool
	Pos_       Position
}

func (f *FnDecl) Pos() Position { return f.Pos_ }
func (f *FnDecl) End() Position { return f.Body.End() }
func (f *FnDecl) String() string {
	var params []string
	for _, p := range f.Params {
		params = append(params, p.String())
	}
	
	prefix := "fn"
	if f.IsAsync {
		prefix = "async fn"
	}

	return fmt.Sprintf("%s %s(%s) %s %s",
		prefix, f.Name, strings.Join(params, ", "), f.ReturnType.String(), f.Body.String())
}
func (f *FnDecl) declNode() {}