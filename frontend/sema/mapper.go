package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// Ensure Analyzer implements the generated ASTMapper interface at compile time.
var _ ast.ASTMapper[tast.Node] = (*Analyzer)(nil)

// =============================================================================
// ctx() & Error Handling
// =============================================================================

func (a *Analyzer) ctx() *RuleContext {
	return &RuleContext{
		Scope:          a.scope,
		ExpectedReturn: a.expectedReturn,
		IsAsync:        a.currentFn != nil && a.currentFn.IsAsync,
	}
}

func (a *Analyzer) drainViolations(vs []Violation) {
	for _, v := range vs {
		a.errorf(v.Pos, "%s", v.Msg)
	}
}

// =============================================================================
// Strongly-Typed Dispatchers (The "any" boundary)
// =============================================================================

// These helpers wrap the generic AcceptMapper call so the rest of the code
// doesn't have to deal with type assertions.

func (a *Analyzer) mapDecl(decl ast.Decl) tast.Decl {
	if res := ast.MapNode(decl, a); res != nil {
		return res.(tast.Decl)
	}
	return nil
}

func (a *Analyzer) mapStmt(stmt ast.Stmt) tast.Stmt {
	if res := ast.MapNode(stmt, a); res != nil {
		return res.(tast.Stmt)
	}
	return nil
}

func (a *Analyzer) mapExpr(expr ast.Expr) tast.Expr {
	if res := ast.MapNode(expr, a); res != nil {
		return res.(tast.Expr)
	}
	return nil
}

func (a *Analyzer) mapPattern(pat ast.Pattern, subjectType types.Type) tast.Pattern {
	if pat == nil {
		return nil
	}
	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return &tast.WildcardPattern{Pos_: p.Pos_}

	case *ast.LiteralPattern:
		tastVal := a.mapExpr(p.Value)
		return &tast.LiteralPattern{Pos_: p.Pos_, End_: p.End_, Value: tastVal}

	case *ast.CompositePattern:
		return a.mapVariantPattern(p, subjectType)

	case *ast.IdentifierPattern:
		// Check whether the identifier names a unit variant on the subject sum type.
		if sumType, ok := subjectType.(*types.SumType); ok {
			if variant := sumType.GetVariant(p.Name); variant != nil {
				sym := &types.Symbol{
					Kind:    types.VariantSymbol,
					Name:    variant.Name,
					Type:    sumType,
					SumType: sumType,
					Variant: variant,
				}
				return &tast.VariantPattern{
					Pos_:    p.Pos_,
					End_:    p.Pos_,
					Variant: &tast.Identifier{Pos_: p.Pos_, Value: p.Name, Type: sumType, Symbol: sym},
					Type:    sumType,
					Symbol:  sym,
				}
			}
		}
		// Bare identifier that is not a variant name — catch-all binding.
		bindingSym := &types.Symbol{
			Kind: types.VarSymbol, Name: p.Name, Type: subjectType,
		}
		a.scope.symbols[p.Name] = bindingSym
		return &tast.IdentifierPattern{Pos_: p.Pos_, Name: p.Name}
	}

	a.errorf(pat.Pos(), "unsupported pattern type: %T", pat)
	return nil
}

// =============================================================================
// Pass 2 entry: Declaration dispatch
// =============================================================================

func (a *Analyzer) buildDeclarations(program *ast.Program) []tast.Decl {
	var decls []tast.Decl
	for _, decl := range program.Decls {
		if mapped := a.mapDecl(decl); mapped != nil {
			decls = append(decls, mapped)
		}
	}
	return decls
}

// =============================================================================
// Declaration Mappers (Implementing ASTMapper[tast.Node])
// =============================================================================

func (a *Analyzer) MapProgram(p *ast.Program) tast.Node {
	// Program mapping is handled directly by buildDeclarations in this architecture
	return nil
}

