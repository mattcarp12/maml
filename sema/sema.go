package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/ast"
)

type Scope struct {
	parent  *Scope
	symbols map[string]*Symbol
	types   map[string]ast.TypeExpr
}

type Symbol struct {
	Name    string
	Mutable bool
	// For V1, resolving types to a string representation (e.g., "int") is easiest.
	// Later, this will point to a concrete semantic `Type` struct.
	Type string
}

type Analyzer struct {
	scope  *Scope
	errors []string
}

func New() *Analyzer {
	return &Analyzer{
		errors: []string{},
	}
}

// errorf is our standardized compiler error formatter
func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	a.errors = append(a.errors, fmt.Sprintf("Line %d:%d - %s", pos.Line, pos.Col, msg))
}

func (a *Analyzer) Analyze(program *ast.Program) []string {
	a.pushScope() // Push the Global Scope

	// PASS 1: Register all top-level functions so they can call each other
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.scope.symbols[fn.Name] = &Symbol{
				Name:    fn.Name,
				Mutable: false,
				Type:    "function", // We will refine function signatures later
			}
		}
	}

	// PASS 2: Analyze the bodies
	for _, decl := range program.Decls {
		a.analyzeDecl(decl)
	}

	a.popScope()
	return a.errors
}

// -----------------------------------------------------------------------------
// Scope Management
// -----------------------------------------------------------------------------

func (a *Analyzer) pushScope() {
	a.scope = &Scope{
		parent:  a.scope,
		symbols: make(map[string]*Symbol),
	}
}

func (a *Analyzer) popScope() {
	a.scope = a.scope.parent
}

// resolve walks up the scope chain looking for a variable
func (a *Analyzer) resolve(name string) *Symbol {
	for s := a.scope; s != nil; s = s.parent {
		if sym, ok := s.symbols[name]; ok {
			return sym
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// AST Traversal
// -----------------------------------------------------------------------------

func (a *Analyzer) analyzeDecl(decl ast.Decl) {
	switch v := decl.(type) {
	case *ast.FnDecl:
		a.pushScope() // Create a new scope for the function body

		// Load parameters into the local scope!
		for _, param := range v.Params {
			a.scope.symbols[param.Name] = &Symbol{
				Name:    param.Name,
				Mutable: false,                            // In MAML, parameters are immutable by default
				Type:    param.Type.(*ast.NamedType).Name, // e.g., "int"
			}
		}

		a.analyzeBlockStmt(v.Body)
		a.popScope()

	case *ast.TypeDecl:
		// check if type name is already taken
		if _, ok := a.scope.types[v.Name.Name]; ok {
			a.errorf(v.Pos(), "type %s already defined", v.Name.Name)
			return
		}
		a.scope.types[v.Name.Name] = v.Rhs

	default:
		a.errorf(decl.Pos(), "declaration type not recognized")
	}
}

func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) string {
	for _, stmt := range body.Statements {
		stmtType := a.analyzeStmt(stmt)
		switch stmt.(type) {
		case *ast.ReturnStmt, *ast.YieldStmt:
			return stmtType
		}
	}
	return "void"
}

func (a *Analyzer) analyzeStmt(stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		// 1. Analyze the right side to figure out what type we are assigning
		exprType := a.analyzeExpr(s.Value)

		// 2. MAML strict rule: No shadowing in the same scope
		if _, exists := a.scope.symbols[s.Name]; exists {
			a.errorf(s.Pos(), "variable '%s' is already declared in this block", s.Name)
			return "unknown"
		}

		// 3. Register the variable in the current scope
		a.scope.symbols[s.Name] = &Symbol{
			Name:    s.Name,
			Mutable: s.Mutable,
			Type:    exprType,
		}

		return exprType

	case *ast.ReturnStmt:
		// Just ensure the expression being returned is valid
		exprType := a.analyzeExpr(s.Value)
		// TODO: Compare this expression's type against the Function's ReturnType
		return exprType

	case *ast.YieldStmt:
		exprType := a.analyzeExpr(s.Value)
		return exprType

	default:
		a.errorf(stmt.Pos(), "statement type not recognized")
		return "unknown"
	}
}

// analyzeExpr evaluates an expression and returns its computed type
func (a *Analyzer) analyzeExpr(expr ast.Expr) string {
	switch e := expr.(type) {

	case *ast.IntLiteral:
		return "int"

	case *ast.BoolLiteral:
		return "bool"

	case *ast.Identifier:
		sym := a.resolve(e.Value)
		if sym == nil {
			a.errorf(e.Pos(), "undefined variable '%s'", e.Value)
			return "unknown"
		}
		return sym.Type

	case *ast.InfixExpr:
		leftType := a.analyzeExpr(e.Left)
		rightType := a.analyzeExpr(e.Right)

		// MAML strict rule: You can only do math on identical types
		if leftType != "unknown" && rightType != "unknown" && leftType != rightType {
			a.errorf(e.Pos(), "type mismatch: cannot %s '%s' and '%s'", e.Operator, leftType, rightType)
		}

		// A math operation between two ints results in an int
		return leftType

	case *ast.IfExpr:
		// 1. The condition must evaluate to a boolean
		condType := a.analyzeExpr(e.Condition)
		if condType != "bool" && condType != "unknown" {
			a.errorf(e.Pos(), "IF condition must be a boolean, got '%s'", condType)
		}

		// 2. Analyze the 'true' branch
		thenType := a.analyzeBlockStmt(e.Consequence)

		// 3. Analyze the 'false' branch
		if e.Alternative != nil {
			elseType := a.analyzeBlockStmt(e.Alternative)

			// If both branches exist, they MUST match types to be used as an expression
			if thenType != "unknown" && elseType != "unknown" && thenType != elseType {
				a.errorf(e.Pos(), "if/else branches have incompatible yield types: '%s' and '%s'", thenType, elseType)
			}
			return thenType
		}

		// If there is no else block, it yields nothing (void)
		return "void"

	case *ast.CallExpr:
		// 1. Ensure the function being called is actually an identifier
		ident, ok := e.Function.(*ast.Identifier)
		if !ok {
			a.errorf(e.Pos(), "cannot call non-function")
			return "unknown"
		}

		// 2. Resolve the function name in the global scope
		sym := a.resolve(ident.Value)
		if sym == nil || sym.Type != "function" {
			a.errorf(e.Pos(), "undefined function '%s'", ident.Value)
			return "unknown"
		}

		// 3. Analyze all the arguments being passed in
		for _, arg := range e.Arguments {
			a.analyzeExpr(arg)
			// TODO later: compare arg types to the function's parameter types!
		}

		// For now, assume all functions return "int" (Tracer bullet)
		return "int"

	default:
		a.errorf(expr.Pos(), "expression type not recognized")
		return "unknown"
	}
}
