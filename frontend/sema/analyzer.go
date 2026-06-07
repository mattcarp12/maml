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

// Analyze is the entry point for the semantic checker.
// It takes an AST and produces a TAST, along with any compile errors encountered.
func (a *Analyzer) Analyze(program *ast.Program) (*tast.Program, []ast.CompileError) {
	// 1. First Pass: Hoist declarations into the global scope
	// These functions are read-only AST passes.
	a.discoverTypes(program)
	a.resolveTypeBodies(program)
	a.registerFunctions(program)

	// 2. Second Pass: Type-check bodies and construct the TAST
	tastDecls := a.buildDeclarations(program)

	return &tast.Program{Decls: tastDecls}, a.errors
}

// =============================================================================
// Pass 1: Type Hoisting (set the table for Pass 2)
// =============================================================================

func (a *Analyzer) discoverTypes(program *ast.Program) {
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, exists := a.scope.types[td.Name.Value]; exists {
				a.errorf(td.Pos(), "type '%s' already defined", td.Name.Value)
				continue
			}
			switch td.Rhs.(type) {
			case *ast.StructTypeExpr:
				a.scope.types[td.Name.Value] = &types.StructType{Name: td.Name.Value}
			case *ast.SumTypeExpr:
				a.scope.types[td.Name.Value] = &types.SumType{BaseName: td.Name.Value}
			}
		}
	}
}

func (a *Analyzer) resolveTypeBodies(program *ast.Program) {
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			a.resolveTypeBody(td)
		}
	}
}

func (a *Analyzer) resolveTypeBody(v *ast.TypeDecl) {
	existingType := a.scope.types[v.Name.Value]

	switch rhs := v.Rhs.(type) {
	case *ast.StructTypeExpr:
		st := existingType.(*types.StructType)
		for _, f := range rhs.Fields {
			st.Fields = append(st.Fields, types.StructField{
				Name: f.Name,
				Type: a.resolveAstType(f.Type),
			})
		}
	case *ast.SumTypeExpr:
		st := existingType.(*types.SumType)
		for i, v := range rhs.Variants {
			variant := types.SumVariant{
				Name:         v.Name,
				Discriminant: i,
			}
			for _, f := range v.Fields {
				variant.Fields = append(variant.Fields, types.StructField{
					Name: f.Name,
					Type: a.resolveAstType(f.Type),
				})
			}
			for _, tf := range v.TupleFields {
				variant.TupleTypes = append(variant.TupleTypes, a.resolveAstType(tf))
			}
			st.Variants = append(st.Variants, variant)

			// Register the variant constructor in the symbol table
			a.scope.symbols[v.Name] = &types.Symbol{
				Kind:    types.VariantSymbol,
				Name:    v.Name,
				Type:    st,
				SumType: st,
				Variant: &st.Variants[i],
			}
		}
	}
}

// =============================================================================
// Pass 1.5: Function Hoisting
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
		case p.Mut:
			paramModes[i] = types.ParamMutBorrow
		default:
			paramModes[i] = types.ParamBorrow
		}
	}

	var returnType types.Type = types.UnitType{}
	if v.ReturnType != nil {
		returnType = a.resolveAstType(v.ReturnType)
		if v.IsAsync {
			returnType = types.FutureType{Base: returnType}
		}
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

// ==========================================================================
// Pass 2: Semantic checker
// ==========================================================================

func (a *Analyzer) buildDeclarations(program *ast.Program) []tast.Decl {
	var decls []tast.Decl
	for _, decl := range program.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			decls = append(decls, a.buildFnBody(d))
		case *ast.TypeDecl:
			decls = append(decls, a.buildTypeDecl(d))
		default:
			a.errorf(decl.Pos(), "unsupported declaration type: %T", decl)
		}
	}
	return decls
}

func (a *Analyzer) buildTypeDecl(td *ast.TypeDecl) *tast.TypeDecl {
	// 1. Retrieve the fully resolved type from Pass 1
	resolvedType := a.lookupCustomType(td.Name.Value)

	// 2. Create the permanently bound symbol for this type declaration
	sym := &types.Symbol{
		Kind: types.VarSymbol,
		Name: td.Name.Value,
		Type: resolvedType,
	}

	// 3. Construct the TAST node, completely discarding the AST Rhs tree
	return &tast.TypeDecl{
		Pos_: td.Pos_,
		End_: td.End_,
		Name: &tast.Identifier{
			Pos_:  td.Name.Pos_,
			Value: td.Name.Value,
			Type:  resolvedType,
		},
		Type:   resolvedType,
		Symbol: sym,
	}
}

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

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}
