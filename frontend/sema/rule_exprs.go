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
//   - If the parameter is ParamOwn, the call site must pass `own`.
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
			break // Arity mismatch already reported by CallArgumentCount
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
		_, isOwnExpr := arg.Argument.(*tast.OwnExpr)

		switch ft.ParamModes[i] {
		case types.ParamOwn:
			if !isOwnExpr {
				violations = append(violations, violation(arg.Pos_,
					"argument %d requires 'own' modifier to transfer ownership", i+1,
				))
			}
			if arg.Mut {
				violations = append(violations, violation(arg.Pos_,
					"argument %d cannot be 'mut' because it requires 'own'", i+1,
				))
			}

		case types.ParamMutBorrow:
			if !arg.Mut {
				violations = append(violations, violation(arg.Pos_,
					"argument %d requires 'mut' modifier", i+1,
				))
			}
			if isOwnExpr {
				violations = append(violations, violation(arg.Pos_,
					"argument %d cannot be 'own' because it expects a mutable borrow", i+1,
				))
			}

		case types.ParamBorrow:
			if arg.Mut || isOwnExpr {
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
	case *types.ArrayType, *types.ViewType, *types.VectorType, types.StringType:
		if !idxType.Equals(types.IntType{}) && !types.IsUnknown(idxType) {
			violations = append(violations, violation(node.Index.Pos(),
				"index must be an integer, got '%s'", idxType.String(),
			))
		}
	case *types.MapType:
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

// =============================================================================
// FreezeExpr Rules
// =============================================================================

// FreezeRequiresOwn ensures that the operand provided to a freeze expression
// is strictly an ownership transfer (an 'own' expression).
type FreezeRequiresOwn struct{}

func (r FreezeRequiresOwn) Name() string { return "freeze-requires-own" }

func (r FreezeRequiresOwn) Check(node *tast.FreezeExpr, ctx *RuleContext) []Violation {
	if _, ok := node.Value.(*tast.OwnExpr); !ok {
		return []Violation{violation(node.Value.Pos(),
			"the 'freeze' operator requires an explicit ownership transfer (e.g., 'freeze(own x)')",
		)}
	}
	return nil
}

// =============================================================================
// OwnExpr Rules
// =============================================================================

// OwnOperandMustBePath enforces the grammar rule that 'own' can only be
// applied to memory paths (identifiers or field access chains).
type OwnOperandMustBePath struct{}

func (r OwnOperandMustBePath) Name() string { return "own-operand-must-be-path" }

func (r OwnOperandMustBePath) Check(node *tast.OwnExpr, ctx *RuleContext) []Violation {
	var current tast.Expr = node.Value
	for {
		switch e := current.(type) {
		case *tast.FieldAccess:
			current = e.Object // Traverse down the path
		case *tast.Identifier:
			return nil // Valid path root
		default:
			return []Violation{violation(node.Pos_,
				"the 'own' operator can only be applied to variables and their fields (e.g., 'own data' or 'own msg.payload')",
			)}
		}
	}
}

// CannotMoveBorrowedValue ensures that a function cannot take ownership
// of a variable that was passed to it as a borrow.
type CannotMoveBorrowedValue struct{}

func (r CannotMoveBorrowedValue) Name() string { return "cannot-move-borrowed-value" }

func (r CannotMoveBorrowedValue) Check(node *tast.OwnExpr, ctx *RuleContext) []Violation {
	sym := ctx.getRootSymbol(node.Value)
	if sym != nil && (sym.ParamMode == types.ParamBorrow || sym.ParamMode == types.ParamMutBorrow) {
		return []Violation{violation(node.Pos_,
			"cannot take ownership ('own') of a borrowed parameter '%s'", sym.Name,
		)}
	}
	return nil
}

// CannotReassignBorrowedParameter enforces Section 4.3: Mutable borrows
// permit in-place mutation (e.g., data.field = x) but forbid full reallocation
// or reassignment of the root binding.
type CannotReassignBorrowedParameter struct{}

func (r CannotReassignBorrowedParameter) Name() string { return "cannot-reassign-borrowed-parameter" }

func (r CannotReassignBorrowedParameter) Check(node *tast.AssignStmt, ctx *RuleContext) []Violation {
	// We only trigger if the LHS is exactly an identifier (root binding).
	// If it is a FieldAccess or IndexExpr, it is a legal in-place mutation.
	if ident, ok := node.LValue.(*tast.Identifier); ok {
		sym := ident.Symbol
		if sym != nil && sym.Kind == types.ParamSymbol {
			if sym.ParamMode == types.ParamBorrow || sym.ParamMode == types.ParamMutBorrow {
				return []Violation{violation(node.Pos_,
					"cannot reassign borrowed parameter '%s'; mutable borrows only permit in-place mutation", sym.Name,
				)}
			}
		}
	}
	return nil
}

// ==========================================================================
// Async function fules
// ==========================================================================
type SpawnRequiresAsyncCall struct{}

func (r SpawnRequiresAsyncCall) Name() string { return "spawn-requires-async-call" }

func (r SpawnRequiresAsyncCall) Check(node *tast.SpawnExpr, ctx *RuleContext) []Violation {
	if node.Value == nil {
		return nil // structural error already caught by mapper
	}
	callType := tast.TypeOf(node.Value)
	if _, ok := callType.(*types.FutureType); !ok && !types.IsUnknown(callType) {
		return []Violation{violation(node.Pos_,
			"spawn can only be applied to async functions, got '%s'", callType.String())}
	}
	return nil
}

type AwaitRequiresFuture struct{}

func (r AwaitRequiresFuture) Name() string { return "await-requires-future" }

func (r AwaitRequiresFuture) Check(node *tast.AwaitExpr, ctx *RuleContext) []Violation {
	var violations []Violation

	// Check Context
	if !ctx.IsAsync {
		violations = append(violations, violation(node.Pos_,
			"cannot use 'await' outside of an async function"))
	}

	// Check Type
	valType := tast.TypeOf(node.Value)
	if _, ok := valType.(*types.FutureType); !ok && !types.IsUnknown(valType) {
		violations = append(violations, violation(node.Pos_,
			"cannot await non-Future type '%s'", valType.String()))
	}

	return violations
}
