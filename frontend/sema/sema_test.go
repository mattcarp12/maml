package sema_test

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Position helpers
// =============================================================================

// zpos returns a zeroed position suitable for synthetic AST nodes.
func zpos() ast.Position { return ast.Position{} }

// =============================================================================
// AST leaf constructors
// =============================================================================

func ident(name string) *ast.Identifier {
	return &ast.Identifier{Pos_: zpos(), Value: name}
}

func intLit(v int64) *ast.IntLiteral {
	return &ast.IntLiteral{Pos_: zpos(), End_: zpos(), Value: v}
}

func boolLit(v bool) *ast.BoolLiteral {
	return &ast.BoolLiteral{Pos_: zpos(), Value: v}
}

func strLit(v string) *ast.StringLiteral {
	return &ast.StringLiteral{Pos_: zpos(), Value: v}
}

// =============================================================================
// AST type-expression constructors
// =============================================================================

func namedType(name string) *ast.NamedTypeExpr {
	return &ast.NamedTypeExpr{Name: ident(name)}
}

func intTypeExpr() *ast.NamedTypeExpr  { return namedType("int") }
func boolTypeExpr() *ast.NamedTypeExpr { return namedType("bool") }
func strTypeExpr() *ast.NamedTypeExpr  { return namedType("string") }
func unitTypeExpr() *ast.NamedTypeExpr { return namedType("unit") }

func arrayTypeExpr(base ast.TypeExpr, size int) *ast.ArrayTypeExpr {
	return &ast.ArrayTypeExpr{Base: base, Size: size}
}

func sliceTypeExpr(base ast.TypeExpr) *ast.SliceTypeExpr {
	return &ast.SliceTypeExpr{Base: base}
}

// =============================================================================
// AST statement constructors
// =============================================================================

func declareStmt(name string, mutable bool, value ast.Expr) *ast.DeclareStmt {
	return &ast.DeclareStmt{Pos_: zpos(), Name: name, Mutable: mutable, Value: value}
}

func assignStmt(lval, rval ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Pos_: zpos(), LValue: lval, RValue: rval}
}

func returnStmt(value ast.Expr) *ast.ReturnStmt {
	return &ast.ReturnStmt{Pos_: zpos(), Value: value}
}

func yieldStmt(value ast.Expr) *ast.YieldStmt {
	return &ast.YieldStmt{Pos_: zpos(), Value: value}
}

func exprStmt(e ast.Expr) *ast.ExprStmt {
	return &ast.ExprStmt{Pos_: zpos(), Value: e}
}

func blockStmt(stmts ...ast.Stmt) *ast.BlockStmt {
	return &ast.BlockStmt{Pos_: zpos(), Statements: stmts}
}

func forStmt(init ast.Stmt, cond ast.Expr, post ast.Stmt, body *ast.BlockStmt) *ast.ForStmt {
	return &ast.ForStmt{Pos_: zpos(), Init: init, Condition: cond, Post: post, Body: body}
}

// =============================================================================
// AST expression constructors
// =============================================================================

func infixExpr(left ast.Expr, op string, right ast.Expr) *ast.InfixExpr {
	return &ast.InfixExpr{Pos_: zpos(), Left: left, Operator: op, Right: right}
}

func prefixExpr(op string, right ast.Expr) *ast.PrefixExpr {
	return &ast.PrefixExpr{Pos_: zpos(), Operator: op, Right: right}
}

func ifExpr(cond ast.Expr, cons *ast.BlockStmt, alt *ast.BlockStmt) *ast.IfExpr {
	return &ast.IfExpr{Pos_: zpos(), Condition: cond, Consequence: cons, Alternative: alt}
}

func callExpr(fn ast.Expr, args ...ast.CallArg) *ast.CallExpr {
	return &ast.CallExpr{Pos_: zpos(), Function: fn, Arguments: args}
}

func callArg(e ast.Expr) ast.CallArg {
	return ast.CallArg{Pos_: zpos(), Argument: e}
}

func mutArg(e ast.Expr) ast.CallArg {
	return ast.CallArg{Pos_: zpos(), Argument: e, Mut: true}
}

func ownArg(e ast.Expr) ast.CallArg {
	return ast.CallArg{Pos_: zpos(), Argument: e, Own: true}
}

func awaitExpr(value ast.Expr) *ast.AwaitExpr {
	return &ast.AwaitExpr{Pos_: zpos(), Value: value}
}

func arrayLiteral(elems ...ast.Expr) *ast.ArrayLiteral {
	return &ast.ArrayLiteral{Pos_: zpos(), Elements: elems}
}

func indexExpr(left, index ast.Expr) *ast.IndexExpr {
	return &ast.IndexExpr{Pos_: zpos(), Left: left, Index: index}
}

func sliceExpr(left, low, high ast.Expr) *ast.SliceExpr {
	return &ast.SliceExpr{Pos_: zpos(), Left: left, Low: low, High: high}
}

func fieldAccess(obj ast.Expr, field string) *ast.FieldAccess {
	return &ast.FieldAccess{Pos_: zpos(), Object: obj, Field: ident(field)}
}

func structLiteralExpr(typeName string, fields ...ast.StructField) *ast.StructLiteral {
	return &ast.StructLiteral{Pos_: zpos(), Type: ident(typeName), Fields: fields}
}

func structField(name string, value ast.Expr) ast.StructField {
	return ast.StructField{Pos_: zpos(), Key: ident(name), Value: value}
}

// =============================================================================
// AST declaration constructors
// =============================================================================

// mkParam builds a function parameter (pointer, as FnDecl.Params is []*Param).
func mkParam(name string, typ ast.TypeExpr) *ast.Param {
	return &ast.Param{Pos_: zpos(), Name: name, Type: typ}
}

func mkMutParam(name string, typ ast.TypeExpr) *ast.Param {
	return &ast.Param{Pos_: zpos(), Name: name, Type: typ, Mut: true}
}

func mkOwnParam(name string, typ ast.TypeExpr) *ast.Param {
	return &ast.Param{Pos_: zpos(), Name: name, Type: typ, Own: true}
}

// makeFn assembles an *ast.FnDecl.
func makeFn(name string, retType ast.TypeExpr, body *ast.BlockStmt, params ...*ast.Param) *ast.FnDecl {
	return &ast.FnDecl{
		Pos_:       zpos(),
		Name:       name,
		Params:     params,
		ReturnType: retType,
		Body:       body,
	}
}

func makeAsyncFn(name string, retType ast.TypeExpr, body *ast.BlockStmt, params ...*ast.Param) *ast.FnDecl {
	fn := makeFn(name, retType, body, params...)
	fn.IsAsync = true
	return fn
}

