package hir

import (
	"bytes"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
)

type Param struct {
	Pos_   Position
	Name   string
	Type   types.Type
	Symbol *types.Symbol
}

type FnDecl struct {
	Pos_       Position
	Name       string
	Params     []*Param
	ReturnType types.Type
	Body       *BlockStmt
	IsAsync    bool
	Symbol     *types.Symbol
}

type TypeDecl struct {
	Pos_   Position
	Name   *Identifier
	Type   types.Type
	Symbol *types.Symbol
}

func (p *Param) Pos() Position  { return p.Pos_ }
func (p *Param) String() string { return p.Name }

func (f *FnDecl) Pos() Position { return f.Pos_ }
func (f *FnDecl) declNode()     {}
func (f *FnDecl) String() string {
	var out bytes.Buffer
	out.WriteString("fn ")
	out.WriteString(f.Name)
	out.WriteString("(")
	var params []string
	for _, p := range f.Params {
		params = append(params, p.String())
	}
	out.WriteString(strings.Join(params, ", "))
	out.WriteString(") ")
	if f.Body != nil {
		out.WriteString(f.Body.String())
	}
	return out.String()
}

func (t *TypeDecl) Pos() Position  { return t.Pos_ }
func (t *TypeDecl) declNode()      {}
func (t *TypeDecl) String() string { return "type " + t.Name.String() }
