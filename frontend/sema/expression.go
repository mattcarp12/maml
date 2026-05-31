package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// The Master Expression Dispatcher
// =============================================================================

func (a *Analyzer) buildExpr(expr ast.Expr) tast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.IntLiteral:
		return &tast.IntLiteral{Pos_: e.Pos_, End_: e.End_, Value: e.Value, Type: types.IntType{}}
	case *ast.BoolLiteral:
		return &tast.BoolLiteral{Pos_: e.Pos_, Value: e.Value, Type: types.BoolType{}}
	case *ast.StringLiteral:
		return &tast.StringLiteral{Pos_: e.Pos_, Value: e.Value, Type: types.StringType{}}
	case *ast.Identifier:
		return a.buildIdentifier(e)
	case *ast.InfixExpr:
		return a.buildInfixExpr(e)
	case *ast.PrefixExpr:
		return a.buildPrefixExpr(e)
	case *ast.BlockStmt:
		return a.buildBlockStmt(e)
	case *ast.IfExpr:
		return a.buildIfExpr(e)
	case *ast.CallExpr:
		return a.buildCallExpr(e)
	case *ast.MethodCallExpr:
		return a.buildMethodCallExpr(e)
	case *ast.StructLiteral:
		return a.buildStructLiteral(e)
	case *ast.FieldAccess:
		return a.buildFieldAccess(e)
	case *ast.ArrayLiteral:
		return a.buildArrayLiteral(e)
	case *ast.IndexExpr:
		return a.buildIndexExpr(e)
	case *ast.SliceExpr:
		return a.buildSliceExpr(e)
	case *ast.AwaitExpr:
		return a.buildAwaitExpr(e)
	case *ast.MatchExpr:
		return a.buildMatchExpr(e)
	}

	a.errorf(expr.Pos(), "unsupported expression type during TAST construction: %T", expr)
	return nil
}

// =============================================================================
// Primitive Builders
// =============================================================================

func (a *Analyzer) buildIdentifier(e *ast.Identifier) tast.Expr {
	sym := a.resolve(e.Value)
	if sym == nil {
		a.errorf(e.Pos(), "undefined name '%s'", e.Value)
		return &tast.Identifier{Pos_: e.Pos_, Value: e.Value, Type: types.UnknownType{}}
	}
	if sym.Kind == types.VariantSymbol {
		isUnit := sym.Variant != nil &&
			len(sym.Variant.TupleTypes) == 0 &&
			len(sym.Variant.Fields) == 0

		if isUnit {
			sumType := sym.SumType

			// Generic Inference for Unit Variants (e.g., bare `None`)
			if sumType.BaseName == "Option" && sym.Variant.Name == "None" {
				if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Option" {
					sumType = exp
				}
			}

			return &tast.StructLiteral{
				Pos_: e.Pos_,
				Type: sumType,
				Fields: []tast.StructField{
					{
						Key:   &tast.Identifier{Pos_: e.Pos_, Value: "__discriminant", Type: types.StringType{}},
						Value: &tast.IntLiteral{Pos_: e.Pos_, Value: int64(sym.Variant.Discriminant), Type: types.IntType{}},
					},
				},
			}
		}
	}

	return &tast.Identifier{
		Pos_:   e.Pos_,
		Value:  e.Value,
		Type:   sym.Type,
		Symbol: sym,
	}
}

func (a *Analyzer) buildInfixExpr(e *ast.InfixExpr) *tast.InfixExpr {
	leftNode := a.buildExpr(e.Left)
	rightNode := a.buildExpr(e.Right)

	leftType := typeOf(leftNode)
	rightType := typeOf(rightNode)

	// Math and Logic Rules
	var resultType types.Type = types.UnknownType{}
	if leftType.Equals(rightType) {
		switch e.Operator {
		case "+", "-", "*", "/", "%":
			if leftType.Equals(types.IntType{}) {
				resultType = types.IntType{}
			} else {
				a.errorf(e.Pos(), "type mismatch: cannot %s '%s' and '%s'", e.Operator, leftType.String(), rightType.String())
			}
		case "==", "!=", "<", "<=", ">", ">=":
			resultType = types.BoolType{}
		case "&&", "||":
			if leftType.Equals(types.BoolType{}) {
				resultType = types.BoolType{}
			} else {
				a.errorf(e.Pos(), "type mismatch: cannot %s non-booleans", e.Operator)
			}
		}
	} else if !types.IsUnknown(leftType) && !types.IsUnknown(rightType) {
		a.errorf(e.Pos(), "type mismatch: cannot %s '%s' and '%s'", e.Operator, leftType.String(), rightType.String())
	}

	return &tast.InfixExpr{
		Pos_:     e.Pos_,
		Left:     leftNode,
		Operator: e.Operator,
		Right:    rightNode,
		Type:     resultType,
	}
}

