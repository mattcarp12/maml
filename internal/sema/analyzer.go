// sema/analyzer.go
package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/internal/ast"
)

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	TypeMap        map[ast.Node]Type
	expectedReturn Type
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

func (a *Analyzer) Analyze(program *ast.Program) ([]ast.CompileError, map[ast.Node]Type) {
	// PASS 1: Type Discovery
	a.discoverTypes(program)

	// PASS 2: Type Resolution
	a.resolveTypeBodies(program)

	// PASS 3: Function Signatures
	a.registerFunctions(program)

	// PASS 4: Function Bodies
	a.analyzeFunctionBodies(program)

	return a.errors, a.TypeMap
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}
