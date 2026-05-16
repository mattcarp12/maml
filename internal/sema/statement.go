// sema/statement.go
package sema

import "github.com/mattcarp12/maml/internal/ast"

// Returns true if the block guarantees a return on all paths
func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) bool {
	a.pushScope()
	defer a.popScope()

	alwaysReturns := false
	for i, stmt := range body.Statements {
		if alwaysReturns {
			// Any statement after a guaranteed return is unreachable
			a.errorf(stmt.Pos(), "unreachable code after return statement")
			// Still analyze it for other errors, but don't change alwaysReturns
			a.analyzeStmt(stmt)
			continue
		}
		if a.analyzeStmt(stmt) {
			alwaysReturns = true
			// Check if there are more statements after this one
			if i < len(body.Statements)-1 {
				// We'll catch them in the next iterations
			}
		}
	}
	return alwaysReturns
}

// Returns true if the statement guarantees a return on all paths
func (a *Analyzer) analyzeStmt(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		return a.analyzeDeclareStmt(s)
	case *ast.AssignStmt:
		return a.analyzeAssignStmt(s)
	case *ast.ReturnStmt:
		return a.analyzeReturnStmt(s)
	case *ast.YieldStmt:
		a.analyzeExpr(s.Value)
		return false
	case *ast.ExprStmt:
		a.analyzeExpr(s.Value)
		return false
	case *ast.ForStmt:
		return a.analyzeForStmt(s)
	}
	return false
}

func (a *Analyzer) analyzeDeclareStmt(s *ast.DeclareStmt) bool {
	exprType := a.analyzeExpr(s.Value)
	if _, exists := a.scope.symbols[s.Name]; exists {
		a.errorf(s.Pos(), "variable '%s' is already declared", s.Name)
		return false
	}
	a.scope.symbols[s.Name] = &Symbol{
		Kind:    VarSymbol,
		Name:    s.Name,
		Mutable: s.Mutable,
		Type:    exprType,
	}
	return false
}

func (a *Analyzer) analyzeAssignStmt(s *ast.AssignStmt) bool {
	lvalType := a.analyzeExpr(s.LValue)
	rvalType := a.analyzeExpr(s.RValue)

	if !lvalType.Equals(UnknownType{}) && !rvalType.Equals(UnknownType{}) && !lvalType.Equals(rvalType) {
		a.errorf(s.Pos(), "type mismatch: cannot assign '%s' to '%s'",
			rvalType.String(), lvalType.String())
	}

	// --- NEW MUTABILITY ENFORCEMENT ---

	// RULE 1: The String Data Rule
	// If the user is trying to index into a string (e.g., s[0] = 'a')
	if indexExpr, ok := s.LValue.(*ast.IndexExpr); ok {
		leftObjType := a.analyzeExpr(indexExpr.Left)
		if leftObjType.Equals(StringType{}) {
			a.errorf(s.Pos(), "strings are immutable and cannot be modified by index")
			return false // Return early to prevent cascading errors
		}
	}

	// RULE 2: The General Mutability Rule
	// Trace the left-hand side down to its root variable name
	rootIdent := a.getRootIdentifier(s.LValue)

	if rootIdent != nil {
		// We found the base variable! Use your existing resolve logic.
		sym := a.resolve(rootIdent.Value)
		if sym != nil && !sym.Mutable {
			a.errorf(s.Pos(), "cannot mutate immutable variable '%s'", rootIdent.Value)
		}
	} else {
		// Optional: If rootIdent is nil, they might be trying to assign to a literal (e.g. `10 = 5`)
		a.errorf(s.Pos(), "cannot assign to non-variable expression")
	}

	return false
}

func (a *Analyzer) analyzeReturnStmt(s *ast.ReturnStmt) bool {
	retType := a.analyzeExpr(s.Value)

	if a.expectedReturn != nil && !a.expectedReturn.Equals(UnknownType{}) {
		if !retType.Equals(a.expectedReturn) && !retType.Equals(UnknownType{}) {
			a.errorf(s.Pos(), "type mismatch: expected return type '%s', got '%s'",
				a.expectedReturn.String(), retType.String())
		}
	}
	return true
}

func (a *Analyzer) analyzeForStmt(s *ast.ForStmt) bool {
	if s.Init != nil {
		a.analyzeStmt(s.Init)
	}

	// Detect infinite loop: `for (true) { ... }`
	isInfiniteLoop := false
	if s.Condition != nil {
		condType := a.analyzeExpr(s.Condition)
		if !condType.Equals(BoolType{}) && !condType.Equals(UnknownType{}) {
			a.errorf(s.Condition.Pos(), "condition must be of type 'bool', got '%s'", condType.String())
		}
		if lit, ok := s.Condition.(*ast.BoolLiteral); ok && lit.Value {
			isInfiniteLoop = true
		}
	} else {
		// No condition means unconditional loop (e.g., `for { ... }`)
		isInfiniteLoop = true
	}

	if s.Post != nil {
		a.analyzeStmt(s.Post)
	}

	bodyReturns := a.analyzeBlockStmt(s.Body)

	// An infinite loop that always returns in its body guarantees a return
	// (it either loops forever or returns — effectively guarantees return)
	if isInfiniteLoop && bodyReturns {
		return true
	}

	return false
}

// getRootIdentifier peels back index and slice expressions to find the base variable.
// e.g., arr[0][1] -> returns the ast.Identifier for "arr"
func (a *Analyzer) getRootIdentifier(expr ast.Expr) *ast.Identifier {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e
	case *ast.IndexExpr:
		return a.getRootIdentifier(e.Left)
	case *ast.SliceExpr:
		return a.getRootIdentifier(e.Left)
	case *ast.FieldAccess:
		return a.getRootIdentifier(e.Object)
	default:
		return nil // It's not a variable (e.g., trying to assign to `5 = 10`)
	}
}