func (a *Analyzer) buildPrefixExpr(e *ast.PrefixExpr) *tast.PrefixExpr {
	rightNode := a.buildExpr(e.Right)
	rightType := typeOf(rightNode)

	var resultType types.Type = types.UnknownType{}
	switch e.Operator {
	case "!":
		if rightType.Equals(types.BoolType{}) {
			resultType = types.BoolType{}
		} else if !types.IsUnknown(rightType) {
			a.errorf(e.Pos(), "operator '!' expects 'bool', got '%s'", rightType.String())
		}
	case "-":
		if rightType.Equals(types.IntType{}) {
			resultType = types.IntType{}
		} else if !types.IsUnknown(rightType) {
			a.errorf(e.Pos(), "operator '-' expects 'int', got '%s'", rightType.String())
		}
	}

	return &tast.PrefixExpr{
		Pos_:     e.Pos_,
		Operator: e.Operator,
		Right:    rightNode,
		Type:     resultType,
	}
}

// =============================================================================
// Control Flow Expressions
// =============================================================================

func (a *Analyzer) buildIfExpr(e *ast.IfExpr) *tast.IfExpr {
	condNode := a.buildExpr(e.Condition)
	condType := typeOf(condNode)

	if !condType.Equals(types.BoolType{}) && !types.IsUnknown(condType) {
		a.errorf(e.Pos(), "IF condition must be a boolean")
	}

	consBlock := a.buildBlockStmt(e.Consequence)
	consYield := TypeOfBlock(consBlock)

	var altBlock *tast.BlockStmt
	altYield := types.Type(types.UnitType{})

	if e.Alternative != nil {
		altBlock = a.buildBlockStmt(e.Alternative)
		altYield = TypeOfBlock(altBlock)
	}

	resultType := mergeTypes(consYield, altYield)

	return &tast.IfExpr{
		Pos_:        e.Pos_,
		Condition:   condNode,
		Consequence: consBlock,
		Alternative: altBlock,
		Type:        resultType,
	}
}

func (a *Analyzer) buildCallExpr(e *ast.CallExpr) tast.Expr {
	// 1. EARLY DISPATCH: Inspect the raw AST identifier BEFORE calling buildExpr.
	// This must happen at the AST level so it works regardless of what buildIdentifier
	// returns — a *tast.Identifier for tuple variants OR a *tast.StructLiteral for
	// unit variants like None. The old post-build check missed the unit-variant case
	// because buildIdentifier("None") returns StructLiteral, not Identifier.
	if funcIdent, ok := e.Function.(*ast.Identifier); ok {
		if sym := a.resolve(funcIdent.Value); sym != nil && sym.Kind == types.VariantSymbol {
			return a.buildTupleVariantLiteral(e, sym)
		}
	}

	// 2. Resolve the expression being called (e.g., 'divide')
	funcNode := a.buildExpr(e.Function)

	// 3. STANDARD FUNCTION CALL HANDLING
	// Only proceed here if it is NOT a variant.
	ft, ok := typeOf(funcNode).(*types.FunctionType)
	if !ok {
		a.errorf(e.Pos(), "cannot call non-function type '%s'", typeOf(funcNode).String())
		return &tast.CallExpr{
			Pos_:      e.Pos_,
			Function:  funcNode,
			Arguments: nil,
			Type:      types.UnknownType{},
		}
	}

	// Build and validate arguments for a standard function
	if len(e.Arguments) != len(ft.Params) {
		a.errorf(e.Pos(), "wrong number of arguments: expected %d, got %d", len(ft.Params), len(e.Arguments))
	}

	var tastArgs []tast.CallArg
	for i, arg := range e.Arguments {
		valNode := a.buildExpr(arg.Argument)
		tastArgs = append(tastArgs, tast.CallArg{
			Pos_:     arg.Pos_,
			Argument: valNode,
			Mut:      arg.Mut,
			Own:      arg.Own,
		})

		// Validate parameter type and mode if within bounds
		if i < len(ft.Params) {
			expected := ft.Params[i]
			got := typeOf(valNode)
			if !got.Equals(expected) && !types.IsUnknown(got) && !types.IsUnknown(expected) {
				a.errorf(arg.Pos_, "argument %d type mismatch: expected '%s', got '%s'", i+1, expected.String(), got.String())
			}

			// Validate ownership/mutability modes
			switch ft.ParamModes[i] {
			case types.ParamMutBorrow:
				if !arg.Mut {
					a.errorf(arg.Pos_, "argument %d requires 'mut' modifier", i+1)
				}
				if arg.Own {
					a.errorf(arg.Pos_, "argument %d expects 'mut', got 'own'", i+1)
				}
			case types.ParamOwned:
				if !arg.Own {
					a.errorf(arg.Pos_, "argument %d requires 'own' modifier", i+1)
				}
				if arg.Mut {
					a.errorf(arg.Pos_, "argument %d expects 'own', got 'mut'", i+1)
				}
			case types.ParamBorrow:
				if arg.Mut || arg.Own {
					a.errorf(arg.Pos_, "argument %d does not take ownership modifiers", i+1)
				}
			}
		}
	}

	return &tast.CallExpr{
		Pos_:      e.Pos_,
		Function:  funcNode,
		Arguments: tastArgs,
		Type:      ft.Return,
	}
}

