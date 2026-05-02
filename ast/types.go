package ast

import (
	"fmt"
	"strings"
)

type TypeExpr interface {
	Node
	typeNode()
}

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
	return fmt.Sprintf("%s<%s>", g.Name, strings.Join(params, ", "))
}
func (g *GenericType) typeNode() {}
