package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/ast"
)

type Scope struct {
	parent  *Scope
	symbols map[string]*Symbol
}

type Symbol struct {
	Name    string
	Mutable bool
	// For V1, resolving types to a string representation (e.g., "int") is easiest.
	// Later, this will point to a concrete semantic `Type` struct.
	Type    string 
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
		symbols: make(map[string]*Symbol), // Critical fix: map must be initialized!
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
		
		// TODO: Add function parameters to this new scope here so the body can see them

		a.analyzeBlockStmt(v.Body)
		a.popScope()
	default:
		a.errorf(decl.Pos(), "declaration type not recognized")
	}
}

func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) {
	for _, stmt := range body.Statements {
		a.analyzeStmt(stmt)
	}
}

func (a *Analyzer) analyzeStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		// 1. Analyze the right side to figure out what type we are assigning
		exprType := a.analyzeExpr(s.Value)

		// 2. MAML strict rule: No shadowing in the same scope
		if _, exists := a.scope.symbols[s.Name]; exists {
			a.errorf(s.Pos(), "variable '%s' is already declared in this block", s.Name)
			return
		}

		// 3. Register the variable in the current scope
		a.scope.symbols[s.Name] = &Symbol{
			Name:    s.Name,
			Mutable: s.Mutable,
			Type:    exprType,
		}

	case *ast.ReturnStmt:
		// Just ensure the expression being returned is valid
		_ = a.analyzeExpr(s.Value)
		// TODO: Compare this expression's type against the Function's ReturnType

	default:
		a.errorf(stmt.Pos(), "statement type not recognized")
	}
}

// analyzeExpr evaluates an expression and returns its computed type
func (a *Analyzer) analyzeExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	
	case *ast.IntLiteral:
		return "int"

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

	default:
		a.errorf(expr.Pos(), "expression type not recognized")
		return "unknown"
	}
}