func (a *Analyzer) buildMethodCallExpr(e *ast.MethodCallExpr) *tast.MethodCallExpr {
	objNode := a.buildExpr(e.Object)
	objType := typeOf(objNode)
	methodName := e.Method.Value

	var resultType types.Type = types.UnknownType{}
	var expectedParams []types.Type
	mutating := false

	// 1. Resolve Method Signature based on the Object's Type
	switch ty := objType.(type) {
	case types.MapType:
		switch methodName {
		case "put":
			expectedParams = []types.Type{ty.Key, ty.Value}
			resultType = types.UnitType{}
			mutating = true
		case "get":
			expectedParams = []types.Type{ty.Key}
			resultType = ty.Value // Returns V. (If you add Option later, this would return Option<V>)
		case "remove":
			expectedParams = []types.Type{ty.Key}
			resultType = types.UnitType{}
			mutating = true
		default:
			a.errorf(e.Method.Pos(), "method '%s' does not exist on type '%s'", methodName, objType.String())
		}
	case types.VectorType:
		switch methodName {
		case "push":
			expectedParams = []types.Type{ty.Base}
			resultType = types.UnitType{}
			mutating = true
		case "pop":
			expectedParams = []types.Type{}
			resultType = ty.Base
			mutating = true
		case "len":
			expectedParams = []types.Type{}
			resultType = types.IntType{}
		default:
			a.errorf(e.Method.Pos(), "method '%s' does not exist on type '%s'", methodName, objType.String())
		}
	default:
		if !types.IsUnknown(objType) {
			a.errorf(e.Method.Pos(), "type '%s' has no methods", objType.String())
		}
	}

	// 2. Enforce Mutability limits!
	if mutating {
		rootSym := a.getRootSymbol(objNode)
		if rootSym != nil && !rootSym.Mutable {
			a.errorf(e.Method.Pos(), "cannot call mutating method '%s' on immutable variable '%s'", methodName, rootSym.Name)
		}
	}

	// 3. Arity Check
	if expectedParams != nil && len(e.Arguments) != len(expectedParams) {
		a.errorf(e.Pos(), "wrong number of arguments for method '%s': expected %d, got %d",
			methodName, len(expectedParams), len(e.Arguments))
	}

	// 4. Type Check Arguments
	var tastArgs []tast.CallArg
	for i, arg := range e.Arguments {
		argNode := a.buildExpr(arg.Argument)
		argType := typeOf(argNode)

		if expectedParams != nil && i < len(expectedParams) {
			if !argType.Equals(expectedParams[i]) && !types.IsUnknown(argType) {
				a.errorf(arg.Pos_, "argument %d type mismatch: expected '%s', got '%s'",
					i+1, expectedParams[i].String(), argType.String())
			}
		}

		tastArgs = append(tastArgs, tast.CallArg{
			Pos_:     arg.Pos_,
			Argument: argNode,
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}

	// After:
	methodSym := a.buildMethodSymbol(methodName, objType, resultType, expectedParams, mutating)
	return &tast.MethodCallExpr{
		Pos_:   e.Pos_,
		Object: objNode,
		Method: &tast.Identifier{
			Pos_:   e.Method.Pos_,
			Value:  methodSym.Name, // mangled name for codegen, e.g. "maml_vec_push"
			Type:   methodSym.Type,
			Symbol: methodSym,
		},
		Arguments: tastArgs,
		Type:      resultType,
	}
}

func (a *Analyzer) buildAwaitExpr(e *ast.AwaitExpr) *tast.AwaitExpr {
	if a.currentFn == nil || !a.currentFn.IsAsync {
		a.errorf(e.Pos(), "cannot use 'await' outside of an async function")
	}

	valNode := a.buildExpr(e.Value)
	valType := typeOf(valNode)

	var resultType types.Type = types.UnknownType{}
	if taskTy, ok := valType.(types.TaskType); ok {
		resultType = taskTy.Base
	} else if !types.IsUnknown(valType) {
		a.errorf(e.Pos(), "cannot await non-task type '%s'", valType.String())
	}

	return &tast.AwaitExpr{
		Pos_:  e.Pos_,
		Value: valNode,
		Type:  resultType,
	}
}

// =============================================================================
// Container Expressions
// =============================================================================

func (a *Analyzer) buildArrayLiteral(e *ast.ArrayLiteral) *tast.ArrayLiteral {
	if len(e.Elements) == 0 {
		a.errorf(e.Pos(), "cannot infer type of empty array literal")
		return &tast.ArrayLiteral{Pos_: e.Pos_, Elements: nil, Type: types.UnknownType{}}
	}

	var elems []tast.Expr
	var baseType types.Type

	for i, el := range e.Elements {
		elNode := a.buildExpr(el)
		elType := typeOf(elNode)
		elems = append(elems, elNode)

		if i == 0 {
			baseType = elType
		} else if !elType.Equals(baseType) && !types.IsUnknown(elType) {
			a.errorf(el.Pos(), "array element %d type mismatch: expected '%s', got '%s'", i, baseType.String(), elType.String())
		}
	}

	return &tast.ArrayLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: elems,
		Type:     types.ArrayType{Base: baseType, Size: len(elems)},
	}
}

func (a *Analyzer) buildIndexExpr(e *ast.IndexExpr) *tast.IndexExpr {
	leftNode := a.buildExpr(e.Left)
	idxNode := a.buildExpr(e.Index)

	leftType := typeOf(leftNode)
	idxType := typeOf(idxNode)

	if !idxType.Equals(types.IntType{}) && !types.IsUnknown(idxType) {
		a.errorf(e.Index.Pos(), "index must be an integer, got '%s'", idxType.String())
	}

	var resultType types.Type = types.UnknownType{}
	switch ty := leftType.(type) {
	case types.ArrayType:
		resultType = ty.Base
	case types.SliceType:
		resultType = ty.Base
	case types.VectorType:
		resultType = ty.Base
	case types.StringType:
		resultType = types.IntType{} // Characters are currently ints
	default:
		if !types.IsUnknown(leftType) {
			a.errorf(e.Left.Pos(), "cannot index non-array/slice type '%s'", leftType.String())
		}
	}

	return &tast.IndexExpr{
		Pos_:  e.Pos_,
		Left:  leftNode,
		Index: idxNode,
		Type:  resultType,
	}
}

func (a *Analyzer) buildSliceExpr(e *ast.SliceExpr) *tast.SliceExpr {
	leftNode := a.buildExpr(e.Left)
	leftType := typeOf(leftNode)

	var lowNode, highNode tast.Expr

	if e.Low != nil {
		lowNode = a.buildExpr(e.Low)
		if t := typeOf(lowNode); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.Low.Pos(), "slice low index must be an integer")
		}
	}
	if e.High != nil {
		highNode = a.buildExpr(e.High)
		if t := typeOf(highNode); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.High.Pos(), "slice high index must be an integer")
		}
	}

	var resultType types.Type = types.UnknownType{}
	switch ty := leftType.(type) {
	case types.ArrayType:
		resultType = types.SliceType{Base: ty.Base}
	case types.SliceType, types.VectorType:
		resultType = leftType
	case types.StringType:
		resultType = types.StringType{}
	default:
		if !types.IsUnknown(leftType) {
			a.errorf(e.Left.Pos(), "cannot slice non-array/slice type '%s'", leftType.String())
		}
	}

	return &tast.SliceExpr{
		Pos_: e.Pos_,
		Left: leftNode,
		Low:  lowNode,
		High: highNode,
		Type: resultType,
	}
}

