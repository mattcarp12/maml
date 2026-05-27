package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Block Scope
// =============================================================================

func (a *Analyzer) buildBlockStmt(body *ast.BlockStmt) *tast.BlockStmt {
	if body == nil {
		return nil
	}

	a.pushScope()
	defer a.popScope()

	var stmts []tast.Stmt
	for _, s := range body.Statements {
		if stmtNode := a.buildStmt(s); stmtNode != nil {
			stmts = append(stmts, stmtNode)
		}
	}

	return &tast.BlockStmt{
		Pos_:       body.Pos_,
		End_:       body.End_,
		Statements: stmts,
	}
}

// =============================================================================
// Statement Dispatcher
// =============================================================================

func (a *Analyzer) buildStmt(stmt ast.Stmt) tast.Stmt {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		return a.buildDeclareStmt(s)
	case *ast.AssignStmt:
		return a.buildAssignStmt(s)
	case *ast.ReturnStmt:
		return a.buildReturnStmt(s)
	case *ast.YieldStmt:
		return a.buildYieldStmt(s)
	case *ast.ExprStmt:
		return a.buildExprStmt(s)
	case *ast.ForStmt:
		return a.buildForStmt(s)
	case *ast.BreakStmt:
		return &tast.BreakStmt{Pos_: s.Pos(), End_: s.End()}
	case *ast.ContinueStmt:
		return &tast.ContinueStmt{Pos_: s.Pos(), End_: s.End()}
	case *ast.BlockStmt:
		return a.buildBlockStmt(s)
	}

	return nil
}

// =============================================================================
// Variable Statements
// =============================================================================

func (a *Analyzer) buildDeclareStmt(s *ast.DeclareStmt) *tast.DeclareStmt {
	if _, exists := a.scope.symbols[s.Name]; exists {
		a.errorf(s.Pos_, "variable '%s' is already declared", s.Name)
	}

	tastValue := a.buildExpr(s.Value)
	exprType := typeOf(tastValue)

	if exprType.Equals(types.UnitType{}) {
		a.errorf(s.Pos_, "cannot assign the result of a function that returns 'unit'")
	}

	sym := &types.Symbol{
		Kind:    types.VarSymbol,
		Name:    s.Name,
		Type:    exprType,
		Mutable: s.Mutable,
	}

	a.scope.symbols[s.Name] = sym

	return &tast.DeclareStmt{
		Pos_:   s.Pos_,
		Symbol: sym,
		Value:  tastValue,
	}
}

func (a *Analyzer) buildAssignStmt(s *ast.AssignStmt) *tast.AssignStmt {
	tastLValue := a.buildExpr(s.LValue)
	tastRValue := a.buildExpr(s.RValue)

	lvalType := typeOf(tastLValue)
	rvalType := typeOf(tastRValue)

	// 1. Static Capability Check: Extract the root symbol and verify mutability
	rootSym := a.getRootSymbol(tastLValue)
	if rootSym == nil {
		a.errorf(s.Pos_, "cannot assign to non-variable expression")
	} else if !rootSym.Mutable {
		a.errorf(s.Pos_, "cannot mutate immutable variable '%s'", rootSym.Name)
	}

	// 2. Static Type Check
	if !lvalType.Equals(rvalType) && !types.IsUnknown(lvalType) && !types.IsUnknown(rvalType) {
		a.errorf(s.Pos_, "type mismatch: cannot assign '%s' to '%s'", rvalType.String(), lvalType.String())
	}

	return &tast.AssignStmt{
		Pos_:   s.Pos_,
		LValue: tastLValue,
		RValue: tastRValue,
	}
}

// =============================================================================
// Control Flow Statements
// =============================================================================

func (a *Analyzer) buildReturnStmt(s *ast.ReturnStmt) *tast.ReturnStmt {
	var tastValue tast.Expr
	var retType types.Type = types.UnitType{}

	if s.Value != nil {
		tastValue = a.buildExpr(s.Value)
		retType = typeOf(tastValue)
	}

	expected := a.expectedReturn

	// Unwrap Task<T> to T if we are inside an async function
	if a.currentFn != nil && a.currentFn.IsAsync {
		if taskTy, ok := expected.(types.TaskType); ok {
			expected = taskTy.Base
		}
	}

	if !retType.Equals(expected) && !types.IsUnknown(retType) && !types.IsUnknown(expected) {
		a.errorf(s.Pos(), "type mismatch: expected return type '%s', got '%s'", expected, retType)
	}

	return &tast.ReturnStmt{
		Pos_:  s.Pos_,
		Value: tastValue,
	}
}

func (a *Analyzer) buildForStmt(s *ast.ForStmt) *tast.ForStmt {
	a.pushScope()
	defer a.popScope()

	var initStmt tast.Stmt
	if s.Init != nil {
		initStmt = a.buildStmt(s.Init)
	}

	var condExpr tast.Expr
	if s.Condition != nil {
		condExpr = a.buildExpr(s.Condition)
		condType := typeOf(condExpr)
		if !condType.Equals(types.BoolType{}) && !types.IsUnknown(condType) {
			a.errorf(s.Condition.Pos(), "condition must be of type 'bool'")
		}
	}

	var postStmt tast.Stmt
	if s.Post != nil {
		postStmt = a.buildStmt(s.Post)
	}

	var bodyBlock *tast.BlockStmt
	if s.Body != nil {
		bodyBlock = a.buildBlockStmt(s.Body)
	}

	return &tast.ForStmt{
		Pos_:      s.Pos_,
		Init:      initStmt,
		Condition: condExpr,
		Post:      postStmt,
		Body:      bodyBlock,
	}
}

func (a *Analyzer) buildYieldStmt(s *ast.YieldStmt) *tast.YieldStmt {
	return &tast.YieldStmt{
		Pos_:  s.Pos_,
		Value: a.buildExpr(s.Value),
	}
}

func (a *Analyzer) buildExprStmt(s *ast.ExprStmt) *tast.ExprStmt {
	return &tast.ExprStmt{
		Pos_:  s.Pos_,
		Value: a.buildExpr(s.Value),
	}
}

// =============================================================================
// Semantic Root Extraction
// =============================================================================

// getRootSymbol recursively strips away fields and indices to find the base memory owner.
func (a *Analyzer) getRootSymbol(expr tast.Expr) *types.Symbol {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *tast.Identifier:
		return e.Symbol
	case *tast.IndexExpr:
		return a.getRootSymbol(e.Left)
	case *tast.SliceExpr:
		return a.getRootSymbol(e.Left)
	case *tast.FieldAccess:
		return a.getRootSymbol(e.Object)
	default:
		return nil
	}
}
