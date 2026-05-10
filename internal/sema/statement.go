// sema/statement.go
package sema

import "github.com/mattcarp12/maml/internal/ast"

// Returns true if the block guarantees a return
func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) bool {
	alwaysReturns := false
	for _, stmt := range body.Statements {
		if a.analyzeStmt(stmt) {
			alwaysReturns = true
		}
	}
	return alwaysReturns
}

// Returns true if the statement is a guaranteed return
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

	// check mutability if lval is a symbol
	if ident, ok := s.LValue.(*ast.Identifier); ok {
		sym := a.resolve(ident.Value)
		if sym != nil && !sym.Mutable {
			a.errorf(s.Pos(), "cannot assign to immutable variable '%s'", ident.Value)
		}
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
	a.pushScope()
	defer a.popScope()

	if s.Init != nil {
		a.analyzeStmt(s.Init)
	}
	if s.Condition != nil {
		condType := a.analyzeExpr(s.Condition)
		if !condType.Equals(BoolType{}) && !condType.Equals(UnknownType{}) {
			a.errorf(s.Condition.Pos(), "condition must be of type 'bool', got '%s'", condType.String())
		}
	}
	if s.Post != nil {
		a.analyzeStmt(s.Post)
	}

	a.analyzeBlockStmt(s.Body)

	return false // for loops don't guarantee a return, even if the body does return
}
