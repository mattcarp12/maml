package sema

import (
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// DeclareStmt Rules
// =============================================================================

// NoDuplicateDeclaration rejects re-declaration of a name that already exists
// in the current (innermost) scope. Note: this checks only the current scope,
// not parent scopes, so shadowing an outer variable is intentionally allowed.
type NoDuplicateDeclaration struct{}

func (r NoDuplicateDeclaration) Name() string { return "no-duplicate-declaration" }

func (r NoDuplicateDeclaration) Check(node *tast.DeclareStmt, ctx *RuleContext) []Violation {
	if node.Symbol == nil {
		return nil
	}
	name := node.Symbol.Name
	if _, exists := ctx.Scope.symbols[name]; exists {
		return []Violation{violation(node.Pos_, "variable '%s' is already declared", name)}
	}
	return nil
}

// NoUnitAssignment rejects declarations whose RHS evaluates to unit, since
// there is no meaningful value to bind.
type NoUnitAssignment struct{}

func (r NoUnitAssignment) Name() string { return "no-unit-assignment" }

func (r NoUnitAssignment) Check(node *tast.DeclareStmt, ctx *RuleContext) []Violation {
	if node.Value == nil {
		return nil
	}
	if tast.TypeOf(node.Value).Equals(types.UnitType{}) {
		return []Violation{violation(node.Pos_, "cannot assign the result of a function that returns 'unit'")}
	}
	return nil
}

// =============================================================================
// AssignStmt Rules
// =============================================================================

// AssignLValueMustBeSymbol ensures the LHS of an assignment resolves back to
// a named variable. Assignments to arbitrary expressions (e.g. a call result)
// are not valid l-values.
type AssignLValueMustBeSymbol struct{}

func (r AssignLValueMustBeSymbol) Name() string { return "assign-lvalue-must-be-symbol" }

func (r AssignLValueMustBeSymbol) Check(node *tast.AssignStmt, ctx *RuleContext) []Violation {
	if ctx.getRootSymbol(node.LValue) == nil {
		return []Violation{violation(node.Pos_, "cannot assign to non-variable expression")}
	}
	return nil
}

// AssignMutabilityCheck ensures the root variable of the LHS was declared
// mutable. Immutable variables may not be reassigned.
type AssignMutabilityCheck struct{}

func (r AssignMutabilityCheck) Name() string { return "assign-mutability-check" }

func (r AssignMutabilityCheck) Check(node *tast.AssignStmt, ctx *RuleContext) []Violation {
	sym := ctx.getRootSymbol(node.LValue)
	if sym == nil {
		// AssignLValueMustBeSymbol already reported this; don't double-report.
		return nil
	}
	if !sym.Mutable {
		return []Violation{violation(node.Pos_, "cannot mutate immutable variable '%s'", sym.Name)}
	}
	return nil
}

// AssignmentTypeCompatibility checks that the RHS type is assignable to the
// LHS type. There is one special case: when assigning into a map index
// expression, the LHS type reported by the index expression is Option<V>
// (because map reads return Option), but the correct expected type for a
// write is the raw V. This rule unwraps that before comparing.
type AssignmentTypeCompatibility struct{}

func (r AssignmentTypeCompatibility) Name() string { return "assignment-type-compatibility" }

func (r AssignmentTypeCompatibility) Check(node *tast.AssignStmt, ctx *RuleContext) []Violation {
	lvalType := tast.TypeOf(node.LValue)
	rvalType := tast.TypeOf(node.RValue)

	// Map-index write: strip the Option<V> wrapper that map reads impose.
	expectedType := lvalType
	if idxExpr, ok := node.LValue.(*tast.IndexExpr); ok {
		if mapTy, isMap := tast.TypeOf(idxExpr.Left).(*types.MapType); isMap {
			expectedType = mapTy.Value
		}
	}

	if !expectedType.Equals(rvalType) && !types.IsUnknown(expectedType) && !types.IsUnknown(rvalType) {
		return []Violation{violation(node.Pos_,
			"type mismatch: cannot assign '%s' to '%s'",
			rvalType.String(), expectedType.String(),
		)}
	}
	return nil
}

// =============================================================================
// VecPushStmt Rules
// =============================================================================

// VecPushLValueMustBeVector rejects push operations on non-Vector expressions.
type VecPushLValueMustBeVector struct{}

func (r VecPushLValueMustBeVector) Name() string { return "vec-push-lvalue-must-be-vector" }

func (r VecPushLValueMustBeVector) Check(node *tast.VecPushStmt, ctx *RuleContext) []Violation {
	if _, ok := tast.TypeOf(node.LValue).(*types.VectorType); !ok {
		if !types.IsUnknown(tast.TypeOf(node.LValue)) {
			return []Violation{violation(node.Pos_, "cannot push to a non-Vector")}
		}
	}
	return nil
}

// VecPushMutabilityCheck ensures the target Vec variable was declared mutable.
type VecPushMutabilityCheck struct{}

func (r VecPushMutabilityCheck) Name() string { return "vec-push-mutability-check" }

func (r VecPushMutabilityCheck) Check(node *tast.VecPushStmt, ctx *RuleContext) []Violation {
	sym := ctx.getRootSymbol(node.LValue)
	if sym == nil {
		return []Violation{violation(node.Pos_, "cannot push to non-variable expression")}
	}
	if !sym.Mutable {
		return []Violation{violation(node.Pos_, "cannot mutate immutable variable '%s'", sym.Name)}
	}
	return nil
}

// VecPushTypeCompatibility checks that the pushed element type matches the
// Vec's declared element type.
type VecPushTypeCompatibility struct{}

func (r VecPushTypeCompatibility) Name() string { return "vec-push-type-compatibility" }

func (r VecPushTypeCompatibility) Check(node *tast.VecPushStmt, ctx *RuleContext) []Violation {
	vecTy, ok := tast.TypeOf(node.LValue).(*types.VectorType)
	if !ok {
		// VecPushLValueMustBeVector already reported this.
		return nil
	}
	elemType := vecTy.Base
	rvalType := tast.TypeOf(node.RValue)

	if !elemType.Equals(rvalType) && !types.IsUnknown(elemType) && !types.IsUnknown(rvalType) {
		return []Violation{violation(node.Pos_,
			"type mismatch: cannot push '%s' to Vec<%s>",
			rvalType.String(), elemType.String(),
		)}
	}
	return nil
}

// =============================================================================
// ReturnStmt Rules
// =============================================================================

// ReturnTypeCompatibility checks that the returned expression's type matches
// the enclosing function's declared return type. For async functions the
// declared return is Future<T>, but return statements yield T directly, so
// the Future wrapper is stripped before comparison.
type ReturnTypeCompatibility struct{}

func (r ReturnTypeCompatibility) Name() string { return "return-type-compatibility" }

func (r ReturnTypeCompatibility) Check(node *tast.ReturnStmt, ctx *RuleContext) []Violation {
	var retType types.Type = types.UnitType{}
	if node.Value != nil {
		retType = tast.TypeOf(node.Value)
	}

	expected := ctx.ExpectedReturn
	if expected == nil {
		expected = types.UnitType{}
	}

	// Async functions declare Future<T> as their return type, but the body
	// uses bare `return <expr of type T>`. Unwrap one level.
	if ctx.IsAsync {
		if futTy, ok := expected.(*types.FutureType); ok {
			expected = futTy.Base
		}
	}

	if !retType.Equals(expected) && !types.IsUnknown(retType) && !types.IsUnknown(expected) {
		return []Violation{violation(node.Pos_,
			"type mismatch: expected return type '%s', got '%s'",
			expected.String(), retType.String(),
		)}
	}
	return nil
}

// =============================================================================
// ForStmt Rules
// =============================================================================

// ForConditionMustBeBool rejects for-loop conditions that are not boolean.
// A nil condition (infinite loop) is always valid and is skipped.
type ForConditionMustBeBool struct{}

func (r ForConditionMustBeBool) Name() string { return "for-condition-must-be-bool" }

func (r ForConditionMustBeBool) Check(node *tast.ForStmt, ctx *RuleContext) []Violation {
	if node.Condition == nil {
		return nil
	}
	condType := tast.TypeOf(node.Condition)
	if !condType.Equals(types.BoolType{}) && !types.IsUnknown(condType) {
		return []Violation{violation(node.Condition.Pos(),
			"for-loop condition must be of type 'bool', got '%s'",
			condType.String(),
		)}
	}
	return nil
}
