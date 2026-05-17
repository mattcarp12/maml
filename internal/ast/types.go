package ast

import (
	"fmt"
	"strings"
)

// -----------------------------------------------------------------------------
// Type Nodes
// -----------------------------------------------------------------------------

// NamedType represents a simple type like 'int' or 'string'
type NamedType struct {
	Name string
	Pos_ Position
}

func (nt *NamedType) Pos() Position { return nt.Pos_ }
func (nt *NamedType) End() Position {
	return Position{Line: nt.Pos_.Line, Col: nt.Pos_.Col + len(nt.Name)}
}
func (nt *NamedType) String() string { return nt.Name }
func (nt *NamedType) typeNode()      {}

// GenericType represents a parameterized type like 'Result<User, string>'.
type GenericType struct {
	Name   string
	Params []TypeExpr
	Pos_   Position
}

func (g *GenericType) Pos() Position { return g.Pos_ }
func (g *GenericType) End() Position {
	if len(g.Params) > 0 {
		return g.Params[len(g.Params)-1].End() // Rough estimation for now
	}
	return g.Pos_
}
func (g *GenericType) String() string {
	var params []string
	for _, p := range g.Params {
		params = append(params, p.String())
	}
	return fmt.Sprintf("%s[%s]", g.Name, strings.Join(params, ", "))
}
func (g *GenericType) typeNode() {}

type SliceType struct {
	Base TypeExpr
	Pos_ Position
}

func (s *SliceType) Pos() Position  { return s.Pos_ }
func (s *SliceType) End() Position  { return s.Base.End() }
func (s *SliceType) String() string { return "[]" + s.Base.String() }
func (s *SliceType) typeNode()      {}

type ArrayType struct {
	Size int64
	Base TypeExpr
	Pos_ Position
}

func (a *ArrayType) Pos() Position { return a.Pos_ }
func (a *ArrayType) End() Position { return a.Base.End() }
func (a *ArrayType) String() string {
	return fmt.Sprintf("[%d]%s", a.Size, a.Base.String())
}
func (a *ArrayType) typeNode() {}
