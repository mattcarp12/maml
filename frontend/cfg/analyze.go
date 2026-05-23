package cfg

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

type Result struct {
	AlwaysReturns bool
	Errors        []ast.CompileError
	Warnings      []ast.CompileError
}

func Analyze(fn *ast.FnDecl, g *CFG) *Result {
	res := &Result{
		AlwaysReturns: AlwaysReturns(g),
		Errors:        []ast.CompileError{},
		Warnings:      []ast.CompileError{},
	}

	// Determine if the function has a unit return type.
	isUnit := false
	if fn.ReturnType == nil || fn.ReturnType.String() == "unit" {
		isUnit = true
	}

	// 1. Missing Return Policy
	// If the function expects a return value but control flow can fall through,
	// generate a fatal compilation error.
	if !isUnit && !res.AlwaysReturns {
		res.Errors = append(res.Errors, ast.CompileError{
			Stage: "CFG",
			Pos:   fn.End(),
			Msg:   fmt.Sprintf("missing return at end of function '%s'", fn.Name),
		})
	}

	// 2. Strict Dead Code Policy (Go-style)
	// Fetch all unreachable nodes in the control flow graph.
	deadNodes := UnreachableStatements(g) // [2]

	// Convert every piece of dead code into a fatal compilation error.
	for _, node := range deadNodes {
		res.Errors = append(res.Errors, ast.CompileError{
			Stage: "CFG",
			Pos:   node.Pos(), // [3]
			Msg:   "unreachable code",
		})
	}

	return res
}

type Analyzer struct {
	typeMap map[ast.Node]sema.Type
}

func NewAnalyzer(typeMap map[ast.Node]sema.Type) *Analyzer {
	return &Analyzer{typeMap: typeMap}
}

// Analyze performs control flow analysis on all functions in the program.
// It returns a mapping of functions to their CFGs, a mapping of functions to
// their analysis results, and a flattened slice of all control flow errors.
func (a *Analyzer) Analyze(program *ast.Program) (map[*ast.FnDecl]*CFG, map[*ast.FnDecl]*Result, []ast.CompileError) {
	cfgs := make(map[*ast.FnDecl]*CFG)
	results := make(map[*ast.FnDecl]*Result)
	var allErrors []ast.CompileError

	for _, decl := range program.Decls {
		// We only care about function declarations for control flow
		fn, ok := decl.(*ast.FnDecl)
		if !ok {
			continue
		}

		// 1. Build the Control Flow Graph for this specific function
		g := Build(fn)
		cfgs[fn] = g

		// 2. Initialize the result struct for this function
		res := &Result{
			AlwaysReturns: AlwaysReturns(g),
			Errors:        []ast.CompileError{},
			Warnings:      []ast.CompileError{},
		}

		// Determine if the function has a unit return type.
		isUnit := false
		if fn.ReturnType == nil || fn.ReturnType.String() == "unit" {
			isUnit = true
		}

		// 3. Missing Return Policy
		// If the function expects a return value but control flow can fall through,
		// generate a fatal compilation error.
		if !isUnit && !res.AlwaysReturns {
			err := ast.CompileError{
				Stage: "CFG",
				Pos:   fn.End(),
				Msg:   fmt.Sprintf("missing return at end of function '%s'", fn.Name),
			}
			res.Errors = append(res.Errors, err)
		}

		// 4. Strict Dead Code Policy (Go-style)
		// Fetch all unreachable nodes in the control flow graph.
		deadNodes := UnreachableStatements(g)

		// Convert every piece of dead code into a fatal compilation error.
		for _, node := range deadNodes {
			err := ast.CompileError{
				Stage: "CFG",
				Pos:   node.Pos(),
				Msg:   "unreachable code",
			}
			res.Errors = append(res.Errors, err)
		}

		// Save the results and aggregate the errors for the compiler driver
		results[fn] = res
		allErrors = append(allErrors, res.Errors...)
	}

	return cfgs, results, allErrors
}
