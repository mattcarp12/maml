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

func (a *Analyzer) buildIdentifier(e *ast.Identifier) *tast.Identifier {
	sym := a.resolve(e.Value)
	if sym == nil {
		a.errorf(e.Pos(), "undefined name '%s'", e.Value)
		return &tast.Identifier{Pos_: e.Pos_, Value: e.Value, Type: types.UnknownType{}}
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
	consYield := typeOfBlock(consBlock)

	var altBlock *tast.BlockStmt
	altYield := types.Type(types.UnitType{})

	if e.Alternative != nil {
		altBlock = a.buildBlockStmt(e.Alternative)
		altYield = typeOfBlock(altBlock)
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

func (a *Analyzer) buildCallExpr(e *ast.CallExpr) *tast.CallExpr {
	funcNode := a.buildExpr(e.Function)
	funcType := typeOf(funcNode)

	var resultType types.Type = types.UnknownType{}
	var expectedParams []types.Type
	var paramModes []types.ParamMode
	isFunc := false
	isVariant := false

	if ft, ok := funcType.(*types.FunctionType); ok {
		isFunc = true
		if len(e.Arguments) != len(ft.Params) {
			a.errorf(e.Pos(), "wrong number of arguments: expected %d, got %d", len(ft.Params), len(e.Arguments))
		}
		resultType = ft.Return
		expectedParams = ft.Params
		paramModes = ft.ParamModes

	} else if ident, ok := funcNode.(*tast.Identifier); ok && ident.Symbol != nil && ident.Symbol.Kind == types.VariantSymbol {
		// NEW: Handle Tuple Variant Instantiation!
		isVariant = true
		variant := ident.Symbol.Variant
		if len(e.Arguments) != len(variant.TupleTypes) {
			a.errorf(e.Pos(), "wrong number of arguments for tuple variant '%s': expected %d, got %d", variant.Name, len(variant.TupleTypes), len(e.Arguments))
		}
		resultType = ident.Symbol.SumType
		expectedParams = variant.TupleTypes
	} else if !types.IsUnknown(funcType) {
		a.errorf(e.Pos(), "cannot call non-function type '%s'", funcType.String())
	}

	var args []tast.CallArg
	for i, arg := range e.Arguments {
		argNode := a.buildExpr(arg.Argument)
		argType := typeOf(argNode)

		if (isFunc || isVariant) && i < len(expectedParams) {
			if !argType.Equals(expectedParams[i]) && !types.IsUnknown(argType) && !types.IsUnknown(expectedParams[i]) {
				a.errorf(arg.Pos_, "argument %d type mismatch: expected '%s', got '%s'", i+1, expectedParams[i].String(), argType.String())
			}

			if isFunc {
				switch paramModes[i] {
				case types.ParamMutBorrow:
					if !arg.Mut {
						a.errorf(arg.Pos_, "argument %d requires 'mut' modifier to match function signature", i+1)
					}
					if arg.Own {
						a.errorf(arg.Pos_, "argument %d expects 'mut', got 'own'", i+1)
					}
				case types.ParamOwned:
					if !arg.Own {
						a.errorf(arg.Pos_, "argument %d requires 'own' modifier to match function signature", i+1)
					}
					if arg.Mut {
						a.errorf(arg.Pos_, "argument %d expects 'own', got 'mut'", i+1)
					}
				case types.ParamBorrow:
					if arg.Mut || arg.Own {
						a.errorf(arg.Pos_, "argument %d does not take ownership modifiers", i+1)
					}
				}
			} else if isVariant {
				if arg.Mut || arg.Own {
					a.errorf(arg.Pos_, "argument %d does not take ownership modifiers for variant instantiation", i+1)
				}
			}
		}

		args = append(args, tast.CallArg{
			Pos_:     arg.Pos_,
			Argument: argNode,
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}

	return &tast.CallExpr{
		Pos_:      e.Pos_,
		Function:  funcNode,
		Arguments: args,
		Type:      resultType,
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

	return &tast.MethodCallExpr{
		Pos_:      e.Pos_,
		Object:    objNode,
		Method:    &tast.Identifier{Pos_: e.Method.Pos(), Value: methodName, Type: types.UnknownType{}},
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

func (a *Analyzer) buildStructLiteral(e *ast.StructLiteral) *tast.StructLiteral {
	// e.Type is now an ast.Expr! We type-switch to determine how to build it.
	switch typeNode := e.Type.(type) {

	// =========================================================================
	// CASE 1: Standard Structs and Variants (e.g., User{name: "Alice"})
	// =========================================================================
	case *ast.Identifier:
		name := typeNode.Value
		sym := a.resolve(name)
		if sym != nil && sym.Kind == types.VariantSymbol {
			// Note: You will need to update buildVariantLiteral to handle field.Key
			// instead of field.Name, just like we do for structs below!
			return a.buildVariantLiteral(e, sym)
		}

		structDef := a.lookupStruct(name)
		if structDef == nil {
			a.errorf(e.Type.Pos(), "undefined struct type '%s'", name)
			return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
		}

		var tastFields []tast.StructField
		seen := make(map[string]bool)

		for _, field := range e.Fields {
			// Struct keys MUST be identifiers
			ident, ok := field.Key.(*ast.Identifier)
			if !ok || field.Key == nil {
				a.errorf(field.Value.Pos(), "struct fields must be keyed with identifiers (e.g., name: value)")
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

			// Type check the field against the struct definition
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
				Pos_: field.Pos_,
				End_: field.End_,
				// FIX: Do not use a.buildExpr! Just wrap the raw name in an Identifier.
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

	// =========================================================================
	// CASE 2: Map Literal Instantiation (e.g., Map<string, int>{"score": 100})
	// =========================================================================
	case *ast.MapTypeExpr:
		mapType := a.resolveAstType(typeNode).(types.MapType)
		var tastFields []tast.StructField

		for _, field := range e.Fields {
			if field.Key == nil {
				a.errorf(field.Value.Pos(), "map literals require keyed fields (e.g., key: value)")
				continue
			}

			keyNode := a.buildExpr(field.Key)
			valNode := a.buildExpr(field.Value)

			keyType := typeOf(keyNode)
			valType := typeOf(valNode)

			if !keyType.Equals(mapType.Key) && !types.IsUnknown(keyType) {
				a.errorf(field.Key.Pos(), "type mismatch for map key: expected '%s', got '%s'", mapType.Key.String(), keyType.String())
			}
			if !valType.Equals(mapType.Value) && !types.IsUnknown(valType) {
				a.errorf(field.Value.Pos(), "type mismatch for map value: expected '%s', got '%s'", mapType.Value.String(), valType.String())
			}

			tastFields = append(tastFields, tast.StructField{
				Pos_:  field.Pos_,
				End_:  field.End_,
				Key:   keyNode, // TAST now holds the fully typed key expression
				Value: valNode,
			})
		}

		return &tast.StructLiteral{
			Pos_:   e.Pos_,
			End_:   e.End_,
			Fields: tastFields,
			Type:   mapType,
		}

	// =========================================================================
	// CASE 3: Vector Literal Instantiation (e.g., Vec<int>{1, 2, 3})
	// =========================================================================
	case *ast.VectorTypeExpr:
		vecType := a.resolveAstType(typeNode).(types.VectorType)
		var tastFields []tast.StructField

		for i, field := range e.Fields {
			if field.Key != nil {
				a.errorf(field.Key.Pos(), "vector literals should be a simple list of values, not keyed")
				continue
			}

			valNode := a.buildExpr(field.Value)
			valType := typeOf(valNode)

			if !valType.Equals(vecType.Base) && !types.IsUnknown(valType) {
				a.errorf(field.Value.Pos(), "type mismatch for vector element %d: expected '%s', got '%s'", i, vecType.Base.String(), valType.String())
			}

			tastFields = append(tastFields, tast.StructField{
				Pos_:  field.Pos_,
				End_:  field.End_,
				Key:   nil, // Safely unkeyed in TAST!
				Value: valNode,
			})
		}

		return &tast.StructLiteral{
			Pos_:   e.Pos_,
			End_:   e.End_,
			Fields: tastFields,
			Type:   vecType,
		}

	// =========================================================================
	// DEFAULT: Invalid left-hand side
	// =========================================================================
	default:
		a.errorf(e.Type.Pos(), "invalid type for literal instantiation")
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}
}

func (a *Analyzer) buildVariantLiteral(e *ast.StructLiteral, sym *types.Symbol) *tast.StructLiteral {
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
	case *tast.AwaitExpr:
		return e.Type
	case *tast.MatchExpr:
		return e.Type
	}
	return types.UnknownType{}
}

// typeOfBlock evaluates the unified return type of a block by checking its final Yield statement.
func typeOfBlock(block *tast.BlockStmt) types.Type {
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