// mainFn wraps statements into a minimal `fn main() { ... }`.
func mainFn(stmts ...ast.Stmt) *ast.FnDecl {
	return makeFn("main", nil, blockStmt(stmts...))
}

// makeProgram wraps declarations into a *ast.Program.
func makeProgram(decls ...ast.Decl) *ast.Program {
	return &ast.Program{Decls: decls}
}

// =============================================================================
// Type declaration constructors
// =============================================================================

func structTypeDecl(name string, fields ...ast.StructTypeField) *ast.TypeDecl {
	return &ast.TypeDecl{
		Pos_: zpos(),
		Name: ident(name),
		Rhs:  &ast.StructTypeExpr{Fields: fields},
	}
}

func mkStructField(name string, typ ast.TypeExpr) ast.StructTypeField {
	return ast.StructTypeField{Name: name, Type: typ}
}

func sumTypeDecl(name string, variants ...ast.VariantTypeExpr) *ast.TypeDecl {
	return &ast.TypeDecl{
		Pos_: zpos(),
		Name: ident(name),
		Rhs:  &ast.SumTypeExpr{Variants: variants},
	}
}

// mkVariant constructs a VariantTypeExpr (used inside SumTypeExpr).
func mkVariant(name string, fields ...ast.StructTypeField) ast.VariantTypeExpr {
	return ast.VariantTypeExpr{Name: name, Fields: fields}
}

// =============================================================================
// Match expression constructors
// =============================================================================

func matchExpr(subject ast.Expr, arms ...ast.MatchArm) *ast.MatchExpr {
	return &ast.MatchExpr{Pos_: zpos(), Subject: subject, Arms: arms}
}

func matchArm(pattern ast.Pattern, body ast.Expr) ast.MatchArm {
	return ast.MatchArm{Pos_: zpos(), Pattern: pattern, Body: body}
}

func wildcardPattern() *ast.WildcardPattern {
	return &ast.WildcardPattern{Pos_: zpos()}
}

func literalPattern(value ast.Expr) *ast.LiteralPattern {
	return &ast.LiteralPattern{Pos_: zpos(), Value: value}
}

// For Struct and Unit variants (e.g., case Circle{radius: r}: or case None:)
func variantPattern(variantName string, fields ...ast.VariantPatternField) *ast.VariantPattern {
	return &ast.VariantPattern{
		Pos_:   zpos(),
		Name:   variantName,
		Fields: fields,
	}
}

// For Tuple variants (e.g., case Point(x, y):)
func tupleVariantPattern(variantName string, bindings ...string) *ast.VariantPattern {
	var tBindings []*ast.Identifier
	for _, b := range bindings {
		tBindings = append(tBindings, ident(b))
	}
	return &ast.VariantPattern{
		Pos_:          zpos(),
		Name:          variantName,
		TupleBindings: tBindings,
	}
}

func variantField(field, binding string) ast.VariantPatternField {
	return ast.VariantPatternField{Field: field, Binding: ident(binding)}
}

func mkTupleVariant(name string, tupleTypes ...ast.TypeExpr) ast.VariantTypeExpr {
	return ast.VariantTypeExpr{Name: name, TupleFields: tupleTypes}
}

// =============================================================================
// Analysis driver
// =============================================================================

// analyze runs the full semantic analysis pipeline and returns the TAST and
// any compiler errors produced.
func analyze(t *testing.T, program *ast.Program) (*tast.Program, []ast.CompileError) {
	t.Helper()
	return sema.New().Analyze(program)
}

// =============================================================================
// Assertion helpers
// =============================================================================

// assertNoErrors fails the test immediately if any semantic errors were produced.
func assertNoErrors(t *testing.T, errs []ast.CompileError) {
	t.Helper()
	require.Empty(t, errs, "expected no errors but got: %v", errs)
}

// assertHasError fails unless at least one error message contains substr.
func assertHasError(t *testing.T, errs []ast.CompileError, substr string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Msg, substr) {
			return
		}
	}
	require.Failf(t, "expected error not found",
		"wanted substring %q in errors:\n%v", substr, errs)
}

// =============================================================================
// Scope & Symbol Resolution
// =============================================================================

