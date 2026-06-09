package sema

import (
	"fmt"
	"reflect"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Violation
// =============================================================================

// Violation is a single failed rule check. Rules return slices of these;
// they never call errorf directly. The analyzer drains violations into its
// error list after each registry.Check call.
type Violation struct {
	Pos ast.Position
	Msg string
}

func violation(pos ast.Position, format string, args ...interface{}) Violation {
	return Violation{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// =============================================================================
// RuleContext
// =============================================================================

// RuleContext is the read-only view of analyzer state that rules receive.
// Rules must never mutate it. All scope mutation (push/pop, symbol insertion)
// remains the exclusive responsibility of the mapper methods in mapper.go.
type RuleContext struct {
	// Scope is exposed for symbol resolution (e.g. checking if a name is
	// already declared, or resolving the root symbol of an l-value).
	Scope *Scope

	// ExpectedReturn is the declared return type of the enclosing function.
	// Rules that check return/yield statements read this.
	ExpectedReturn types.Type

	// IsAsync is true when the enclosing function is declared async.
	// Return-type rules use this to unwrap Future<T> before comparing.
	IsAsync bool
}

// getRootSymbol recursively strips field accesses and index expressions to
// find the base identifier whose symbol carries the mutability flag.
func (ctx *RuleContext) getRootSymbol(expr tast.Expr) *types.Symbol {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *tast.Identifier:
		return e.Symbol
	case *tast.IndexExpr:
		return ctx.getRootSymbol(e.Left)
	case *tast.SliceExpr:
		return ctx.getRootSymbol(e.Left)
	case *tast.FieldAccess:
		return ctx.getRootSymbol(e.Object)
	default:
		return nil
	}
}

// =============================================================================
// Rule Interface
// =============================================================================

// AnyRule is the type-erased rule stored in the registry. The registry calls
// tryCheck, which performs the concrete type assertion and dispatches to the
// typed Check method. If the node is the wrong type the rule is silently
// skipped (this should never happen in practice because Register binds a rule
// to a specific TAST node type, but the assertion keeps the registry safe).
type AnyRule interface {
	name() string
	tryCheck(node tast.Node, ctx *RuleContext) []Violation
}

// Rule is the typed rule interface that concrete rule structs implement.
// T must be a pointer-to-TAST-node type (e.g. *tast.AssignStmt).
//
// Implement Name() and Check(node T, ctx *RuleContext) []Violation.
// Rules must be pure: they read node and ctx, they never mutate them.
type Rule[T tast.Node] interface {
	Name() string
	Check(node T, ctx *RuleContext) []Violation
}

// typedRule wraps a Rule[T] into an AnyRule by closing over the type parameter.
type typedRule[T tast.Node] struct {
	inner Rule[T]
}

func (tr typedRule[T]) name() string { return tr.inner.Name() }

func (tr typedRule[T]) tryCheck(node tast.Node, ctx *RuleContext) []Violation {
	concrete, ok := node.(T)
	if !ok {
		// Should never happen: the registry only calls a rule when the node
		// type matches the key it was registered under.
		return nil
	}
	return tr.inner.Check(concrete, ctx)
}

// =============================================================================
// Registry
// =============================================================================

// Registry maps TAST node types to the ordered list of rules that apply to
// them. It is built once at startup (in newRegistry) and then treated as
// immutable.
type Registry struct {
	rules map[reflect.Type][]AnyRule
}

func newRegistry() *Registry {
	r := &Registry{rules: make(map[reflect.Type][]AnyRule)}

	// -------------------------------------------------------------------------
	// DeclareStmt rules
	// -------------------------------------------------------------------------
	Register(r,
		NoDuplicateDeclaration{},
		NoUnitAssignment{},
	)

	// -------------------------------------------------------------------------
	// AssignStmt rules
	// -------------------------------------------------------------------------
	Register(r,
		AssignLValueMustBeSymbol{},
		AssignMutabilityCheck{},
		AssignmentTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// VecPushStmt rules
	// -------------------------------------------------------------------------
	Register(r,
		VecPushLValueMustBeVector{},
		VecPushMutabilityCheck{},
		VecPushTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// ReturnStmt rules
	// -------------------------------------------------------------------------
	Register(r,
		ReturnTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// ForStmt rules
	// -------------------------------------------------------------------------
	Register(r,
		ForConditionMustBeBool{},
	)

	// -------------------------------------------------------------------------
	// InfixExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		InfixTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// PrefixExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		PrefixTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// IfExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		IfConditionMustBeBool{},
		IfBranchTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// CallExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		CalleeIsCallable{},
		CallArgumentCount{},
		CallArgumentTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// MapLiteral & VecLiteral rules
	// -------------------------------------------------------------------------
	Register(r, MapLiteralTypeCompatibility{})
	Register(r, VecLiteralTypeCompatibility{})

	// -------------------------------------------------------------------------
	// FieldAccess rules
	// -------------------------------------------------------------------------
	Register(r,
		FieldAccessOnStruct{},
	)

	// -------------------------------------------------------------------------
	// IndexExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		IndexExprTypeCompatibility{},
	)

	// -------------------------------------------------------------------------
	// MatchExpr rules
	// -------------------------------------------------------------------------
	Register(r,
		MatchArmTypeUniformity{},
		MatchExhaustiveness{},
	)

	// -------------------------------------------------------------------------
	// FnDecl rules
	// -------------------------------------------------------------------------
	Register(r,
		MainCannotBeAsync{},
	)

	return r
}

// Register binds one or more strongly-typed rules to the TAST node type they
// are designed to check. Go automatically infers T from the rule implementations.
func Register[T tast.Node](r *Registry, rules ...Rule[T]) {
	var sentinel T
	key := reflect.TypeOf(sentinel)
	for _, rule := range rules {
		r.rules[key] = append(r.rules[key], typedRule[T]{inner: rule})
	}
}

// Check runs all rules registered for the concrete type of node, accumulating
// violations.
func (r *Registry) Check(node tast.Node, ctx *RuleContext) []Violation {
	key := reflect.TypeOf(node)
	var all []Violation
	for _, rule := range r.rules[key] {
		all = append(all, rule.tryCheck(node, ctx)...)
	}
	return all
}
