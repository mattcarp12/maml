package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Analyzer Core
// =============================================================================

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	expectedReturn types.Type
	currentFn      *ast.FnDecl
	registry       *Registry
	allowAsyncCall bool
}

func New() *Analyzer {
	return &Analyzer{
		scope:    newGlobalScope(),
		errors:   []ast.CompileError{},
		registry: newRegistry(),
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
	// This delegates to our mapper.go architecture, firing the registry rules.
	tastDecls := a.buildDeclarations(program)

	return &tast.Program{Decls: tastDecls}, a.errors
}

// errorf appends a formatting string to the analyzer's error list.
// In the new architecture, rules do not call this directly; they return Violations,
// which mapper.go drains into errorf.
func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
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
	paramTypes := make([]types.Type, len(v.Params))
	caps := make([]types.Cap, len(v.Params)) // Using strings instead of ParamModes

	for i, p := range v.Params {
		paramTypes[i] = a.resolveAstType(p.Type)
		caps[i] = types.Cap(p.Cap) // Map the unified capability string directly!
	}

	var returnType types.Type = types.UnitType{}
	if v.ReturnType != nil {
		returnType = a.resolveAstType(v.ReturnType)
	}

	if v.IsAsync {
		returnType = &types.FutureType{Base: returnType}
	}

	fnType := &types.FunctionType{
		Params: paramTypes,
		Caps:   caps, // Inject the capabilities into the type signature
		Return: returnType,
	}

	sym := &types.Symbol{
		Kind: types.FuncSymbol,
		Name: v.Name,
		Type: fnType,
	}

	a.scope.symbols[v.Name] = sym
}