func TestScope_UndefinedName(t *testing.T) {
	program := makeProgram(
		mainFn(exprStmt(ident("ghost"))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined name 'ghost'")
}

func TestScope_BuiltinPutsResolvesWithoutError(t *testing.T) {
	// 'puts' is pre-seeded in the global scope and must resolve.
	program := makeProgram(
		mainFn(exprStmt(callExpr(ident("puts"), callArg(strLit("hello"))))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestScope_HoistedFunctionVisibleBeforeDeclaration(t *testing.T) {
	// Functions are hoisted in Pass 1, so a function declared after its caller
	// in source order must still be resolvable.
	caller := mainFn(exprStmt(callExpr(ident("helper"))))
	helper := makeFn("helper", intTypeExpr(), blockStmt(returnStmt(intLit(1))))
	program := makeProgram(caller, helper)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestScope_DuplicateLocalVariable(t *testing.T) {
	// Re-declaring a variable with the same name in the same scope is an error.
	program := makeProgram(
		mainFn(
			declareStmt("x", false, intLit(1)),
			declareStmt("x", false, intLit(2)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "already declared")
}

func TestScope_InnerScopeSeesOuterVariable(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("outer", false, intLit(42)),
			blockStmt(
				exprStmt(ident("outer")),
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestScope_InnerVariableNotLeakedToOuter(t *testing.T) {
	program := makeProgram(
		mainFn(
			blockStmt( // Removed exprStmt()
				declareStmt("inner", false, intLit(7)),
			),
			exprStmt(ident("inner")),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined name 'inner'")
}
func TestScope_ForLoopInitVariableScopedToLoop(t *testing.T) {
	// A variable introduced in a for-loop init clause must not escape the loop.
	program := makeProgram(
		mainFn(
			forStmt(
				declareStmt("i", true, intLit(0)),
				infixExpr(ident("i"), "<", intLit(10)),
				assignStmt(ident("i"), infixExpr(ident("i"), "+", intLit(1))),
				blockStmt(),
			),
			exprStmt(ident("i")), // 'i' must be out of scope here
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined name 'i'")
}

func TestScope_FunctionParamVisibleInsideBody(t *testing.T) {
	fn := makeFn("add", intTypeExpr(),
		blockStmt(returnStmt(infixExpr(ident("a"), "+", ident("b")))),
		mkParam("a", intTypeExpr()),
		mkParam("b", intTypeExpr()),
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestScope_FunctionParamNotVisibleInSiblingFunction(t *testing.T) {
	inner := makeFn("inner", nil, blockStmt(), mkParam("x", intTypeExpr()))
	outer := mainFn(exprStmt(ident("x"))) // 'x' must not bleed into outer
	program := makeProgram(inner, outer)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined name 'x'")
}

func TestScope_VariantConstructorRegisteredAsSymbol(t *testing.T) {
	// After a sum-type declaration, each variant name becomes a global symbol.
	colorType := sumTypeDecl("Color",
		mkVariant("Red"),
		mkVariant("Green"),
		mkVariant("Blue"),
	)
	program := makeProgram(
		colorType,
		mainFn(exprStmt(ident("Red"))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Literals
// =============================================================================

func TestExpr_Literals(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
	}{
		{"int literal", intLit(42)},
		{"bool literal true", boolLit(true)},
		{"bool literal false", boolLit(false)},
		{"string literal", strLit("hello")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(tc.expr)))
			_, errs := analyze(t, program)
			assertNoErrors(t, errs)
		})
	}
}

// =============================================================================
// Infix Expressions — valid combinations
// =============================================================================

func TestExpr_Infix_ValidOperations(t *testing.T) {
	tests := []struct {
		name  string
		left  ast.Expr
		op    string
		right ast.Expr
	}{
		{"int + int", intLit(1), "+", intLit(2)},
		{"int - int", intLit(5), "-", intLit(3)},
		{"int * int", intLit(4), "*", intLit(3)},
		{"int / int", intLit(9), "/", intLit(3)},
		{"int % int", intLit(7), "%", intLit(2)},
		{"int == int", intLit(1), "==", intLit(1)},
		{"int != int", intLit(1), "!=", intLit(2)},
		{"int < int", intLit(1), "<", intLit(2)},
		{"int <= int", intLit(1), "<=", intLit(1)},
		{"int > int", intLit(2), ">", intLit(1)},
		{"int >= int", intLit(1), ">=", intLit(1)},
		{"bool == bool", boolLit(true), "==", boolLit(false)},
		{"bool && bool", boolLit(true), "&&", boolLit(false)},
		{"bool || bool", boolLit(false), "||", boolLit(true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(infixExpr(tc.left, tc.op, tc.right))))
			_, errs := analyze(t, program)
			assertNoErrors(t, errs)
		})
	}
}

// =============================================================================
// Infix Expressions — type-error cases
// =============================================================================

func TestExpr_Infix_TypeErrors(t *testing.T) {
	tests := []struct {
		name    string
		left    ast.Expr
		op      string
		right   ast.Expr
		wantErr string
	}{
		{
			name: "int + bool",
			left: intLit(1), op: "+", right: boolLit(true),
			wantErr: "type mismatch",
		},
		{
			name: "bool + bool arithmetic fails",
			left: boolLit(true), op: "+", right: boolLit(false),
			wantErr: "type mismatch",
		},
		{
			name: "int && int non-boolean logic",
			left: intLit(1), op: "&&", right: intLit(2),
			wantErr: "type mismatch",
		},
		{
			name: "int || int non-boolean logic",
			left: intLit(0), op: "||", right: intLit(1),
			wantErr: "type mismatch",
		},
		{
			name: "string + int heterogeneous",
			left: strLit("a"), op: "+", right: intLit(1),
			wantErr: "type mismatch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(infixExpr(tc.left, tc.op, tc.right))))
			_, errs := analyze(t, program)
			assertHasError(t, errs, tc.wantErr)
		})
	}
}

// =============================================================================
// Prefix Expressions
// =============================================================================

func TestExpr_Prefix_Valid(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		right ast.Expr
	}{
		{"negate int", "-", intLit(5)},
		{"not bool", "!", boolLit(true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(prefixExpr(tc.op, tc.right))))
			_, errs := analyze(t, program)
			assertNoErrors(t, errs)
		})
	}
}

func TestExpr_Prefix_TypeErrors(t *testing.T) {
	tests := []struct {
		name    string
		op      string
		right   ast.Expr
		wantErr string
	}{
		{"not int", "!", intLit(1), "operator '!' expects 'bool'"},
		{"negate bool", "-", boolLit(false), "operator '-' expects 'int'"},
		{"negate string", "-", strLit("x"), "operator '-' expects 'int'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(prefixExpr(tc.op, tc.right))))
			_, errs := analyze(t, program)
			assertHasError(t, errs, tc.wantErr)
		})
	}
}

// =============================================================================
// If Expressions
// =============================================================================

func TestExpr_If_CondMustBeBool(t *testing.T) {
	tests := []struct {
		name string
		cond ast.Expr
	}{
		{"int condition", intLit(1)},
		{"string condition", strLit("yes")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(ifExpr(tc.cond, blockStmt(), nil))))
			_, errs := analyze(t, program)
			assertHasError(t, errs, "IF condition must be a boolean")
		})
	}
}

func TestExpr_If_ValidBoolCondition(t *testing.T) {
	program := makeProgram(mainFn(exprStmt(ifExpr(boolLit(true), blockStmt(), nil))))
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestExpr_If_BothBranchesYieldSameType(t *testing.T) {
	// When both branches yield the same type, the if-expression has that type.
	program := makeProgram(mainFn(exprStmt(
		ifExpr(
			boolLit(true),
			blockStmt(yieldStmt(intLit(1))),
			blockStmt(yieldStmt(intLit(2))),
		),
	)))
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestExpr_If_NoElseBranchIsValid(t *testing.T) {
	// An if without else is always valid (result is unit).
	program := makeProgram(mainFn(exprStmt(
		ifExpr(boolLit(false), blockStmt(yieldStmt(intLit(0))), nil),
	)))
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Call Expressions
// =============================================================================

func TestExpr_Call_Ok(t *testing.T) {
	fn := makeFn("greet", intTypeExpr(),
		blockStmt(returnStmt(intLit(0))),
		mkParam("name", strTypeExpr()),
	)
	program := makeProgram(
		fn,
		mainFn(exprStmt(callExpr(ident("greet"), callArg(strLit("world"))))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestExpr_Call_ArityErrors(t *testing.T) {
	twoArgFn := makeFn("f", nil, blockStmt(),
		mkParam("a", intTypeExpr()),
		mkParam("b", intTypeExpr()),
	)
	tests := []struct {
		name string
		args []ast.CallArg
	}{
		{"too few arguments", []ast.CallArg{callArg(intLit(1))}},
		{"too many arguments", []ast.CallArg{callArg(intLit(1)), callArg(intLit(2)), callArg(intLit(3))}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(
				twoArgFn,
				mainFn(exprStmt(callExpr(ident("f"), tc.args...))),
			)
			_, errs := analyze(t, program)
			assertHasError(t, errs, "wrong number of arguments")
		})
	}
}

func TestExpr_Call_ArgTypeMismatch(t *testing.T) {
	fn := makeFn("takesInt", nil, blockStmt(), mkParam("n", intTypeExpr()))
	program := makeProgram(
		fn,
		mainFn(exprStmt(callExpr(ident("takesInt"), callArg(boolLit(true))))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "type mismatch")
}

func TestExpr_Call_OwnershipModifiers(t *testing.T) {
	tests := []struct {
		name    string
		paramFn func(string, ast.TypeExpr) *ast.Param
		argFn   func(ast.Expr) ast.CallArg
		wantErr string
	}{
		{
			name:    "mut required, not provided",
			paramFn: mkMutParam,
			argFn:   callArg,
			wantErr: "requires 'mut' modifier",
		},
		{
			name:    "own required, not provided",
			paramFn: mkOwnParam,
			argFn:   callArg,
			wantErr: "requires 'own' modifier",
		},
		{
			name:    "plain borrow, mut incorrectly supplied",
			paramFn: mkParam,
			argFn:   mutArg,
			wantErr: "does not take ownership modifiers",
		},
		{
			name:    "own expected but mut supplied",
			paramFn: mkOwnParam,
			argFn:   mutArg,
			wantErr: "expects 'own', got 'mut'",
		},
		{
			name:    "mut expected but own supplied",
			paramFn: mkMutParam,
			argFn:   ownArg,
			wantErr: "expects 'mut', got 'own'",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn := makeFn("target", nil, blockStmt(), tc.paramFn("x", intTypeExpr()))
			program := makeProgram(
				fn,
				mainFn(
					declareStmt("v", true, intLit(1)),
					exprStmt(callExpr(ident("target"), tc.argFn(ident("v")))),
				),
			)
			_, errs := analyze(t, program)
			assertHasError(t, errs, tc.wantErr)
		})
	}
}

func TestExpr_Call_CorrectModifiers_NoError(t *testing.T) {
	tests := []struct {
		name    string
		paramFn func(string, ast.TypeExpr) *ast.Param
		argFn   func(ast.Expr) ast.CallArg
	}{
		{"mut provided for mut param", mkMutParam, mutArg},
		{"own provided for own param", mkOwnParam, ownArg},
		{"no modifier for borrow param", mkParam, callArg},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn := makeFn("target", nil, blockStmt(), tc.paramFn("x", intTypeExpr()))
			program := makeProgram(
				fn,
				mainFn(
					declareStmt("v", true, intLit(1)),
					exprStmt(callExpr(ident("target"), tc.argFn(ident("v")))),
				),
			)
			_, errs := analyze(t, program)
			assertNoErrors(t, errs)
		})
	}
}

// =============================================================================
// Await Expressions
// =============================================================================

func TestExpr_Await_Errors(t *testing.T) {
	tests := []struct {
		name    string
		fn      func() *ast.FnDecl
		wantErr string
	}{
		{
			name: "await outside async function",
			fn: func() *ast.FnDecl {
				return makeFn("syncFn", nil,
					blockStmt(exprStmt(awaitExpr(intLit(0)))),
				)
			},
			wantErr: "cannot use 'await' outside of an async function",
		},
		{
			name: "await non-task type",
			fn: func() *ast.FnDecl {
				return makeAsyncFn("asyncFn", nil,
					blockStmt(exprStmt(awaitExpr(intLit(42)))),
				)
			},
			wantErr: "cannot await non-task type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(tc.fn())
			_, errs := analyze(t, program)
			assertHasError(t, errs, tc.wantErr)
		})
	}
}

// =============================================================================
// Array Literals
// =============================================================================

func TestExpr_ArrayLiteral(t *testing.T) {
	tests := []struct {
		name    string
		elems   []ast.Expr
		wantErr string
	}{
		{
			name:    "empty array cannot be typed",
			elems:   nil,
			wantErr: "cannot infer type of empty array literal",
		},
		{
			name:    "heterogeneous elements",
			elems:   []ast.Expr{intLit(1), boolLit(true)},
			wantErr: "type mismatch",
		},
		{
			name:  "homogeneous int elements",
			elems: []ast.Expr{intLit(1), intLit(2), intLit(3)},
		},
		{
			name:  "single element",
			elems: []ast.Expr{strLit("only")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(mainFn(exprStmt(arrayLiteral(tc.elems...))))
			_, errs := analyze(t, program)
			if tc.wantErr != "" {
				assertHasError(t, errs, tc.wantErr)
			} else {
				assertNoErrors(t, errs)
			}
		})
	}
}

// =============================================================================
// Index Expressions
// =============================================================================

func TestExpr_IndexExpr(t *testing.T) {
	tests := []struct {
		name    string
		setup   []ast.Stmt
		expr    ast.Expr
		wantErr string
	}{
		{
			name: "valid array index",
			setup: []ast.Stmt{
				declareStmt("arr", false, arrayLiteral(intLit(10), intLit(20))),
			},
			expr: indexExpr(ident("arr"), intLit(0)),
		},
		{
			name: "non-integer index",
			setup: []ast.Stmt{
				declareStmt("arr", false, arrayLiteral(intLit(1), intLit(2))),
			},
			expr:    indexExpr(ident("arr"), boolLit(true)),
			wantErr: "index must be an integer",
		},
		{
			name: "index into non-array type",
			setup: []ast.Stmt{
				declareStmt("n", false, intLit(5)),
			},
			expr:    indexExpr(ident("n"), intLit(0)),
			wantErr: "cannot index non-array/slice type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stmts := append(tc.setup, exprStmt(tc.expr))
			program := makeProgram(makeFn("f", nil, blockStmt(stmts...)))
			_, errs := analyze(t, program)
			if tc.wantErr != "" {
				assertHasError(t, errs, tc.wantErr)
			} else {
				assertNoErrors(t, errs)
			}
		})
	}
}

// =============================================================================
// Slice Expressions
// =============================================================================

func TestExpr_SliceExpr(t *testing.T) {
	tests := []struct {
		name    string
		setup   []ast.Stmt
		expr    ast.Expr
		wantErr string
	}{
		{
			name: "valid array slice",
			setup: []ast.Stmt{
				declareStmt("arr", false, arrayLiteral(intLit(1), intLit(2), intLit(3))),
			},
			expr: sliceExpr(ident("arr"), intLit(0), intLit(2)),
		},
		{
			name: "non-sliceable type",
			setup: []ast.Stmt{
				declareStmt("n", false, intLit(5)),
			},
			expr:    sliceExpr(ident("n"), intLit(0), intLit(1)),
			wantErr: "cannot slice non-array/slice type",
		},
		{
			name: "non-integer low bound",
			setup: []ast.Stmt{
				declareStmt("arr", false, arrayLiteral(intLit(1), intLit(2))),
			},
			expr:    sliceExpr(ident("arr"), boolLit(true), intLit(2)),
			wantErr: "slice low index must be an integer",
		},
		{
			name: "non-integer high bound",
			setup: []ast.Stmt{
				declareStmt("arr", false, arrayLiteral(intLit(1), intLit(2))),
			},
			expr:    sliceExpr(ident("arr"), intLit(0), boolLit(false)),
			wantErr: "slice high index must be an integer",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stmts := append(tc.setup, exprStmt(tc.expr))
			program := makeProgram(makeFn("f", nil, blockStmt(stmts...)))
			_, errs := analyze(t, program)
			if tc.wantErr != "" {
				assertHasError(t, errs, tc.wantErr)
			} else {
				assertNoErrors(t, errs)
			}
		})
	}
}

// =============================================================================
// Field Access
// =============================================================================

func TestExpr_FieldAccess(t *testing.T) {
	pointDecl := structTypeDecl("Point",
		mkStructField("x", intTypeExpr()),
		mkStructField("y", intTypeExpr()),
	)

	tests := []struct {
		name    string
		decls   []ast.Decl // prepended to program
		setup   []ast.Stmt
		expr    ast.Expr
		wantErr string
	}{
		{
			name:  "valid field access on struct",
			decls: []ast.Decl{pointDecl},
			setup: []ast.Stmt{
				declareStmt("p", false, structLiteralExpr("Point",
					structField("x", intLit(1)),
					structField("y", intLit(2)),
				)),
			},
			expr: fieldAccess(ident("p"), "x"),
		},
		{
			name:  "unknown field on struct",
			decls: []ast.Decl{pointDecl},
			setup: []ast.Stmt{
				declareStmt("p", false, structLiteralExpr("Point",
					structField("x", intLit(0)),
					structField("y", intLit(0)),
				)),
			},
			expr:    fieldAccess(ident("p"), "z"),
			wantErr: "field 'z' does not exist on struct",
		},
		{
			name: "field access on non-struct type",
			setup: []ast.Stmt{
				declareStmt("n", false, intLit(5)),
			},
			expr:    fieldAccess(ident("n"), "x"),
			wantErr: "cannot access field 'x' on non-struct type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stmts := append(tc.setup, exprStmt(tc.expr))
			fn := makeFn("f", nil, blockStmt(stmts...))
			decls := append(tc.decls, fn)
			program := makeProgram(decls...)
			_, errs := analyze(t, program)
			if tc.wantErr != "" {
				assertHasError(t, errs, tc.wantErr)
			} else {
				assertNoErrors(t, errs)
			}
		})
	}
}

// =============================================================================
// Declare Statements
// =============================================================================

func TestStmt_Declare_Ok(t *testing.T) {
	// A basic let-binding should introduce a symbol with the correct type.
	program := makeProgram(
		mainFn(declareStmt("x", false, intLit(42))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Declare_MutableOk(t *testing.T) {
	program := makeProgram(
		mainFn(declareStmt("x", true, intLit(0))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Declare_DuplicateInSameScope(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("x", false, intLit(1)),
			declareStmt("x", false, intLit(2)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "already declared")
}

func TestStmt_Declare_UnitRhsIsError(t *testing.T) {
	// Assigning the result of a unit-returning function is not allowed.
	unitFn := makeFn("nothing", unitTypeExpr(), blockStmt())
	program := makeProgram(
		unitFn,
		mainFn(declareStmt("x", false, callExpr(ident("nothing")))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "cannot assign the result of a function that returns 'unit'")
}

func TestStmt_Declare_ShadowingInInnerScopeOk(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("x", false, intLit(1)),
			blockStmt( // Removed exprStmt()
				declareStmt("x", false, boolLit(true)),
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Assign Statements
// =============================================================================

func TestStmt_Assign_Ok(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("x", true, intLit(0)),
			assignStmt(ident("x"), intLit(99)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Assign_ImmutableVariable_Error(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("x", false, intLit(0)), // immutable (no 'mut')
			assignStmt(ident("x"), intLit(1)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "cannot mutate immutable variable 'x'")
}

func TestStmt_Assign_TypeMismatch_Error(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("x", true, intLit(0)),
			assignStmt(ident("x"), boolLit(true)), // bool into int
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "type mismatch")
}

func TestStmt_Assign_NonVariableLValue_Error(t *testing.T) {
	// Assigning to a literal expression has no root symbol and must error.
	program := makeProgram(
		mainFn(assignStmt(intLit(0), intLit(1))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "cannot assign to non-variable expression")
}

func TestStmt_Assign_FieldOfMutableStruct(t *testing.T) {
	// Assigning through a field access on a mutable struct should succeed.
	pointDecl := structTypeDecl("Point",
		mkStructField("x", intTypeExpr()),
		mkStructField("y", intTypeExpr()),
	)
	fn := makeFn("f", nil,
		blockStmt(
			declareStmt("p", true, structLiteralExpr("Point",
				structField("x", intLit(0)),
				structField("y", intLit(0)),
			)),
			assignStmt(fieldAccess(ident("p"), "x"), intLit(10)),
		),
	)
	program := makeProgram(pointDecl, fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Assign_ArrayIndex_Mutable(t *testing.T) {
	fn := makeFn("f", nil,
		blockStmt(
			declareStmt("arr", true, arrayLiteral(intLit(1), intLit(2), intLit(3))),
			assignStmt(indexExpr(ident("arr"), intLit(0)), intLit(99)),
		),
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Return Statements
// =============================================================================

func TestStmt_Return_MatchesReturnType(t *testing.T) {
	fn := makeFn("f", intTypeExpr(),
		blockStmt(returnStmt(intLit(42))),
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Return_WrongType_Error(t *testing.T) {
	fn := makeFn("f", intTypeExpr(),
		blockStmt(returnStmt(boolLit(true))), // returning bool from int fn
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "type mismatch: expected return type")
}

func TestStmt_Return_VoidFunctionReturnsNothing(t *testing.T) {
	fn := makeFn("f", nil,
		blockStmt(returnStmt(nil)), // bare return
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Return_MultipleReturns(t *testing.T) {
	// Multiple return statements all must match the declared return type.
	fn := makeFn("f", intTypeExpr(),
		blockStmt(
			exprStmt(ifExpr(
				boolLit(true),
				blockStmt(returnStmt(intLit(1))),
				blockStmt(returnStmt(intLit(0))),
			)),
			returnStmt(intLit(42)),
		),
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_Return_TypeErrors(t *testing.T) {
	tests := []struct {
		name    string
		retType ast.TypeExpr
		value   ast.Expr
		wantErr string
	}{
		{
			name:    "bool returned from int function",
			retType: intTypeExpr(),
			value:   boolLit(true),
			wantErr: "type mismatch: expected return type",
		},
		{
			name:    "string returned from int function",
			retType: intTypeExpr(),
			value:   strLit("oops"),
			wantErr: "type mismatch: expected return type",
		},
		{
			name:    "int returned from bool function",
			retType: boolTypeExpr(),
			value:   intLit(0),
			wantErr: "type mismatch: expected return type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn := makeFn("f", tc.retType, blockStmt(returnStmt(tc.value)))
			program := makeProgram(fn)
			_, errs := analyze(t, program)
			assertHasError(t, errs, tc.wantErr)
		})
	}
}

// =============================================================================
// For Statements
// =============================================================================

func TestStmt_For_FullForm_Ok(t *testing.T) {
	// for i := 0; i < 10; i = i + 1 { }
	program := makeProgram(
		mainFn(
			forStmt(
				declareStmt("i", true, intLit(0)),
				infixExpr(ident("i"), "<", intLit(10)),
				assignStmt(ident("i"), infixExpr(ident("i"), "+", intLit(1))),
				blockStmt(),
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_For_ConditionMustBeBool_Error(t *testing.T) {
	program := makeProgram(
		mainFn(
			forStmt(nil, intLit(1), nil, blockStmt()),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "condition must be of type 'bool'")
}

func TestStmt_For_ConditionCanBeVariable(t *testing.T) {
	program := makeProgram(
		mainFn(
			declareStmt("running", true, boolLit(true)),
			forStmt(nil, ident("running"), nil, blockStmt(
				assignStmt(ident("running"), boolLit(false)),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStmt_For_BodySeesInitVariable(t *testing.T) {
	// The loop body must be able to see the init variable.
	program := makeProgram(
		mainFn(
			forStmt(
				declareStmt("i", true, intLit(0)),
				infixExpr(ident("i"), "<", intLit(5)),
				assignStmt(ident("i"), infixExpr(ident("i"), "+", intLit(1))),
				blockStmt(exprStmt(ident("i"))), // 'i' must resolve
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Break and Continue
// =============================================================================

func TestStmt_BreakAndContinue_EmittedAsTastNodes(t *testing.T) {
	// break and continue are simple pass-through statements – no errors.
	program := makeProgram(
		mainFn(
			forStmt(
				nil,
				boolLit(true),
				nil,
				blockStmt(
					&ast.BreakStmt{},
				),
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Yield Statements
// =============================================================================

func TestStmt_Yield_SetsBlockType(t *testing.T) {
	// A yield at the end of an if-branch dictates the branch's type.
	program := makeProgram(
		mainFn(exprStmt(
			ifExpr(
				boolLit(true),
				blockStmt(yieldStmt(intLit(7))),
				blockStmt(yieldStmt(intLit(8))),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// getRootSymbol – mutation path resolution
// =============================================================================

func TestStmt_Assign_MutatesViaSlice(t *testing.T) {
	// Assigning through a slice expression should resolve to the base symbol.
	fn := makeFn("f", nil,
		blockStmt(
			declareStmt("arr", true, arrayLiteral(intLit(1), intLit(2), intLit(3))),
			declareStmt("s", true, sliceExpr(ident("arr"), intLit(0), intLit(2))),
			assignStmt(indexExpr(ident("s"), intLit(0)), intLit(42)),
		),
	)
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Type Declaration — Struct Types
// =============================================================================

func TestTypeDecl_Struct_Ok(t *testing.T) {
	// A basic struct declaration with two fields must produce no errors.
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_Struct_MultipleFieldTypes(t *testing.T) {
	program := makeProgram(
		structTypeDecl("Record",
			mkStructField("id", intTypeExpr()),
			mkStructField("name", strTypeExpr()),
			mkStructField("active", boolTypeExpr()),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_Struct_Duplicate_Error(t *testing.T) {
	// Two type declarations with the same name should be caught.
	program := makeProgram(
		structTypeDecl("Dup", mkStructField("x", intTypeExpr())),
		structTypeDecl("Dup", mkStructField("y", intTypeExpr())),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "already defined")
}

// =============================================================================
// Type Declaration — Sum Types
// =============================================================================

func TestTypeDecl_SumType_Ok(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
			mkVariant("Rectangle",
				mkStructField("width", intTypeExpr()),
				mkStructField("height", intTypeExpr()),
			),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_SumType_VariantsRegisteredAsSymbols(t *testing.T) {
	// After declaring a sum type, each variant name must be a callable symbol.
	program := makeProgram(
		sumTypeDecl("Result",
			mkVariant("Ok", mkStructField("value", intTypeExpr())),
			mkVariant("Err", mkStructField("code", intTypeExpr())),
		),
		mainFn(exprStmt(ident("Ok"))), // must resolve
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_SumType_Duplicate_Error(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Dup", mkVariant("A")),
		sumTypeDecl("Dup", mkVariant("B")),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "already defined")
}

// =============================================================================
// resolveAstType — built-in and generic types
// =============================================================================

func TestTypeDecl_ResolveBuiltinTypes(t *testing.T) {
	// Each primitive type must be usable as a function return type without error.
	tests := []struct {
		name string
		ret  ast.TypeExpr
	}{
		{"int return type", intTypeExpr()},
		{"bool return type", boolTypeExpr()},
		{"string return type", strTypeExpr()},
		{"unit return type", unitTypeExpr()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			program := makeProgram(makeFn("f", tc.ret, blockStmt()))
			_, errs := analyze(t, program)
			assertNoErrors(t, errs)
		})
	}
}

func TestTypeDecl_ResolveArrayType(t *testing.T) {
	fn := makeFn("f", arrayTypeExpr(intTypeExpr(), 3), blockStmt())
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_ResolveSliceType(t *testing.T) {
	fn := makeFn("f", sliceTypeExpr(intTypeExpr()), blockStmt())
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestTypeDecl_UnknownTypeName_Error(t *testing.T) {
	fn := makeFn("f", namedType("Phantom"), blockStmt())
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "unknown type")
}

func TestTypeDecl_CustomStructAsReturnType(t *testing.T) {
	// A user-defined struct should be usable as a return type annotation.
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
		makeFn("makePoint", namedType("Point"), blockStmt(
			returnStmt(structLiteralExpr("Point",
				structField("x", intLit(0)),
				structField("y", intLit(0)),
			)),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Struct Literals
// =============================================================================

func TestStructLiteral_AllFieldsPresent_Ok(t *testing.T) {
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
		mainFn(exprStmt(structLiteralExpr("Point",
			structField("x", intLit(1)),
			structField("y", intLit(2)),
		))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestStructLiteral_MissingField_Error(t *testing.T) {
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
		mainFn(exprStmt(structLiteralExpr("Point",
			structField("x", intLit(1)), // y is missing
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "missing field")
}

func TestStructLiteral_WrongFieldType_Error(t *testing.T) {
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
		mainFn(exprStmt(structLiteralExpr("Point",
			structField("x", boolLit(true)), // bool instead of int
			structField("y", intLit(2)),
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "type mismatch for field 'x'")
}

func TestStructLiteral_DuplicateField_Error(t *testing.T) {
	program := makeProgram(
		structTypeDecl("Point",
			mkStructField("x", intTypeExpr()),
			mkStructField("y", intTypeExpr()),
		),
		mainFn(exprStmt(structLiteralExpr("Point",
			structField("x", intLit(1)),
			structField("x", intLit(2)), // duplicate
			structField("y", intLit(3)),
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "duplicate field 'x'")
}

func TestStructLiteral_UndefinedStructType_Error(t *testing.T) {
	program := makeProgram(
		mainFn(exprStmt(structLiteralExpr("Ghost",
			structField("x", intLit(1)),
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined struct type 'Ghost'")
}

// =============================================================================
// Variant Literals (struct literal syntax for sum-type variants)
// =============================================================================

func TestVariantLiteral_Ok(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
		),
		mainFn(exprStmt(structLiteralExpr("Circle",
			structField("radius", intLit(5)),
		))),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestVariantLiteral_MissingField_Error(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle",
				mkStructField("radius", intTypeExpr()),
				mkStructField("colour", strTypeExpr()),
			),
		),
		mainFn(exprStmt(structLiteralExpr("Circle",
			structField("radius", intLit(5)), // colour is missing
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "missing field")
}

func TestVariantLiteral_WrongFieldType_Error(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
		),
		mainFn(exprStmt(structLiteralExpr("Circle",
			structField("radius", strLit("big")), // string instead of int
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "type mismatch for field 'radius'")
}

func TestVariantLiteral_DuplicateField_Error(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
		),
		mainFn(exprStmt(structLiteralExpr("Circle",
			structField("radius", intLit(1)),
			structField("radius", intLit(2)), // duplicate
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "duplicate field 'radius'")
}

func TestVariantLiteral_UnknownField_Error(t *testing.T) {
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
		),
		mainFn(exprStmt(structLiteralExpr("Circle",
			structField("radius", intLit(5)),
			structField("colour", strLit("red")), // 'colour' not on variant
		))),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "field 'colour' does not exist on variant")
}

// =============================================================================
// Exhaustiveness Checking
// =============================================================================

func TestMatch_WildcardAlwaysExhaustive(t *testing.T) {
	// A single wildcard arm is always exhaustive, regardless of subject type.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(5),
				matchArm(wildcardPattern(), intLit(0)),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_SumType_AllVariantsCovered_Ok(t *testing.T) {
	// Covering every variant of a sum type is exhaustive.
	program := makeProgram(
		sumTypeDecl("Color",
			mkVariant("Red"),
			mkVariant("Green"),
			mkVariant("Blue"),
		),
		mainFn(
			declareStmt("c", false, structLiteralExpr("Red")),
			exprStmt(matchExpr(ident("c"),
				matchArm(variantPattern("Red"), intLit(1)),
				matchArm(variantPattern("Green"), intLit(2)),
				matchArm(variantPattern("Blue"), intLit(3)),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_SumType_MissingVariant_Error(t *testing.T) {
	// Omitting one variant from the arms must produce a non-exhaustive error.
	program := makeProgram(
		sumTypeDecl("Color",
			mkVariant("Red"),
			mkVariant("Green"),
			mkVariant("Blue"),
		),
		mainFn(
			declareStmt("c", false, structLiteralExpr("Red")),
			exprStmt(matchExpr(ident("c"),
				matchArm(variantPattern("Red"), intLit(1)),
				matchArm(variantPattern("Green"), intLit(2)),
				// Blue is missing
			)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "non-exhaustive match")
}

func TestMatch_NonSumType_WithoutWildcard_Error(t *testing.T) {
	// Matching on a non-sum type (e.g. int) without a wildcard is an error.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(1),
				matchArm(literalPattern(intLit(1)), strLit("one")),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "requires a wildcard '_' pattern")
}

func TestMatch_NonSumType_WithWildcard_Ok(t *testing.T) {
	// A wildcard covers non-sum types.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(1),
				matchArm(literalPattern(intLit(1)), strLit("one")),
				matchArm(wildcardPattern(), strLit("other")),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_SumType_WildcardCoversRemainder_Ok(t *testing.T) {
	// A wildcard at the end makes the match exhaustive without all variants.
	program := makeProgram(
		sumTypeDecl("Color",
			mkVariant("Red"),
			mkVariant("Green"),
			mkVariant("Blue"),
		),
		mainFn(
			declareStmt("c", false, structLiteralExpr("Red")),
			exprStmt(matchExpr(ident("c"),
				matchArm(variantPattern("Red"), intLit(1)),
				matchArm(wildcardPattern(), intLit(0)), // covers Green and Blue
			)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Literal Patterns
// =============================================================================

func TestMatch_LiteralPattern_Ok(t *testing.T) {
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(42),
				matchArm(literalPattern(intLit(42)), strLit("forty-two")),
				matchArm(wildcardPattern(), strLit("other")),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_LiteralPattern_TypeMismatch_Error(t *testing.T) {
	// Matching an int subject against a bool pattern is a type error.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(1),
				matchArm(literalPattern(boolLit(true)), strLit("yes")),
				matchArm(wildcardPattern(), strLit("no")),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "pattern type mismatch")
}

// =============================================================================
// Variant Patterns
// =============================================================================

func TestMatch_VariantPattern_BadVariantName_Error(t *testing.T) {
	// Referencing a non-existent variant in a pattern must produce an error.
	program := makeProgram(
		sumTypeDecl("Color",
			mkVariant("Red"),
			mkVariant("Green"),
		),
		mainFn(
			declareStmt("c", false, structLiteralExpr("Red")),
			exprStmt(matchExpr(ident("c"),
				matchArm(variantPattern("Red"), intLit(1)),
				matchArm(variantPattern("Purple"), intLit(2)), // does not exist
				matchArm(variantPattern("Green"), intLit(3)),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "does not exist on sum type")
}

func TestMatch_VariantPattern_BadFieldName_Error(t *testing.T) {
	// Destructuring a field that doesn't exist on the variant is an error.
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
			mkVariant("Square", mkStructField("side", intTypeExpr())),
		),
		mainFn(
			declareStmt("s", false, structLiteralExpr("Circle",
				structField("radius", intLit(5)),
			)),
			exprStmt(matchExpr(ident("s"),
				matchArm(
					variantPattern("Circle",
						variantField("diameter", "d"), // 'diameter' doesn't exist
					),
					ident("d"),
				),
				matchArm(variantPattern("Square"), intLit(0)),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "field 'diameter' does not exist on variant")
}

func TestMatch_VariantPattern_NonSumSubject_Error(t *testing.T) {
	// Using a variant pattern on a non-sum-type subject must error.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(1),
				matchArm(variantPattern("Some"), intLit(0)),
				matchArm(wildcardPattern(), intLit(1)),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "cannot match variant pattern on non-sum type")
}

// =============================================================================
// Destructuring Bindings
// =============================================================================

func TestMatch_VariantDestructuring_BindingVisibleInBody(t *testing.T) {
	// Fields destructured in a variant pattern must be accessible in the arm body.
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
			mkVariant("Square", mkStructField("side", intTypeExpr())),
		),
		mainFn(
			declareStmt("s", false, structLiteralExpr("Circle",
				structField("radius", intLit(10)),
			)),
			exprStmt(matchExpr(ident("s"),
				matchArm(
					variantPattern("Circle", variantField("radius", "r")),
					ident("r"), // 'r' must be visible here
				),
				matchArm(
					variantPattern("Square", variantField("side", "side")),
					ident("side"),
				),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_VariantDestructuring_BindingNotLeakedToSiblingArm(t *testing.T) {
	// A binding from one arm must not be visible in another arm.
	program := makeProgram(
		sumTypeDecl("Shape",
			mkVariant("Circle", mkStructField("radius", intTypeExpr())),
			mkVariant("Square", mkStructField("side", intTypeExpr())),
		),
		mainFn(
			declareStmt("s", false, structLiteralExpr("Circle",
				structField("radius", intLit(3)),
			)),
			exprStmt(matchExpr(ident("s"),
				matchArm(
					variantPattern("Circle", variantField("radius", "r")),
					intLit(1),
				),
				matchArm(
					variantPattern("Square", variantField("side", "side")),
					ident("r"), // 'r' from Circle arm must NOT be visible here
				),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "undefined name 'r'")
}

// =============================================================================
// Match Expression Type Unification
// =============================================================================

func TestMatch_ArmTypeUnification_AllSameType_Ok(t *testing.T) {
	// All arms yielding int — the match expression should type-check cleanly.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(5),
				matchArm(literalPattern(intLit(1)), intLit(100)),
				matchArm(literalPattern(intLit(2)), intLit(200)),
				matchArm(wildcardPattern(), intLit(0)),
			),
		)),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_ArmTypeUnification_MixedTypes_NoErrorFromAnalyser(t *testing.T) {
	// The analyser uses mergeTypes which returns Unknown for conflicting types
	// but does NOT itself emit an error — that check is the caller's concern.
	// This test documents the current behaviour.
	program := makeProgram(
		mainFn(exprStmt(
			matchExpr(intLit(1),
				matchArm(literalPattern(intLit(1)), intLit(42)),
				matchArm(wildcardPattern(), strLit("fallback")), // int vs string
			),
		)),
	)
	// No assertion on errors here — we are verifying the analyser does not panic.
	_, _ = analyze(t, program)
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestMatch_EmptyArms_NonExhaustive_Error(t *testing.T) {
	// A match with no arms on a sum type is non-exhaustive.
	program := makeProgram(
		sumTypeDecl("Color",
			mkVariant("Red"),
		),
		mainFn(
			declareStmt("c", false, structLiteralExpr("Red")),
			exprStmt(matchExpr(ident("c"))), // no arms
		),
	)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "non-exhaustive match")
}

func TestMatch_MultipleFieldDestructuring_Ok(t *testing.T) {
	// Destructuring multiple fields from a single variant.
	program := makeProgram(
		sumTypeDecl("Pair",
			mkVariant("Both",
				mkStructField("first", intTypeExpr()),
				mkStructField("second", strTypeExpr()),
			),
		),
		mainFn(
			declareStmt("p", false, structLiteralExpr("Both",
				structField("first", intLit(1)),
				structField("second", strLit("hello")),
			)),
			exprStmt(matchExpr(ident("p"),
				matchArm(
					variantPattern("Both",
						variantField("first", "a"),
						variantField("second", "b"),
					),
					// Both bindings must be visible
					callExpr(ident("puts"), callArg(ident("b"))),
				),
			)),
		),
	)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

// =============================================================================
// Async main restriction
// =============================================================================

func TestFunction_AsyncMainForbidden(t *testing.T) {
	fn := makeAsyncFn("main", nil, blockStmt())
	program := makeProgram(fn)
	_, errs := analyze(t, program)
	assertHasError(t, errs, "cannot be async")
}

// =============================================================================
// Function hoisting: two-pass ensures mutual recursion compiles
// =============================================================================

func TestFunction_MutualRecursionResolvable(t *testing.T) {
	// isEven calls isOdd and vice-versa.  Both must hoist before either body
	// is checked.
	isEven := makeFn("isEven", boolTypeExpr(),
		blockStmt(
			returnStmt(callExpr(ident("isOdd"), callArg(intLit(0)))),
		),
		mkParam("n", intTypeExpr()),
	)
	isOdd := makeFn("isOdd", boolTypeExpr(),
		blockStmt(
			returnStmt(callExpr(ident("isEven"), callArg(intLit(0)))),
		),
		mkParam("n", intTypeExpr()),
	)
	program := makeProgram(isEven, isOdd)
	_, errs := analyze(t, program)
	assertNoErrors(t, errs)
}

func TestMatch_TupleVariantDestructuring(t *testing.T) {
	t.Run("Destructured variables are visible in body", func(t *testing.T) {
		program := makeProgram(
			sumTypeDecl("Shape", mkTupleVariant("Point", intTypeExpr(), intTypeExpr())),
			mainFn(
				declareStmt("p", false, callExpr(ident("Point"), callArg(intLit(10)), callArg(intLit(20)))), // Assuming CallExpr instantiation for tuples
				exprStmt(matchExpr(ident("p"),
					matchArm(
						tupleVariantPattern("Point", "x", "y"),
						infixExpr(ident("x"), "+", ident("y")), // x and y must be resolvable here
					),
				)),
			),
		)
		_, errs := analyze(t, program)
		// We expect no errors regarding undefined names!
		assertNoErrors(t, errs)
	})

	t.Run("Wrong number of bindings throws error", func(t *testing.T) {
		program := makeProgram(
			sumTypeDecl("Shape", mkTupleVariant("Point", intTypeExpr(), intTypeExpr())),
			mainFn(
				declareStmt("p", false, callExpr(ident("Point"), callArg(intLit(10)), callArg(intLit(20)))),
				exprStmt(matchExpr(ident("p"),
					matchArm(
						tupleVariantPattern("Point", "x"), // Only provided 1 binding for a 2-tuple
						intLit(1),
					),
				)),
			),
		)
		_, errs := analyze(t, program)
		assertHasError(t, errs, "wrong number of bindings for tuple variant")
	})
}

// Helper so test file compiles even with no direct ast reference on all paths.
var _ ast.Expr = (*ast.IntLiteral)(nil)
