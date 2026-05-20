// sema/function.go
package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
)

func (a *Analyzer) registerFunctions(program *ast.Program) {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.registerFunction(fn)
		}
	}
}

func (a *Analyzer) registerFunction(v *ast.FnDecl) {
	if v.Name == "main" && v.IsAsync {
		a.errorf(v.Pos(), "the 'main' function cannot be async; you must manually spawn tasks and run the executor")
	}
	paramTypes := make([]Type, len(v.Params))
	paramModes := make([]ParamMode, len(v.Params))
	for i, p := range v.Params {
		paramTypes[i] = a.resolveAstType(p.Type)
		switch {
		case p.Own:
			paramModes[i] = ParamOwned
		case p.Mut:
			paramModes[i] = ParamMutBorrow
		default:
			paramModes[i] = ParamBorrow
		}
	}

	var retType Type
	if v.ReturnType != nil {
		retType = a.resolveAstType(v.ReturnType)
	} else {
		retType = UnitType{}
	}

	// NEW: If the function is async, it returns a Task<T>, not T.
	if v.IsAsync {
		retType = TaskType{Base: retType}
	}

	a.scope.symbols[v.Name] = &Symbol{
		Kind:    FuncSymbol,
		Name:    v.Name,
		Mutable: false,
		Type:    &FunctionType{Params: paramTypes, ParamModes: paramModes, Return: retType},
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
	a.currentFn = v
	defer func() { a.currentFn = nil }()

	for _, param := range v.Params {
		pType := a.resolveAstType(param.Type)

		var mode ParamMode
		var mutable bool

		switch {
		case param.Own:
			mode = ParamOwned
			mutable = true

		case param.Mut:
			mode = ParamMutBorrow
			mutable = true

		default:
			mode = ParamBorrow
			mutable = false
		}

		a.scope.symbols[param.Name] = &Symbol{
			Kind:      VarSymbol,
			Name:      param.Name,
			Mutable:   mutable,
			Type:      pType,
			ParamMode: mode,
		}
	}

	if v.ReturnType != nil {
		a.expectedReturn = a.resolveAstType(v.ReturnType)
	} else {
		a.expectedReturn = UnitType{}
	}

	defer func() { a.expectedReturn = nil }()

	a.analyzeBlockStmt(v.Body)
}
