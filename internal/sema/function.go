// sema/function.go
package sema

import "github.com/mattcarp12/maml/internal/ast"

func (a *Analyzer) registerFunctions(program *ast.Program) {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.registerFunction(fn)
		}
	}
}

func (a *Analyzer) registerFunction(v *ast.FnDecl) {
	paramTypes := make([]Type, len(v.Params))
	for i, p := range v.Params {
		paramTypes[i] = a.resolveAstType(p.Type)
	}
	retType := a.resolveAstType(v.ReturnType)

	a.scope.symbols[v.Name] = &Symbol{
		Kind:    FuncSymbol,
		Name:    v.Name,
		Mutable: false,
		Type:    &FunctionType{Params: paramTypes, Return: retType},
	}
}

func (a *Analyzer) analyzeFunctionBodies(program *ast.Program) {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.analyzeFnBody(fn)
		}
	}
}

func (a *Analyzer) analyzeFnBody(v *ast.FnDecl) {
	a.pushScope()
	defer a.popScope()

	// Bind parameters
	for _, param := range v.Params {
		pType := a.resolveAstType(param.Type)
		a.scope.symbols[param.Name] = &Symbol{
			Kind:    VarSymbol,
			Name:    param.Name,
			Mutable: false,
			Type:    pType,
		}
	}

	a.expectedReturn = a.resolveAstType(v.ReturnType)
	defer func() { a.expectedReturn = nil }()

	alwaysReturns := a.analyzeBlockStmt(v.Body)
	if !alwaysReturns {
		a.errorf(v.Pos(), "function '%s' is missing a return statement", v.Name)
	}
}