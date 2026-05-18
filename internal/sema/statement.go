// sema/statement.go
package sema

import "github.com/mattcarp12/maml/internal/ast"

func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) bool {
	a.pushScope()
	defer a.popScope()

	alwaysReturns := false
	for _, stmt := range body.Statements {
		if alwaysReturns {
			a.errorf(stmt.Pos(), "unreachable code after return statement")
			a.analyzeStmt(stmt)
			continue
		}
		if a.analyzeStmt(stmt) {
			alwaysReturns = true
		}
	}
	return alwaysReturns
}

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
		// If the expression is an if-expr used as a statement, we need its
		// return-path guarantee. analyzeIfExprAsStmt handles both type-checking
		// and return-path analysis in a single traversal.
		if ifExpr, ok := s.Value.(*ast.IfExpr); ok {
			return a.analyzeIfExprAsStmt(ifExpr)
		}
		if matchExpr, ok := s.Value.(*ast.MatchExpr); ok {
			return a.analyzeMatchExprAsStmt(matchExpr)
		}
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

	if s.Mutable {
		if ident, ok := s.Value.(*ast.Identifier); ok {
			src := a.resolve(ident.Value)
			if src != nil {
				if src.Invalidated {
					a.errorf(s.Pos(), "use of moved variable '%s'", ident.Value)
					return false
				}
				// Variant constructors are value literals and are always assignable
				if src.Kind != VariantSymbol {
					if !src.Mutable {
						a.errorf(s.Pos(),
							"cannot acquire mutable ownership of immutable variable '%s': use %s.clone()",
							ident.Value, ident.Value)
						return false
					}
					if a.hasActiveAliases(ident.Value) {
						a.errorf(s.Pos(),
							"cannot acquire mutable ownership of '%s': it has active immutable aliases",
							ident.Value)
						return false
					}
					src.Invalidated = true
					a.removeAlias(ident.Value)
				}
			}
		}
	} else {
		if ident, ok := s.Value.(*ast.Identifier); ok {
			src := a.resolve(ident.Value)
			// Record alias regardless of source mutability — an immutable binding
			// of a mutable value still counts as an alias for ownership transfer purposes.
			if src != nil && !src.Invalidated {
				a.recordAlias(s.Name, ident.Value)
			}
		}
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

	if indexExpr, ok := s.LValue.(*ast.IndexExpr); ok {
		leftObjType := a.analyzeExpr(indexExpr.Left)
		if leftObjType.Equals(StringType{}) {
			a.errorf(s.Pos(), "strings are immutable and cannot be modified by index")
			return false
		}
	}

	rootIdent := a.getRootIdentifier(s.LValue)
	if rootIdent != nil {
		sym := a.resolve(rootIdent.Value)
		if sym != nil {
			if sym.Invalidated {
				a.errorf(s.Pos(), "use of moved variable '%s'", rootIdent.Value)
				return false
			}
			if !sym.Mutable {
				a.errorf(s.Pos(), "cannot mutate immutable variable '%s'", rootIdent.Value)
			}
		}
	} else {
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

// analyzeForStmt pushes a scope around the entire for construct so that
// variables declared in Init (e.g. mut i := 0) are scoped to the loop
// and not visible in the enclosing block.
func (a *Analyzer) analyzeForStmt(s *ast.ForStmt) bool {
	// Push a scope for the entire for statement — Init lives here.
	a.pushScope()
	defer a.popScope()

	if s.Init != nil {
		a.analyzeStmt(s.Init)
	}

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
		isInfiniteLoop = true
	}

	if s.Post != nil {
		a.analyzeStmt(s.Post)
	}

	bodyReturns := a.analyzeBlockStmt(s.Body)

	if isInfiniteLoop && bodyReturns {
		return true
	}

	return false
}

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
		return nil
	}
}
