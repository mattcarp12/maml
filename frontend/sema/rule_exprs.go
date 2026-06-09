package sema

import (
	"strings"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// InfixExpr Rules
// =============================================================================

// InfixTypeCompatibility validates that both operands of a binary expression
// are type-compatible with each other and with the operator.
//
//   - Arithmetic (+, -, *, /, %): both operands must be int; result is int.
//   - Comparison (==, !=, <, <=, >, >=): both operands must be the same type;
//     result is bool.
//   - Logical (&&, ||): both operands must be bool; result is bool.
//
// When either operand is UnknownType the check is suppressed to avoid
// cascading errors from an earlier unresolved name.
type InfixTypeCompatibility struct{}

func (r InfixTypeCompatibility) Name() string { return "infix-type-compatibility" }

func (r InfixTypeCompatibility) Check(node *tast.InfixExpr, ctx *RuleContext) []Violation {
	left := tast.TypeOf(node.Left)
	right := tast.TypeOf(node.Right)

	if types.IsUnknown(left) || types.IsUnknown(right) {
		return nil
	}

	if !left.Equals(right) {
		return []Violation{violation(node.Pos_,
			"type mismatch: cannot apply '%s' to '%s' and '%s'",
			node.Operator, left.String(), right.String(),
		)}
	}

	// Operands are the same type — now check operator compatibility.
	switch node.Operator {
	case "+", "-", "*", "/", "%":
		if !left.Equals(types.IntType{}) {
			return []Violation{violation(node.Pos_,
				"type mismatch: operator '%s' requires 'int' operands, got '%s'",
				node.Operator, left.String(),
			)}
		}
	case "&&", "||":
		if !left.Equals(types.BoolType{}) {
			return []Violation{violation(node.Pos_,
				"type mismatch: operator '%s' requires 'bool' operands, got '%s'",
				node.Operator, left.String(),
			)}
		}
		// ==, !=, <, <=, >, >= are valid for any pair of equal types; no further check needed.
	}

	return nil
}

// =============================================================================
// PrefixExpr Rules
// =============================================================================

// PrefixTypeCompatibility validates unary operator operand types.
//
//   - !  requires bool
//   - -  requires int
type PrefixTypeCompatibility struct{}

func (r PrefixTypeCompatibility) Name() string { return "prefix-type-compatibility" }

func (r PrefixTypeCompatibility) Check(node *tast.PrefixExpr, ctx *RuleContext) []Violation {
	right := tast.TypeOf(node.Right)
	if types.IsUnknown(right) {
		return nil
	}

	switch node.Operator {
	case "!":
		if !right.Equals(types.BoolType{}) {
			return []Violation{violation(node.Pos_,
				"operator '!' expects 'bool', got '%s'", right.String(),
			)}
		}
	case "-":
		if !right.Equals(types.IntType{}) {
			return []Violation{violation(node.Pos_,
				"operator '-' expects 'int', got '%s'", right.String(),
			)}
		}
	}
	return nil
}

// =============================================================================
// IfExpr Rules
// =============================================================================

// IfConditionMustBeBool rejects if-expressions whose condition is not bool.
type IfConditionMustBeBool struct{}

func (r IfConditionMustBeBool) Name() string { return "if-condition-must-be-bool" }

func (r IfConditionMustBeBool) Check(node *tast.IfExpr, ctx *RuleContext) []Violation {
	condType := tast.TypeOf(node.Condition)
	if !condType.Equals(types.BoolType{}) && !types.IsUnknown(condType) {
		return []Violation{violation(node.Condition.Pos(),
			"if condition must be of type 'bool', got '%s'", condType.String(),
		)}
	}
	return nil
}

// IfBranchTypeCompatibility checks that the consequence and alternative
// branches yield compatible types. A missing alternative is treated as
// yielding unit, so a one-armed if must yield unit from its consequence
// (or the whole expression's type is unit by MergeTypes).
//
// This rule does not error directly — type merging is handled during
// construction and an UnknownType result from MergeTypes signals a conflict.
// We report here only when both branches are fully known and incompatible.
type IfBranchTypeCompatibility struct{}

func (r IfBranchTypeCompatibility) Name() string { return "if-branch-type-compatibility" }

func (r IfBranchTypeCompatibility) Check(node *tast.IfExpr, ctx *RuleContext) []Violation {
	consType := tast.TypeOfBlock(node.Consequence)
	altType := types.Type(types.UnitType{})
	if node.Alternative != nil {
		altType = tast.TypeOfBlock(node.Alternative)
	}

	if types.IsUnknown(consType) || types.IsUnknown(altType) {
		return nil
	}

	if !consType.Equals(altType) {
		return []Violation{violation(node.Pos_,
			"if/else branches have incompatible types: '%s' vs '%s'",
			consType.String(), altType.String(),
		)}
	}
	return nil
}

// =============================================================================
// CallExpr Rules
// =============================================================================

// CalleeIsCallable rejects call expressions whose callee does not resolve to
// a FunctionType. VariantLiteral callees are dispatched separately before
// CallExpr is built, so by the time this rule fires the callee must be a
// plain function.
type CalleeIsCallable struct{}

func (r CalleeIsCallable) Name() string { return "callee-is-callable" }

func (r CalleeIsCallable) Check(node *tast.CallExpr, ctx *RuleContext) []Violation {
	if _, ok := tast.TypeOf(node.Function).(*types.FunctionType); !ok {
		if !types.IsUnknown(tast.TypeOf(node.Function)) {
			return []Violation{violation(node.Pos_,
				"cannot call non-function type '%s'", tast.TypeOf(node.Function).String(),
			)}
		}
	}
	return nil
}

// CallArgumentCount checks that the number of arguments at a call site matches
// the function's declared parameter count.
type CallArgumentCount struct{}

func (r CallArgumentCount) Name() string { return "call-argument-count" }

func (r CallArgumentCount) Check(node *tast.CallExpr, ctx *RuleContext) []Violation {
	ft, ok := tast.TypeOf(node.Function).(*types.FunctionType)
	if !ok {
		// CalleeIsCallable already reported this.
		return nil
	}
	if len(node.Arguments) != len(ft.Params) {
		return []Violation{violation(node.Pos_,
			"wrong number of arguments: expected %d, got %d",
			len(ft.Params), len(node.Arguments),
		)}
	}
	return nil
}

// CallArgumentTypeCompatibility checks each argument's type and ownership
// modifier against the corresponding parameter declaration.
//
// For each argument i (within bounds):
//   - The argument's type must match the parameter's declared type.
//   - If the parameter is ParamMutBorrow, the call site must pass `mut`.
//   - If the parameter is ParamBorrow, the call site must not use any modifier.
type CallArgumentTypeCompatibility struct{}

func (r CallArgumentTypeCompatibility) Name() string { return "call-argument-type-compatibility" }

func (r CallArgumentTypeCompatibility) Check(node *tast.CallExpr, ctx *RuleContext) []Violation {
	ft, ok := tast.TypeOf(node.Function).(*types.FunctionType)
	if !ok {
		return nil
	}

	var violations []Violation

	for i, arg := range node.Arguments {
		if i >= len(ft.Params) {
			// Arity mismatch already reported by CallArgumentCount.
			break
		}

		expected := ft.Params[i]
		got := tast.TypeOf(arg.Argument)

		// Type check
		if !got.Equals(expected) && !types.IsUnknown(got) && !types.IsUnknown(expected) {
			violations = append(violations, violation(arg.Pos_,
				"argument %d type mismatch: expected '%s', got '%s'",
				i+1, expected.String(), got.String(),
			))
		}

		// Ownership/mutability mode check
		switch ft.ParamModes[i] {
		case types.ParamMutBorrow:
			if !arg.Mut {
				violations = append(violations, violation(arg.Pos_,
					"argument %d requires 'mut' modifier", i+1,
				))
			}
		case types.ParamBorrow:
			if arg.Mut {
				violations = append(violations, violation(arg.Pos_,
					"argument %d does not take ownership modifiers", i+1,
				))
			}
		}
	}

	return violations
}

// =============================================================================
// FieldAccess Rules
// =============================================================================

// FieldAccessOnStruct ensures that field access is performed on a struct type,
// and that the named field exists on that struct.
type FieldAccessOnStruct struct{}

func (r FieldAccessOnStruct) Name() string { return "field-access-on-struct" }

func (r FieldAccessOnStruct) Check(node *tast.FieldAccess, ctx *RuleContext) []Violation {
	objType := tast.TypeOf(node.Object)
	if types.IsUnknown(objType) {
		return nil
	}

	st, ok := objType.(*types.StructType)
	if !ok {
		return []Violation{violation(node.Pos_,
			"cannot access field '%s' on non-struct type '%s'",
			node.Field.Value, objType.String(),
		)}
	}

	if st.GetFieldIndex(node.Field.Value) == -1 {
		return []Violation{violation(node.Field.Pos_,
			"field '%s' does not exist on struct '%s'",
			node.Field.Value, st.Name,
		)}
	}

	return nil
}

// =============================================================================
// IndexExpr Rules
// =============================================================================

// IndexExprTypeCompatibility validates both the collection type (must be
// indexable) and the index type (must match the collection's key type).
//
//   - Array, View, Vec, String: index must be int.
//   - Map: index must match the map's key type.
//   - Any other type: error (unless unknown).
type IndexExprTypeCompatibility struct{}

func (r IndexExprTypeCompatibility) Name() string { return "index-expr-type-compatibility" }

func (r IndexExprTypeCompatibility) Check(node *tast.IndexExpr, ctx *RuleContext) []Violation {
	leftType := tast.TypeOf(node.Left)
	idxType := tast.TypeOf(node.Index)

	if types.IsUnknown(leftType) {
		return nil
	}

	var violations []Violation

	switch ty := leftType.(type) {
	case types.ArrayType, types.ViewType, types.VectorType, types.StringType:
		if !idxType.Equals(types.IntType{}) && !types.IsUnknown(idxType) {
			violations = append(violations, violation(node.Index.Pos(),
				"index must be an integer, got '%s'", idxType.String(),
			))
		}
	case types.MapType:
		if !idxType.Equals(ty.Key) && !types.IsUnknown(idxType) && !types.IsUnknown(ty.Key) {
			violations = append(violations, violation(node.Index.Pos(),
				"map index must be of type '%s', got '%s'",
				ty.Key.String(), idxType.String(),
			))
		}
	default:
		violations = append(violations, violation(node.Pos_,
			"cannot index into non-collection type '%s'", leftType.String(),
		))
	}

	return violations
}

// =============================================================================
// MatchExpr Rules
// =============================================================================

// MatchArmTypeUniformity checks that all arms of a match expression yield
// compatible types. It uses MergeTypes pairwise, consistent with how the
// mapper constructs the match node's result type, and reports an error when
// two fully-known arm types are incompatible (merge produces UnknownType).
type MatchArmTypeUniformity struct{}

func (r MatchArmTypeUniformity) Name() string { return "match-arm-type-uniformity" }

func (r MatchArmTypeUniformity) Check(node *tast.MatchExpr, ctx *RuleContext) []Violation {
	if len(node.Arms) == 0 {
		return nil
	}

	unified := tast.TypeOf(node.Arms[0].Body)

	for i := 1; i < len(node.Arms); i++ {
		armType := tast.TypeOf(node.Arms[i].Body)
		merged := types.MergeTypes(unified, armType)

		if types.IsUnknown(merged) && !types.IsUnknown(unified) && !types.IsUnknown(armType) {
			return []Violation{violation(node.Arms[i].Pos_,
				"match arm has incompatible type: expected '%s', got '%s'",
				unified.String(), armType.String(),
			)}
		}
		unified = merged
	}

	return nil
}

// MatchExhaustiveness checks that every variant of a sum type subject is
// covered by an arm, or that a wildcard catch-all is present. For non-sum
// subjects a wildcard is always required.
//
// This rule operates on the TAST, so it inspects tast.Pattern types rather
// than ast.Pattern types. The mapping is:
//
//	ast.WildcardPattern    → tast.WildcardPattern
//	ast.IdentifierPattern  → tast.IdentifierPattern  (catch-all binding)
//	                       → tast.VariantPattern     (unit variant)
//	ast.CompositePattern   → tast.VariantPattern     (tuple/struct variant)
type MatchExhaustiveness struct{}

func (r MatchExhaustiveness) Name() string { return "match-exhaustiveness" }

func (r MatchExhaustiveness) Check(node *tast.MatchExpr, ctx *RuleContext) []Violation {
	// A wildcard arm covers everything unconditionally.
	for _, arm := range node.Arms {
		if _, isWild := arm.Pattern.(*tast.WildcardPattern); isWild {
			return nil
		}
	}

	subjectType := tast.TypeOf(node.Subject)

	sumTy, ok := subjectType.(*types.SumType)
	if !ok {
		// Non-sum types require an explicit wildcard to be exhaustive.
		if !types.IsUnknown(subjectType) {
			return []Violation{violation(node.Pos_,
				"non-exhaustive match: matching on type '%s' requires a wildcard '_' pattern",
				subjectType.String(),
			)}
		}
		return nil
	}

	seen := make(map[string]bool)
	for _, arm := range node.Arms {
		switch pat := arm.Pattern.(type) {

		case *tast.VariantPattern:
			// Covers both unit variants (from IdentifierPattern) and
			// tuple/struct variants (from CompositePattern).
			if pat.Variant != nil {
				seen[pat.Variant.Value] = true
			}

		case *tast.IdentifierPattern:
			// A bare identifier that did NOT resolve to a variant is a
			// catch-all binding — it covers all remaining cases.
			return nil
		}
	}

	var missing []string
	for _, v := range sumTy.Variants {
		if !seen[v.Name] {
			missing = append(missing, v.Name)
		}
	}

	if len(missing) > 0 {
		return []Violation{violation(node.Pos_,
			"non-exhaustive match: missing cases for %s",
			strings.Join(missing, ", "),
		)}
	}

	return nil
}

// =============================================================================
// MapLiteral Rules
// =============================================================================

type MapLiteralTypeCompatibility struct{}

func (r MapLiteralTypeCompatibility) Name() string { return "map-literal-type-compatibility" }

func (r MapLiteralTypeCompatibility) Check(node *tast.MapLiteral, ctx *RuleContext) []Violation {
	var violations []Violation
	mapTy := node.Type

	for _, elem := range node.Elements {
		keyType := tast.TypeOf(elem.Key)
		valType := tast.TypeOf(elem.Value)

		if !keyType.Equals(mapTy.Key) && !types.IsUnknown(keyType) {
			violations = append(violations, violation(elem.Key.Pos(),
				"map key type mismatch: expected '%s', got '%s'", mapTy.Key.String(), keyType.String()))
		}
		if !valType.Equals(mapTy.Value) && !types.IsUnknown(valType) {
			violations = append(violations, violation(elem.Value.Pos(),
				"map value type mismatch: expected '%s', got '%s'", mapTy.Value.String(), valType.String()))
		}
	}
	return violations
}

// =============================================================================
// VecLiteral Rules
// =============================================================================

type VecLiteralTypeCompatibility struct{}

func (r VecLiteralTypeCompatibility) Name() string { return "vec-literal-type-compatibility" }

func (r VecLiteralTypeCompatibility) Check(node *tast.VecLiteral, ctx *RuleContext) []Violation {
	var violations []Violation
	vecTy := node.Type

	for _, elem := range node.Elements {
		valType := tast.TypeOf(elem)
		if !valType.Equals(vecTy.Base) && !types.IsUnknown(valType) {
			violations = append(violations, violation(elem.Pos(),
				"vector element type mismatch: expected '%s', got '%s'", vecTy.Base.String(), valType.String()))
		}
	}
	return violations
}
