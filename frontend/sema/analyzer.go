package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	expectedReturn types.Type
	currentFn      *ast.FnDecl
}

func New() *Analyzer {
	return &Analyzer{
		scope:  newGlobalScope(),
		errors: []ast.CompileError{},
	}
}

// Analyze performs type checking and mathematically builds the Typed AST (TAST).
func (a *Analyzer) Analyze(program *ast.Program) (*tast.Program, []ast.CompileError) {
	// 1. First Pass: Hoist declarations into the global scope
	// These functions (in type_resolution.go and function.go) remain read-only AST passes.
	a.discoverTypes(program)
	a.resolveTypeBodies(program)
	a.registerFunctions(program)

	// 2. Second Pass: Type-check bodies and construct the TAST
	tastDecls := a.buildDeclarations(program)

	tastProg := &tast.Program{
		Decls: tastDecls,
	}

	return tastProg, a.errors
}

// buildDeclarations replaces the old analyzeFunctionBodies.
// It routes AST declarations to their respective TAST builders.
func (a *Analyzer) buildDeclarations(program *ast.Program) []tast.Decl {
	var decls []tast.Decl

	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			decls = append(decls, a.buildFnBody(fn))
		} else if td, ok := decl.(*ast.TypeDecl); ok {
			decls = append(decls, a.buildTypeDecl(td))
		}
	}

	return decls
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}
