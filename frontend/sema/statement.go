package sema

import "github.com/mattcarp12/maml/frontend/ast"

func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) {
	a.pushScope()
	defer a.popScope()

	for _, stmt := range body.Statements {
		a.analyzeStmt(stmt)
	}
}

func (a *Analyzer) analyzeStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		a.analyzeDeclareStmt(s)

	case *ast.AssignStmt:
		a.analyzeAssignStmt(s)

	case *ast.ReturnStmt:
		a.analyzeReturnStmt(s)

	case *ast.YieldStmt:
		a.analyzeExpr(s.Value)

	case *ast.ExprStmt:
		a.analyzeExpr(s.Value)

	case *ast.ForStmt:
		a.analyzeForStmt(s)
	}
}

func (a *Analyzer) analyzeDeclareStmt(s *ast.DeclareStmt) {
	exprType := a.analyzeExpr(s.Value)

	if _, exists := a.scope.symbols[s.Name]; exists {
		a.errorf(s.Pos(), "variable '%s' is already declared", s.Name)
		return
	}

	if exprType.Equals(UnitType{}) {
		a.errorf(s.Pos(), "cannot assign the result of a function that returns 'unit'")
		return
	}

	a.scope.symbols[s.Name] = &Symbol{
		Kind:    VarSymbol,
		Name:    s.Name,
		Mutable: s.Mutable,
		Type:    exprType,
	}
}

func (a *Analyzer) analyzeAssignStmt(s *ast.AssignStmt) {
	lvalType := a.analyzeExpr(s.LValue)
	rvalType := a.analyzeExpr(s.RValue)

	if rvalType.Equals(UnitType{}) {
		a.errorf(s.Pos(), "cannot assign the result of a function that returns 'unit'")
		return
	}

	if !lvalType.Equals(UnknownType{}) &&
		!rvalType.Equals(UnknownType{}) &&
		!lvalType.Equals(rvalType) {

		a.errorf(
			s.Pos(),
			"type mismatch: cannot assign '%s' to '%s'",
			rvalType.String(),
			lvalType.String(),
		)
	}

	if indexExpr, ok := s.LValue.(*ast.IndexExpr); ok {
		leftObjType := a.analyzeExpr(indexExpr.Left)

		if leftObjType.Equals(StringType{}) {
			a.errorf(
				s.Pos(),
				"strings are immutable and cannot be modified by index",
			)
			return
		}
	}

	rootIdent := a.getRootIdentifier(s.LValue)
	if rootIdent == nil {
		a.errorf(s.Pos(), "cannot assign to non-variable expression")
	}
}

func (a *Analyzer) analyzeReturnStmt(s *ast.ReturnStmt) {
	var retType Type

	if s.Value == nil {
		retType = UnitType{}
	} else {
		retType = a.analyzeExpr(s.Value)
	}

	if a.expectedReturn != nil &&
		!a.expectedReturn.Equals(UnknownType{}) {

		if !retType.Equals(a.expectedReturn) &&
			!retType.Equals(UnknownType{}) {

			a.errorf(
				s.Pos(),
				"type mismatch: expected return type '%s', got '%s'",
				a.expectedReturn.String(),
				retType.String(),
			)

		} else if s.Value != nil {
			a.propagateType(s.Value, a.expectedReturn)
		}
	}
}

func (a *Analyzer) analyzeForStmt(s *ast.ForStmt) {
	a.pushScope()
	defer a.popScope()

	if s.Init != nil {
		a.analyzeStmt(s.Init)
	}

	if s.Condition != nil {
		condType := a.analyzeExpr(s.Condition)

		if !condType.Equals(BoolType{}) &&
			!condType.Equals(UnknownType{}) {

			a.errorf(
				s.Condition.Pos(),
				"condition must be of type 'bool', got '%s'",
				condType.String(),
			)
		}
	}

	if s.Post != nil {
		a.analyzeStmt(s.Post)
	}

	a.analyzeBlockStmt(s.Body)
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