// =============================================================================
// Structs & Variants
// =============================================================================

func (a *Analyzer) buildStructLiteral(e *ast.StructLiteral) tast.Expr {
	switch typeNode := e.Type.(type) {

	// CASE 1: Standard Structs and Variants (e.g., User{name: "Alice"})
	case *ast.Identifier:
		name := typeNode.Value
		sym := a.resolve(name)
		if sym != nil && sym.Kind == types.VariantSymbol {
			return a.buildStructVariantLiteral(e, sym)
		}

		structDef := a.lookupStruct(name)
		if structDef == nil {
			a.errorf(e.Type.Pos(), "undefined struct type '%s'", name)
			return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
		}

		var tastFields []tast.StructField
		seen := make(map[string]bool)

		for _, field := range e.Fields {
			ident, ok := field.Key.(*ast.Identifier)
			if !ok || field.Key == nil {
				a.errorf(field.Value.Pos(), "struct fields must be keyed with identifiers")
				continue
			}

			fieldName := ident.Value
			if seen[fieldName] {
				a.errorf(field.Pos_, "duplicate field '%s' in struct literal", fieldName)
				continue
			}
			seen[fieldName] = true

			valNode := a.buildExpr(field.Value)
			valType := typeOf(valNode)

			expectedIdx := structDef.GetFieldIndex(fieldName)
			if expectedIdx != -1 {
				expectedType := structDef.Fields[expectedIdx].Type
				if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
					a.errorf(field.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'", fieldName, expectedType.String(), valType.String())
				}
			} else {
				a.errorf(field.Key.Pos(), "field '%s' does not exist on struct '%s'", fieldName, structDef.Name)
			}

			tastFields = append(tastFields, tast.StructField{
				Pos_:  field.Pos_,
				End_:  field.End_,
				Key:   &tast.Identifier{Pos_: field.Key.Pos(), Value: fieldName, Type: types.StringType{}},
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

	// CASE 2: Generic Type Expressions (e.g., Map<K,V>{...}, Vec<T>{...})
	case *ast.GenericTypeExpr:
		resolvedType := a.resolveBuiltinGeneric(typeNode)

		switch ty := resolvedType.(type) {
		case types.MapType:
			return a.buildMapLiteral(e, ty)
		case types.VectorType:
			return a.buildVecLiteral(e, ty)
		default:
			if !types.IsUnknown(resolvedType) {
				a.errorf(typeNode.Pos(), "type '%s' does not support literal instantiation", resolvedType.String())
			}
			return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
		}

	// DEFAULT: Invalid left-hand side
	default:
		a.errorf(e.Type.Pos(), "invalid type for literal instantiation")
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}
}

func (a *Analyzer) buildStructVariantLiteral(e *ast.StructLiteral, sym *types.Symbol) *tast.StructLiteral {
	variant := sym.Variant
	sumType := sym.SumType

	var tastFields []tast.StructField
	seen := make(map[string]bool)

	for _, field := range e.Fields {
		// 1. Variant fields MUST be keyed with identifiers
		ident, ok := field.Key.(*ast.Identifier)
		if !ok || field.Key == nil {
			a.errorf(field.Value.Pos(), "variant fields must be keyed with identifiers (e.g., field: value)")
			continue
		}

		fieldName := ident.Value

		// 2. Prevent duplicate fields
		if seen[fieldName] {
			a.errorf(field.Pos_, "duplicate field '%s' in variant literal", fieldName)
			continue
		}
		seen[fieldName] = true

		valNode := a.buildExpr(field.Value)
		valType := typeOf(valNode)

		// 3. Find the expected type from the variant's definition
		var expectedType types.Type
		for _, vField := range variant.Fields {
			if vField.Name == fieldName {
				expectedType = vField.Type
				break
			}
		}

		if expectedType == nil {
			a.errorf(field.Key.Pos(), "field '%s' does not exist on variant '%s'", fieldName, variant.Name)
		} else if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
			a.errorf(field.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'", fieldName, expectedType.String(), valType.String())
		}

		tastFields = append(tastFields, tast.StructField{
			Pos_:  field.Pos_,
			End_:  field.End_,
			Key:   &tast.Identifier{Pos_: field.Key.Pos(), Value: fieldName, Type: types.StringType{}},
			Value: valNode,
		})
	}

	// 4. Check for missing required fields
	for _, f := range variant.Fields {
		if !seen[f.Name] {
			a.errorf(e.Pos_, "missing field '%s' in variant literal", f.Name)
		}
	}

	return &tast.StructLiteral{
		Pos_:   e.Pos_,
		End_:   e.End_,
		Fields: tastFields,
		Type:   sumType,
	}
}

func (a *Analyzer) buildTupleVariantLiteral(e *ast.CallExpr, sym *types.Symbol) *tast.VariantLiteral {
	variant := sym.Variant
	sumType := sym.SumType

	// --- STEP 1: GENERIC TYPE INFERENCE ---
	// Update the sumType based on the arguments provided
	switch sumType.BaseName {
	case "Option":
		if variant.Name == "Some" && len(e.Arguments) == 1 {
			valNode := a.buildExpr(e.Arguments[0].Argument)
			// Unify: Option<T> where T = typeOf(valNode)
			sumType = types.NewOptionType(typeOf(valNode))
			variant = sumType.GetVariant("Some")
		} else if variant.Name == "None" {
			// None carries no arguments, so infer T from the surrounding expected return type.
			// e.g. in a function returning Option<int>, `None` or `None()` resolves to Option<int>.
			if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Option" {
				sumType = exp
			}
			variant = sumType.GetVariant("None")
		}
	case "Result":
		// Try to infer from arguments first
		var vType, eType types.Type = types.UnknownType{}, types.UnknownType{}

		if variant.Name == "Ok" && len(e.Arguments) == 1 {
			vType = typeOf(a.buildExpr(e.Arguments[0].Argument))
		} else if variant.Name == "Err" && len(e.Arguments) == 1 {
			eType = typeOf(a.buildExpr(e.Arguments[0].Argument))
		}

		// If we still have unknowns, try to infer from expected return type
		if types.IsUnknown(vType) || types.IsUnknown(eType) {
			if exp, ok := a.expectedReturn.(*types.SumType); ok && exp.BaseName == "Result" {
				if len(exp.TypeArgs) == 2 {
					if types.IsUnknown(vType) {
						vType = exp.TypeArgs[0]
					}
					if types.IsUnknown(eType) {
						eType = exp.TypeArgs[1]
					}
				}
			}
		}

		sumType = types.NewResultType(vType, eType)
		variant = sumType.GetVariant(variant.Name)
	}

	// --- STEP 2: BUILD AND VALIDATE ARGUMENTS ---
	if len(e.Arguments) != len(variant.TupleTypes) {
		a.errorf(e.Pos(), "variant '%s' expects %d arguments, got %d", variant.Name, len(variant.TupleTypes), len(e.Arguments))
	}

	var tastArgs []tast.Expr
	for i, arg := range e.Arguments {
		valNode := a.buildExpr(arg.Argument)
		// Basic type validation against inferred tuple types
		if i < len(variant.TupleTypes) {
			if !typeOf(valNode).Equals(variant.TupleTypes[i]) {
				a.errorf(arg.Pos_, "type mismatch for variant arg %d", i)
			}
		}
		tastArgs = append(tastArgs, valNode)
	}

	return &tast.VariantLiteral{
		Pos_:      e.Pos_,
		End_:      e.End(),
		Variant:   variant,
		Arguments: tastArgs,
		Type:      sumType, // This holds the concrete Option<int> or Result<V,E>
	}
}

func (a *Analyzer) buildFieldAccess(e *ast.FieldAccess) *tast.FieldAccess {
	objNode := a.buildExpr(e.Object)
	objType := typeOf(objNode)

	var resultType types.Type = types.UnknownType{}

	if st, ok := objType.(*types.StructType); ok {
		idx := st.GetFieldIndex(e.Field.Value)
		if idx != -1 {
			resultType = st.Fields[idx].Type
		} else {
			a.errorf(e.Field.Pos(), "field '%s' does not exist on struct '%s'", e.Field.Value, st.Name)
		}
	} else if !types.IsUnknown(objType) {
		a.errorf(e.Pos(), "cannot access field '%s' on non-struct type '%s'", e.Field.Value, objType.String())
	}

	return &tast.FieldAccess{
		Pos_:   e.Pos_,
		Object: objNode,
		Field:  &tast.Identifier{Pos_: e.Field.Pos_, Value: e.Field.Value, Type: resultType},
		Type:   resultType,
	}
}

// buildMethodSymbol constructs a synthetic symbol for a built-in method call.
// It is called instead of buildIdentifier because built-in method names ("push",
// "get", etc.) are not in the symbol table — they are structural properties of
// their receiver type, not named declarations.
//
// The mangled Name is the runtime function the backend will call. It must match
// the corresponding declaration in declareRuntimeFunctions on the C++ side.
func (a *Analyzer) buildMethodSymbol(
	methodName string,
	receiverType types.Type,
	returnType types.Type,
	paramTypes []types.Type,
	mutating bool,
) *types.Symbol {
	mangledName := mangleMethodName(methodName, receiverType)

	paramModes := make([]types.ParamMode, len(paramTypes))
	for i := range paramModes {
		paramModes[i] = types.ParamBorrow
	}

	return &types.Symbol{
		Kind: types.FuncSymbol,
		Name: mangledName,
		Type: &types.FunctionType{
			Params:     paramTypes,
			ParamModes: paramModes,
			Return:     returnType,
		},
		Mutable: mutating,
	}
}

// mangleMethodName produces the runtime function name for a built-in method.
// These names must exactly match the declarations in declareRuntimeFunctions.
func mangleMethodName(method string, receiver types.Type) string {
	switch receiver.(type) {
	case types.VectorType:
		switch method {
		case "push":
			return "maml_vec_push"
		case "pop":
			return "maml_vec_pop"
		case "len":
			return "maml_vec_len"
		}
	case types.MapType:
		switch method {
		case "put":
			return "maml_map_put"
		case "get":
			return "maml_map_get"
		case "remove":
			return "maml_map_remove"
		}
	}
	return method // fallback: use name as-is
}

// =============================================================================
// Helper Methods & Stub
// =============================================================================

// typeOf cleanly extracts the bound type from any TAST expression interface.
func typeOf(expr tast.Expr) types.Type {
	if expr == nil {
		return types.UnknownType{}
	}
	switch e := expr.(type) {
	case *tast.IntLiteral:
		return e.Type
	case *tast.BoolLiteral:
		return e.Type
	case *tast.StringLiteral:
		return e.Type
	case *tast.Identifier:
		return e.Type
	case *tast.InfixExpr:
		return e.Type
	case *tast.PrefixExpr:
		return e.Type
	case *tast.IfExpr:
		return e.Type
	case *tast.CallExpr:
		return e.Type
	case *tast.FieldAccess:
		return e.Type
	case *tast.IndexExpr:
		return e.Type
	case *tast.SliceExpr:
		return e.Type
	case *tast.StructLiteral:
		return e.Type
	case *tast.ArrayLiteral:
		return e.Type
	case *tast.VariantLiteral:
		return e.Type
	case *tast.AwaitExpr:
		return e.Type
	case *tast.MatchExpr:
		return e.Type
	case *tast.MethodCallExpr:
		return e.Type
	case *tast.MapLiteral:
		return e.Type
	case *tast.VecLiteral:
		return e.Type
	case *tast.BlockStmt:
		return TypeOfBlock(e)
	}
	return types.UnknownType{}
}

// TypeOfBlock evaluates the unified return type of a block by checking its final Yield statement.
func TypeOfBlock(block *tast.BlockStmt) types.Type {
	if block == nil || len(block.Statements) == 0 {
		return types.UnitType{}
	}
	last := block.Statements[len(block.Statements)-1]
	if yield, ok := last.(*tast.YieldStmt); ok {
		return typeOf(yield.Value)
	}
	return types.UnitType{}
}

func mergeTypes(t1, t2 types.Type) types.Type {
	if t1 == nil || t2 == nil {
		return nil
	}
	if t1.Equals(t2) {
		return t1
	}
	if !types.IsUnknown(t1) && !types.IsUnknown(t2) {
		// They conflict
		return types.UnknownType{}
	}
	if types.IsUnknown(t1) {
		return t2
	}
	return t1
}

func (a *Analyzer) lookupStruct(name string) *types.StructType {
	t := a.lookupCustomType(name)
	if t == nil {
		return nil
	}
	st, _ := t.(*types.StructType)
	return st
}

func (a *Analyzer) getMissingFields(st *types.StructType, seen map[string]bool) []string {
	var missing []string
	for _, f := range st.Fields {
		if !seen[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	return missing
}
