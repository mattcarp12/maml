package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
)

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	TypeMap        map[ast.Node]Type
	expectedReturn Type
	currentFn      *ast.FnDecl
}

func New() *Analyzer {
	globalScope := newGlobalScope()

	return &Analyzer{
		scope:          globalScope,
		errors:         []ast.CompileError{},
		TypeMap:        make(map[ast.Node]Type),
		expectedReturn: nil,
	}
}

func (a *Analyzer) Analyze(program *ast.Program) (map[ast.Node]Type, []ast.CompileError) {
	a.discoverTypes(program)
	a.resolveTypeBodies(program)
	a.registerFunctions(program)
	a.analyzeFunctionBodies(program)
	return a.TypeMap, a.errors
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}