func (a *Analyzer) MapTypeDecl(td *ast.TypeDecl) tast.Node {
	resolvedType := a.lookupCustomType(td.Name.Value)
	sym := &types.Symbol{Kind: types.VarSymbol, Name: td.Name.Value, Type: resolvedType}

	node := &tast.TypeDecl{
		Pos_:   td.Pos_,
		End_:   td.End_,
		Name:   &tast.Identifier{Pos_: td.Name.Pos_, Value: td.Name.Value, Type: resolvedType},
		Type:   resolvedType,
		Symbol: sym,
	}

	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapFnDecl(v *ast.FnDecl) tast.Node {
	a.currentFn = v
	defer func() { a.currentFn = nil }()

	fnSym := a.resolve(v.Name)
	fnType := fnSym.Type.(*types.FunctionType)
	a.expectedReturn = fnType.Return

	a.pushScope()
	defer a.popScope()

	tastParams := make([]*tast.Param, len(v.Params))
	for i, p := range v.Params {
		paramSym := &types.Symbol{
			Kind:      types.VarSymbol,
			Name:      p.Name,
			Type:      fnType.Params[i],
			Mutable:   p.Mut,
			ParamMode: fnType.ParamModes[i],
		}
		a.scope.symbols[p.Name] = paramSym
		tastParams[i] = &tast.Param{
			Pos_: p.Pos_, End_: p.End_, Name: p.Name, Type: fnType.Params[i], Symbol: paramSym,
		}
	}

	var tastBody *tast.BlockStmt
	if v.Body != nil {
		tastBody = a.MapBlockStmt(v.Body).(*tast.BlockStmt)
	}

	node := &tast.FnDecl{
		Pos_:       v.Pos_,
		End_:       v.End_,
		Name:       v.Name,
		Params:     tastParams,
		ReturnType: fnType.Return,
		Body:       tastBody,
		IsAsync:    v.IsAsync,
		IsExtern:   v.IsExtern,
		Symbol:     fnSym,
	}

	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

// =============================================================================
// Statement Mappers
// =============================================================================

func (a *Analyzer) MapBlockStmt(body *ast.BlockStmt) tast.Node {
	a.pushScope()
	defer a.popScope()

	var stmts []tast.Stmt
	for _, s := range body.Statements {
		if mapped := a.mapStmt(s); mapped != nil {
			stmts = append(stmts, mapped)
		}
	}

	return &tast.BlockStmt{Pos_: body.Pos_, End_: body.End_, Statements: stmts}
}

func (a *Analyzer) MapDeclareStmt(s *ast.DeclareStmt) tast.Node {
	value := a.mapExpr(s.Value)
	exprType := tast.TypeOf(value)

	sym := &types.Symbol{Kind: types.VarSymbol, Name: s.Name, Type: exprType, Mutable: s.Mutable}
	node := &tast.DeclareStmt{Pos_: s.Pos_, Symbol: sym, Value: value}

	a.drainViolations(a.registry.Check(node, a.ctx()))
	a.scope.symbols[s.Name] = sym

	return node
}

func (a *Analyzer) MapAssignStmt(s *ast.AssignStmt) tast.Node {
	lhs := a.mapExpr(s.LValue)
	rhs := a.mapExpr(s.RValue)

	node := &tast.AssignStmt{Pos_: s.Pos_, LValue: lhs, RValue: rhs, Operator: s.Operator}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapVecPushStmt(s *ast.VecPushStmt) tast.Node {
	lhs := a.mapExpr(s.LValue)
	rhs := a.mapExpr(s.RValue)

	node := &tast.VecPushStmt{Pos_: s.Pos_, LValue: lhs, RValue: rhs}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapReturnStmt(s *ast.ReturnStmt) tast.Node {
	var value tast.Expr
	if s.Value != nil {
		value = a.mapExpr(s.Value)
	}
	node := &tast.ReturnStmt{Pos_: s.Pos_, Value: value}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapYieldStmt(s *ast.YieldStmt) tast.Node {
	return &tast.YieldStmt{Pos_: s.Pos_, Value: a.mapExpr(s.Value)}
}

func (a *Analyzer) MapExprStmt(s *ast.ExprStmt) tast.Node {
	return &tast.ExprStmt{Pos_: s.Pos_, Value: a.mapExpr(s.Value)}
}

func (a *Analyzer) MapBreakStmt(s *ast.BreakStmt) tast.Node {
	return &tast.BreakStmt{Pos_: s.Pos(), End_: s.End()}
}

func (a *Analyzer) MapContinueStmt(s *ast.ContinueStmt) tast.Node {
	return &tast.ContinueStmt{Pos_: s.Pos(), End_: s.End()}
}

func (a *Analyzer) MapForStmt(s *ast.ForStmt) tast.Node {
	a.pushScope()
	defer a.popScope()

	var init tast.Stmt
	if s.Init != nil {
		init = a.mapStmt(s.Init)
	}
	var cond tast.Expr
	if s.Condition != nil {
		cond = a.mapExpr(s.Condition)
	}
	var post tast.Stmt
	if s.Post != nil {
		post = a.mapStmt(s.Post)
	}
	var body *tast.BlockStmt
	if s.Body != nil {
		body = a.MapBlockStmt(s.Body).(*tast.BlockStmt)
	}

	node := &tast.ForStmt{Pos_: s.Pos_, Init: init, Condition: cond, Post: post, Body: body}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

// =============================================================================
// Expression Mappers
// =============================================================================

func (a *Analyzer) MapIntLiteral(e *ast.IntLiteral) tast.Node {
	return &tast.IntLiteral{Pos_: e.Pos_, End_: e.End_, Value: e.Value, Type: types.IntType{}}
}

func (a *Analyzer) MapBoolLiteral(e *ast.BoolLiteral) tast.Node {
	return &tast.BoolLiteral{Pos_: e.Pos_, Value: e.Value, Type: types.BoolType{}}
}

func (a *Analyzer) MapStringLiteral(e *ast.StringLiteral) tast.Node {
	return &tast.StringLiteral{Pos_: e.Pos_, Value: e.Value, Type: types.StringType{}}
}

func (a *Analyzer) MapIdentifier(e *ast.Identifier) tast.Node {
	sym := a.resolve(e.Value)
	if sym == nil {
		a.errorf(e.Pos(), "undefined name '%s'", e.Value)
		return &tast.Identifier{Pos_: e.Pos_, Value: e.Value, Type: types.UnknownType{}}
	}

	if sym.Kind == types.VariantSymbol {
		isUnit := sym.Variant != nil && len(sym.Variant.TupleTypes) == 0 && len(sym.Variant.Fields) == 0
		if isUnit {
			sumType := sym.SumType
			if sumType.BaseName == "Option" && sym.Variant.Name == "None" {
				if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Option" {
					sumType = exp
				}
			}
			return &tast.VariantLiteral{Pos_: e.Pos_, Variant: sym.Variant, Type: sumType}
		}
	}

	return &tast.Identifier{Pos_: e.Pos_, Value: e.Value, Type: sym.Type, Symbol: sym}
}

func (a *Analyzer) MapInfixExpr(e *ast.InfixExpr) tast.Node {
	left := a.mapExpr(e.Left)
	right := a.mapExpr(e.Right)
	resultType := inferInfixType(e.Operator, tast.TypeOf(left), tast.TypeOf(right))

	node := &tast.InfixExpr{Pos_: e.Pos_, Left: left, Operator: e.Operator, Right: right, Type: resultType}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapPrefixExpr(e *ast.PrefixExpr) tast.Node {
	right := a.mapExpr(e.Right)
	node := &tast.PrefixExpr{Pos_: e.Pos_, Operator: e.Operator, Right: right, Type: inferPrefixType(e.Operator, tast.TypeOf(right))}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapIfExpr(e *ast.IfExpr) tast.Node {
	cond := a.mapExpr(e.Condition)
	cons := a.MapBlockStmt(e.Consequence).(*tast.BlockStmt)

	var alt *tast.BlockStmt
	if e.Alternative != nil {
		alt = a.MapBlockStmt(e.Alternative).(*tast.BlockStmt)
	}

	consType := tast.TypeOfBlock(cons)
	altType := types.Type(types.UnitType{})
	if alt != nil {
		altType = tast.TypeOfBlock(alt)
	}

	node := &tast.IfExpr{Pos_: e.Pos_, Condition: cond, Consequence: cons, Alternative: alt, Type: types.MergeTypes(consType, altType)}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapCallExpr(e *ast.CallExpr) tast.Node {
	// 1. Variant literal intercept
	if funcIdent, ok := e.Function.(*ast.Identifier); ok {
		if sym := a.resolve(funcIdent.Value); sym != nil && sym.Kind == types.VariantSymbol {
			return a.mapTupleVariantLiteral(e, sym)
		}
	}

	// 2. Map Function and Arguments
	fnNode := a.mapExpr(e.Function)
	var tastArgs []tast.CallArg
	for _, arg := range e.Arguments {
		tastArgs = append(tastArgs, tast.CallArg{
			Pos_:     arg.Pos_,
			Argument: a.mapExpr(arg.Argument),
			Mut:      arg.Mut,
		})
	}

	// 3. Resolve Return Type and Async State
	fnType := tast.TypeOf(fnNode)
	var returnType types.Type = types.UnknownType{}
	isAsync := false

	if ft, ok := fnType.(*types.FunctionType); ok {
		returnType = ft.Return
		if _, ok := ft.Return.(*types.FutureType); ok {
			isAsync = true
		}
	}

	// 4. Built-in intercepts
	returnType = a.handleRunExecutor(e, fnNode, tastArgs, returnType)

	// 5. The "No Bare Call" Trap (Driven by context state!)
	if isAsync && !a.allowAsyncCall {
		fnName := "async function"
		if ident, ok := fnNode.(*tast.Identifier); ok {
			fnName = "'" + ident.Value + "'"
		}
		a.errorf(e.Pos(), "%s must be called with 'await' or 'spawn'", fnName)
	}

	node := &tast.CallExpr{
		Pos_:      e.Pos_,
		End_:      e.End_,
		Function:  fnNode,
		Arguments: tastArgs,
		Type:      returnType,
	}

	// Fire rules: CalleeIsCallable, CallArgumentCount, CallArgumentTypeCompatibility
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

// handleRunExecutor extracts the T from Future<T> at compile time
func (a *Analyzer) handleRunExecutor(e *ast.CallExpr, fnNode tast.Expr, args []tast.CallArg, currentReturn types.Type) types.Type {
	if ident, ok := fnNode.(*tast.Identifier); ok && ident.Value == "run_executor" {
		if len(args) != 1 {
			a.errorf(e.Pos(), "'run_executor' expects exactly 1 argument")
		} else {
			argType := tast.TypeOf(args[0].Argument)
			if futTy, ok := argType.(*types.FutureType); ok {
				return futTy.Base
			} else if !types.IsUnknown(argType) {
				a.errorf(e.Pos(), "'run_executor' expects a Future, got '%s'", argType.String())
			}
		}
	}
	return currentReturn
}

func (a *Analyzer) MapSpawnExpr(e *ast.SpawnExpr) tast.Node {
	// Temporarily allow async calls, map the child, then reset
	a.allowAsyncCall = true
	valueNode := a.mapExpr(e.Value)
	a.allowAsyncCall = false

	tastCall, _ := valueNode.(*tast.CallExpr)
	futType := &types.FutureType{Base: types.UnknownType{}}
	if fut, ok := tast.TypeOf(tastCall).(*types.FutureType); ok {
		futType = fut
	}

	node := &tast.SpawnExpr{Pos_: e.Pos_, Value: tastCall, Type: futType}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapAwaitExpr(e *ast.AwaitExpr) tast.Node {
	// Temporarily allow async calls, map the child, then reset
	a.allowAsyncCall = true
	valueNode := a.mapExpr(e.Value)
	a.allowAsyncCall = false

	var baseType types.Type = types.UnknownType{}
	if fut, ok := tast.TypeOf(valueNode).(*types.FutureType); ok {
		baseType = fut.Base
	}

	node := &tast.AwaitExpr{Pos_: e.Pos_, Value: valueNode, Type: baseType}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapCompositeLiteral(e *ast.CompositeLiteral) tast.Node {
	if named, ok := e.TypeExpr.(*ast.NamedTypeExpr); ok {
		if sym := a.resolve(named.Name.Value); sym != nil && sym.Kind == types.VariantSymbol {
			if sym.SumType != nil {
				return a.mapStructVariantLiteral(e, sym.SumType)
			}
		}
	}

	resolvedType := a.resolveAstType(e.TypeExpr)
	switch ty := resolvedType.(type) {
	case *types.StructType:
		return a.mapStructLiteral(e, ty)
	case *types.SumType:
		return a.mapStructVariantLiteral(e, ty)
	case *types.ArrayType:
		return a.mapArrayLiteral(e, ty)
	case *types.VectorType:
		return a.mapVecLiteral(e, ty)
	case *types.MapType:
		return a.mapMapLiteral(e, ty)
	default:
		if !types.IsUnknown(resolvedType) {
			a.errorf(e.Pos(), "type '%s' does not support literal instantiation", resolvedType.String())
		}
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}
}

func (a *Analyzer) MapFieldAccess(e *ast.FieldAccess) tast.Node {
	obj := a.mapExpr(e.Object)
	objType := tast.TypeOf(obj)

	var fieldType types.Type = types.UnknownType{}
	if st, ok := objType.(*types.StructType); ok {
		if idx := st.GetFieldIndex(e.Field.Value); idx != -1 {
			fieldType = st.Fields[idx].Type
		}
	}

	node := &tast.FieldAccess{
		Pos_: e.Pos_, Object: obj, Field: &tast.Identifier{Pos_: e.Field.Pos_, Value: e.Field.Value, Type: fieldType}, Type: fieldType,
	}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapIndexExpr(e *ast.IndexExpr) tast.Node {
	left := a.mapExpr(e.Left)
	node := &tast.IndexExpr{Pos_: e.Pos_, Left: left, Index: a.mapExpr(e.Index), Type: inferIndexType(tast.TypeOf(left))}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapSliceExpr(e *ast.SliceExpr) tast.Node {
	left := a.mapExpr(e.Left)
	leftType := tast.TypeOf(left)

	var low, high tast.Expr
	if e.Low != nil {
		low = a.mapExpr(e.Low)
		if t := tast.TypeOf(low); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.Low.Pos(), "slice low index must be an integer")
		}
	}
	if e.High != nil {
		high = a.mapExpr(e.High)
		if t := tast.TypeOf(high); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.High.Pos(), "slice high index must be an integer")
		}
	}

	var resultType types.Type = types.UnknownType{}
	switch ty := leftType.(type) {
	case *types.ArrayType:
		resultType = &types.ViewType{Base: ty.Base}
	case *types.VectorType:
		resultType = &types.ViewType{Base: ty.Base}
	case *types.ViewType:
		resultType = &types.ViewType{Base: ty.Base}
	case types.StringType:
		resultType = types.StringType{}
	default:
		if !types.IsUnknown(leftType) {
			a.errorf(e.Left.Pos(), "cannot slice non-array/vector/view type '%s'", leftType.String())
		}
	}

	return &tast.SliceExpr{Pos_: e.Pos_, Left: left, Low: low, High: high, Type: resultType}
}

func (a *Analyzer) MapMatchExpr(e *ast.MatchExpr) tast.Node {
	subject := a.mapExpr(e.Subject)
	subjectType := tast.TypeOf(subject)

	var arms []tast.MatchArm
	var resultType types.Type

	for i, arm := range e.Arms {
		a.pushScope()
		tastArm := tast.MatchArm{
			Pos_:    arm.Pos_,
			End_:    arm.End_,
			Pattern: a.mapPattern(arm.Pattern, subjectType),
			Body:    a.mapExpr(arm.Body),
		}
		a.popScope()

		arms = append(arms, tastArm)

		armType := tast.TypeOf(tastArm.Body)
		if i == 0 {
			resultType = armType
		} else {
			resultType = types.MergeTypes(resultType, armType)
		}
	}

	node := &tast.MatchExpr{Pos_: e.Pos_, End_: e.End_, Subject: subject, Arms: arms, Type: resultType}
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapOwnExpr(e *ast.OwnExpr) tast.Node {
	valNode := a.mapExpr(e.Value)

	node := &tast.OwnExpr{
		Pos_:  e.Pos_,
		Value: valNode,
		Type:  tast.TypeOf(valNode),
	}

	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) MapFreezeExpr(e *ast.FreezeExpr) tast.Node {
	valNode := a.mapExpr(e.Value)

	node := &tast.FreezeExpr{
		Pos_:  e.Pos_,
		Value: valNode,
		Type:  tast.TypeOf(valNode),
	}

	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

// =============================================================================
// Internal Builders for Variants and Composites
// =============================================================================

func (a *Analyzer) mapStructLiteral(e *ast.CompositeLiteral, ty *types.StructType) tast.Expr {
	structDef := a.lookupStruct(ty.Name)
	if structDef == nil {
		a.errorf(e.TypeExpr.Pos(), "undefined struct type '%s'", ty.Name)
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}

	var tastFields []tast.StructField
	seen := make(map[string]bool)

	for _, elem := range e.Elements {
		ident, ok := elem.Key.(*ast.Identifier)
		if !ok || elem.Key == nil {
			a.errorf(elem.Value.Pos(), "struct fields must be keyed with identifiers")
			continue
		}

		fieldName := ident.Value
		if seen[fieldName] {
			a.errorf(elem.Pos_, "duplicate field '%s' in struct literal", fieldName)
			continue
		}
		seen[fieldName] = true

		valNode := a.mapExpr(elem.Value)
		valType := tast.TypeOf(valNode)

		expectedIdx := structDef.GetFieldIndex(fieldName)
		if expectedIdx != -1 {
			expectedType := structDef.Fields[expectedIdx].Type
			if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
				a.errorf(elem.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'", fieldName, expectedType.String(), valType.String())
			}
		} else {
			a.errorf(elem.Key.Pos(), "field '%s' does not exist on struct '%s'", fieldName, structDef.Name)
		}

		tastFields = append(tastFields, tast.StructField{
			Pos_:  elem.Pos_,
			End_:  elem.End_,
			Key:   &tast.Identifier{Pos_: elem.Key.Pos(), Value: fieldName, Type: types.StringType{}},
			Value: valNode,
		})
	}

	if missing := a.getMissingFields(structDef, seen); len(missing) > 0 {
		a.errorf(e.Pos(), "missing field(s) '%v' in struct literal", missing)
	}

	return &tast.StructLiteral{
		Pos_:   e.Pos_,
		End_:   e.End_,
		Fields: tastFields,
		Type:   structDef,
	}
}

func (a *Analyzer) mapStructVariantLiteral(e *ast.CompositeLiteral, ty *types.SumType) *tast.VariantLiteral {
	var variantName string
	if named, ok := e.TypeExpr.(*ast.NamedTypeExpr); ok {
		variantName = named.Name.Value
	} else {
		a.errorf(e.TypeExpr.Pos(), "invalid type signature for variant instantiation")
		return nil
	}

	variant := ty.GetVariant(variantName)
	if variant == nil {
		a.errorf(e.Pos(), "variant '%s' does not exist on type '%s'", variantName, ty.BaseName)
		return nil
	}

	var tastFields []tast.VariantField
	seen := make(map[string]bool)

	for _, elem := range e.Elements {
		ident, ok := elem.Key.(*ast.Identifier)
		if !ok || elem.Key == nil {
			a.errorf(elem.Value.Pos(), "variant fields must be keyed with identifiers (e.g., field: value)")
			continue
		}

		fieldName := ident.Value
		if seen[fieldName] {
			a.errorf(elem.Pos_, "duplicate field '%s' in variant literal", fieldName)
			continue
		}
		seen[fieldName] = true

		valNode := a.mapExpr(elem.Value)
		valType := tast.TypeOf(valNode)

		var expectedType types.Type
		for _, vf := range variant.Fields {
			if vf.Name == fieldName {
				expectedType = vf.Type
				break
			}
		}

		if expectedType == nil {
			a.errorf(elem.Key.Pos(), "field '%s' does not exist on variant '%s'", fieldName, variant.Name)
		} else if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
			a.errorf(elem.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'", fieldName, expectedType.String(), valType.String())
		}

		tastFields = append(tastFields, tast.VariantField{Name: fieldName, Value: valNode})
	}

	for _, f := range variant.Fields {
		if !seen[f.Name] {
			a.errorf(e.Pos_, "missing field '%s' in variant literal", f.Name)
		}
	}

	return &tast.VariantLiteral{
		Pos_:    e.Pos_,
		End_:    e.End_,
		Variant: variant,
		Fields:  tastFields,
		Type:    ty,
	}
}

func (a *Analyzer) mapArrayLiteral(e *ast.CompositeLiteral, ty *types.ArrayType) *tast.ArrayLiteral {
	if ty.Base == nil || types.IsUnknown(ty.Base) {
		return &tast.ArrayLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}

	if len(e.Elements) > ty.Size {
		a.errorf(e.Pos(), "too many elements in array literal: declared size is %d, got %d", ty.Size, len(e.Elements))
		return &tast.ArrayLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}

	var elems []tast.Expr
	for i, el := range e.Elements {
		if el.Key != nil {
			a.errorf(el.Key.Pos(), "array literals cannot have keyed elements")
			continue
		}
		elNode := a.mapExpr(el.Value)
		elType := tast.TypeOf(elNode)
		if !elType.Equals(ty.Base) && !types.IsUnknown(elType) {
			a.errorf(el.Value.Pos(), "array element %d type mismatch: expected '%s', got '%s'", i, ty.Base.String(), elType.String())
		}
		elems = append(elems, elNode)
	}

	return &tast.ArrayLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: elems,
		Type:     ty,
	}
}

func (a *Analyzer) mapTupleVariantLiteral(e *ast.CallExpr, sym *types.Symbol) *tast.VariantLiteral {
	variant := sym.Variant
	sumType := sym.SumType

	switch sumType.BaseName {
	case "Option":
		if variant.Name == "Some" && len(e.Arguments) == 1 {
			valNode := a.mapExpr(e.Arguments[0].Argument)
			sumType = types.NewOptionType(tast.TypeOf(valNode))
			variant = sumType.GetVariant("Some")
		} else if variant.Name == "None" {
			if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Option" {
				sumType = exp
			}
			variant = sumType.GetVariant("None")
		}
	case "Result":
		var vType, eType types.Type = types.UnknownType{}, types.UnknownType{}
		if variant.Name == "Ok" && len(e.Arguments) == 1 {
			vType = tast.TypeOf(a.mapExpr(e.Arguments[0].Argument))
		} else if variant.Name == "Err" && len(e.Arguments) == 1 {
			eType = tast.TypeOf(a.mapExpr(e.Arguments[0].Argument))
		}
		if types.IsUnknown(vType) || types.IsUnknown(eType) {
			if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Result" && len(exp.TypeArgs) == 2 {
				if types.IsUnknown(vType) {
					vType = exp.TypeArgs[0]
				}
				if types.IsUnknown(eType) {
					eType = exp.TypeArgs[1]
				}
			}
		}
		sumType = types.NewResultType(vType, eType)
		variant = sumType.GetVariant(sym.Variant.Name)
	}

	if len(e.Arguments) != len(variant.TupleTypes) {
		a.errorf(e.Pos(), "variant '%s' expects %d argument(s), got %d", variant.Name, len(variant.TupleTypes), len(e.Arguments))
	}

	var tastArgs []tast.Expr
	for i, arg := range e.Arguments {
		valNode := a.mapExpr(arg.Argument)
		if i < len(variant.TupleTypes) {
			if !tast.TypeOf(valNode).Equals(variant.TupleTypes[i]) && !types.IsUnknown(tast.TypeOf(valNode)) {
				a.errorf(arg.Pos_, "type mismatch for variant argument %d", i+1)
			}
		}
		tastArgs = append(tastArgs, valNode)
	}

	return &tast.VariantLiteral{
		Pos_:      e.Pos_,
		End_:      e.End(),
		Variant:   variant,
		Arguments: tastArgs,
		Type:      sumType,
	}
}

func (a *Analyzer) mapVariantPattern(p *ast.CompositePattern, subjectType types.Type) *tast.VariantPattern {
	namedType, ok := p.TypeExpr.(*ast.NamedTypeExpr)
	if !ok {
		a.errorf(p.Pos_, "invalid type in composite pattern")
		return nil
	}
	variantName := namedType.Name.Value
	variantIdent := &tast.Identifier{Pos_: p.Pos_, Value: variantName}

	sumType, ok := subjectType.(*types.SumType)
	if !ok {
		if !types.IsUnknown(subjectType) {
			a.errorf(p.Pos_, "cannot match variant pattern on non-sum type '%s'", subjectType.String())
		}
		variantIdent.Type = types.UnknownType{}
		return &tast.VariantPattern{Pos_: p.Pos_, End_: p.End_, Variant: variantIdent, Type: types.UnknownType{}}
	}

	variant := sumType.GetVariant(variantName)
	if variant == nil {
		a.errorf(p.Pos_, "variant '%s' does not exist on type '%s'", variantName, sumType.BaseName)
		variantIdent.Type = sumType
		return &tast.VariantPattern{Pos_: p.Pos_, End_: p.End_, Variant: variantIdent, Type: sumType}
	}

	sym := &types.Symbol{
		Kind: types.VariantSymbol, Name: variant.Name,
		Type: sumType, SumType: sumType, Variant: variant,
	}
	variantIdent.Symbol = sym
	variantIdent.Type = sumType

	var tupleBindings []*tast.Identifier
	var fields []tast.VariantPatternField
	tupleIdx := 0

	for _, elem := range p.Elements {
		if elem.Key == nil {
			// Positional / tuple binding
			var fieldType types.Type = types.UnknownType{}
			if tupleIdx < len(variant.TupleTypes) {
				fieldType = variant.TupleTypes[tupleIdx]
			} else {
				a.errorf(elem.Pos_, "extra positional argument in tuple variant '%s'", variantName)
			}

			if identPat, isIdent := elem.Pattern.(*ast.IdentifierPattern); isIdent {
				if _, exists := a.scope.symbols[identPat.Name]; exists {
					a.errorf(elem.Pos_, "variable '%s' is already bound in this pattern", identPat.Name)
				}
				bindingSym := &types.Symbol{
					Kind: types.VarSymbol, Name: identPat.Name, Type: fieldType,
				}
				a.scope.symbols[identPat.Name] = bindingSym
				tupleBindings = append(tupleBindings, &tast.Identifier{
					Pos_: elem.Pos_, Value: identPat.Name, Type: fieldType, Symbol: bindingSym,
				})
			} else {
				a.errorf(elem.Pos_, "nested complex sub-patterns are currently unsupported inside tuple variants")
			}
			tupleIdx++
		} else {
			// Named field binding
			keyIdent, ok := elem.Key.(*ast.Identifier)
			if !ok {
				a.errorf(elem.Pos_, "struct pattern key must be a valid identifier")
				continue
			}
			fieldName := keyIdent.Value

			var fieldType types.Type = types.UnknownType{}
			for _, vf := range variant.Fields {
				if vf.Name == fieldName {
					fieldType = vf.Type
					break
				}
			}
			if types.IsUnknown(fieldType) {
				a.errorf(elem.Pos_, "field '%s' does not exist on variant '%s'", fieldName, variant.Name)
			}

			var binding *tast.Identifier
			if identPat, isIdent := elem.Pattern.(*ast.IdentifierPattern); isIdent {
				if _, exists := a.scope.symbols[identPat.Name]; exists {
					a.errorf(elem.Pos_, "variable '%s' is already bound in this pattern", identPat.Name)
				}
				bindingSym := &types.Symbol{
					Kind: types.VarSymbol, Name: identPat.Name, Type: fieldType,
				}
				a.scope.symbols[identPat.Name] = bindingSym
				binding = &tast.Identifier{
					Pos_: elem.Pos_, Value: identPat.Name, Type: fieldType, Symbol: bindingSym,
				}
			} else {
				a.errorf(elem.Pos_, "nested complex sub-patterns are currently unsupported inside struct fields")
			}
			fields = append(fields, tast.VariantPatternField{Field: fieldName, Binding: binding})
		}
	}

	if len(variant.TupleTypes) > 0 && tupleIdx != len(variant.TupleTypes) {
		a.errorf(p.Pos_,
			"wrong number of bindings for tuple variant '%s': expected %d, got %d",
			variantName, len(variant.TupleTypes), tupleIdx)
	}

	return &tast.VariantPattern{
		Pos_:          p.Pos_,
		End_:          p.End_,
		Variant:       variantIdent,
		TupleBindings: tupleBindings,
		Fields:        fields,
		Type:          sumType,
		Symbol:        sym,
	}
}

// =============================================================================
// Pure Type Inference Helpers
// =============================================================================

func inferInfixType(op string, left, right types.Type) types.Type {
	if types.IsUnknown(left) || types.IsUnknown(right) {
		return types.UnknownType{}
	}
	if !left.Equals(right) {
		return types.UnknownType{}
	}
	switch op {
	case "+", "-", "*", "/", "%":
		if left.Equals(types.IntType{}) {
			return types.IntType{}
		}
	case "==", "!=", "<", "<=", ">", ">=":
		return types.BoolType{}
	case "&&", "||":
		if left.Equals(types.BoolType{}) {
			return types.BoolType{}
		}
	}
	return types.UnknownType{}
}

func inferPrefixType(op string, right types.Type) types.Type {
	if types.IsUnknown(right) {
		return types.UnknownType{}
	}
	switch op {
	case "!":
		if right.Equals(types.BoolType{}) {
			return types.BoolType{}
		}
	case "-":
		if right.Equals(types.IntType{}) {
			return types.IntType{}
		}
	}
	return types.UnknownType{}
}

func inferIndexType(leftType types.Type) types.Type {
	switch ty := leftType.(type) {
	case *types.ArrayType:
		return ty.Base
	case *types.ViewType:
		return ty.Base
	case *types.VectorType:
		return ty.Base
	case *types.MapType:
		return types.NewOptionType(ty.Value) // map reads return Option<V>
	case types.StringType:
		return types.IntType{} // characters are returned as int
	}
	return types.UnknownType{}
}

// =============================================================================
// Interface Satisfiers for Non-Dispatching Node Types
// =============================================================================

// These types are structurally required by ASTMapper[tast.Node] but are either
// handled directly in custom mapping loops (like Pattern) or bypass normal expression evaluation.
func (a *Analyzer) MapNamedTypeExpr(n *ast.NamedTypeExpr) tast.Node     { return nil }
func (a *Analyzer) MapArrayTypeExpr(n *ast.ArrayTypeExpr) tast.Node     { return nil }
func (a *Analyzer) MapStructTypeExpr(n *ast.StructTypeExpr) tast.Node   { return nil }
func (a *Analyzer) MapSumTypeExpr(n *ast.SumTypeExpr) tast.Node         { return nil }
func (a *Analyzer) MapGenericTypeExpr(n *ast.GenericTypeExpr) tast.Node { return nil }
func (a *Analyzer) MapTypeExprWrapper(n *ast.TypeExprWrapper) tast.Node { return nil }

func (a *Analyzer) MapWildcardPattern(n *ast.WildcardPattern) tast.Node     { return nil }
func (a *Analyzer) MapIdentifierPattern(n *ast.IdentifierPattern) tast.Node { return nil }
func (a *Analyzer) MapLiteralPattern(n *ast.LiteralPattern) tast.Node       { return nil }
func (a *Analyzer) MapCompositePattern(n *ast.CompositePattern) tast.Node   { return nil }
