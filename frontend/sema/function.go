package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Pass 1: Function Hoisting
// =============================================================================

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

	paramTypes := make([]types.Type, len(v.Params))
	paramModes := make([]types.ParamMode, len(v.Params))

	for i, p := range v.Params {
		paramTypes[i] = a.resolveAstType(p.Type)
		switch {
		case p.Own:
			paramModes[i] = types.ParamOwned
		case p.Mut:
			paramModes[i] = types.ParamMutBorrow
		default:
			paramModes[i] = types.ParamBorrow
		}
	}

	var returnType types.Type = types.UnitType{}
	if v.ReturnType != nil {
		returnType = a.resolveAstType(v.ReturnType)
	}

	fnType := &types.FunctionType{
		Params:     paramTypes,
		ParamModes: paramModes,
		Return:     returnType,
	}

	sym := &types.Symbol{
		Kind: types.FuncSymbol,
		Name: v.Name,
		Type: fnType,
	}

	a.scope.symbols[v.Name] = sym
}

// =============================================================================
// Pass 2: TAST Construction
// =============================================================================

// buildFnBody replaces the old analyzeFnBody.
// It type-checks the function parameters and body, returning a fully-hydrated TAST node.
func (a *Analyzer) buildFnBody(v *ast.FnDecl) *tast.FnDecl {
	a.currentFn = v
	defer func() { a.currentFn = nil }()

	// 1. Retrieve the permanently bound function symbol from Pass 1
	fnSym := a.resolve(v.Name)
	fnType := fnSym.Type.(*types.FunctionType)

	// Set the expected return type so inner ReturnStmts can validate against it
	a.expectedReturn = fnType.Return

	// 2. Prepare the local scope for the function body
	a.pushScope()
	defer a.popScope()

	// 3. Hydrate the parameters and inject them into the local scope
	tastParams := make([]*tast.Param, len(v.Params))
	for i, p := range v.Params {
		paramSym := &types.Symbol{
			Kind:      types.VarSymbol,
			Name:      p.Name,
			Type:      fnType.Params[i],
			Mutable:   p.Mut,
			ParamMode: fnType.ParamModes[i],
		}

		a.scope.symbols[p.Name] = paramSym

		tastParams[i] = &tast.Param{
			Pos_:   p.Pos_,
			End_:   p.End_,
			Name:   p.Name,
			Type:   fnType.Params[i],
			Symbol: paramSym,
		}
	}

	// 4. Construct the strictly-typed block statement (the body)
	var tastBody *tast.BlockStmt
	if v.Body != nil {
		tastBody = a.buildBlockStmt(v.Body)
	}

	// 5. Return the fully-hydrated TAST Function Declaration
	return &tast.FnDecl{
		Pos_:       v.Pos_,
		End_:       v.End_,
		Name:       v.Name,
		Params:     tastParams,
		ReturnType: fnType.Return,
		Body:       tastBody,
		IsAsync:    v.IsAsync,
		Symbol:     fnSym,
	}
}
