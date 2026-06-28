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
		return nil
	}

	// Allow mutation if it's an owned mutable variable OR a mutable borrow
	if !sym.Mutable && sym.Cap != "mut" {
		return []Violation{violation(node.Pos_, "cannot mutate immutable variable '%s'", sym.Name)}
	}
	return nil
}

// AssignmentTypeCompatibility checks that the RHS type is assignable to the LHS type.
type AssignmentTypeCompatibility struct{}

func (r AssignmentTypeCompatibility) Name() string { return "assignment-type-compatibility" }

func (r AssignmentTypeCompatibility) Check(node *tast.AssignStmt, ctx *RuleContext) []Violation {
	lvalType := tast.TypeOf(node.LValue)
	rvalType := tast.TypeOf(node.RValue)
	expectedType := lvalType
	if !expectedType.Equals(rvalType) && !types.IsUnknown(expectedType) && !types.IsUnknown(rvalType) {
		return []Violation{violation(node.Pos_,
			"type mismatch: cannot assign '%s' to '%s'",
			rvalType.String(), expectedType.String(),
		)}
	}

	if node.Operator != "" {
		isExpectedInt := isIntegerType(expectedType)
		isRvalInt := isIntegerType(rvalType)

		if !isExpectedInt || !isRvalInt {
			return []Violation{violation(node.Pos_,
				"invalid operation: operator '%s=' not defined on type '%s'",
				node.Operator, expectedType.String(),
			)}
		}
	}

	return nil
}

func isIntegerType(t types.Type) bool {
	switch t.(type) {
	case types.I8Type, types.I16Type, types.I32Type, types.I64Type, types.I128Type,
		types.U8Type, types.U16Type, types.U32Type, types.U64Type, types.U128Type:
		return true
	}
	return false
}

// =============================================================================
// AliasDecl Rules
// =============================================================================

type AliasMutabilityValid struct{}

func (r AliasMutabilityValid) Name() string { return "alias-mutability-valid" }

func (r AliasMutabilityValid) Check(node *tast.AliasDecl, ctx *RuleContext) []Violation {
	if node.Symbol == nil || node.Symbol.Cap != "mut" {
		return nil
	}

	rootSym := ctx.getRootSymbol(node.Value)
	if rootSym != nil {
		// If we are asking for a 'mut' alias, the source must be mutable
		if !rootSym.Mutable && rootSym.Cap != "mut" {
			return []Violation{violation(node.Pos_,
				"cannot take a 'mut' alias of immutable variable '%s'", rootSym.Name)}
		}
	}
	return nil
}

type AliasPathValid struct{}

func (r AliasPathValid) Name() string { return "alias-path-valid" }

func (r AliasPathValid) Check(node *tast.AliasDecl, ctx *RuleContext) []Violation {
	if node.Symbol == nil {
		return nil
	}

	cap := node.Symbol.Cap
	if cap == types.CapMut || cap == types.CapOwn || cap == types.CapRo {
		if !isValidMemoryPath(node.Value) {
			return []Violation{violation(node.Pos_,
				"the '%s' capability can only be applied to variables and their fields", cap)}
		}
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
	if !sym.Mutable && sym.Cap != "mut" {
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
// the enclosing function's declared return type.
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

	if ctx.IsAsync {
		if fut, ok := expected.(*types.FutureType); ok {
			expected = fut.Base
		} else {
			return []Violation{violation(node.Pos_, "error: async function return type should be wrapped in Future")}
		}
	}

	if !retType.Equals(expected) && !types.IsUnknown(retType) && !types.IsUnknown(expected) {
		return []Violation{violation(node.Pos_,
			"type mismatch: expected return type '%s', got '%s'",
			expected.String(), retType.String(),
		)}
	}

	if _, isView := retType.(*types.ViewType); isView {
		return []Violation{violation(node.Pos_, "cannot return a View from a function (views are stack-scoped borrows)")}
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
