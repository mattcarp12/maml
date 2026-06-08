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
	case *ast.CompositeLiteral:
		return a.buildCompositeLiteral(e)
	case *ast.FieldAccess:
		return a.buildFieldAccess(e)
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

			return &tast.VariantLiteral{
				Pos_:      e.Pos_,
				Variant:   sym.Variant,
				Arguments: nil,
				Fields:    nil,
				Type:      sumType,
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

	leftType := tast.TypeOf(leftNode)
	rightType := tast.TypeOf(rightNode)

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
	rightType := tast.TypeOf(rightNode)

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
	condType := tast.TypeOf(condNode)

	if !condType.Equals(types.BoolType{}) && !types.IsUnknown(condType) {
		a.errorf(e.Pos(), "IF condition must be a boolean")
	}

	consBlock := a.buildBlockStmt(e.Consequence)
	consYield := tast.TypeOfBlock(consBlock)

	var altBlock *tast.BlockStmt
	altYield := types.Type(types.UnitType{})

	if e.Alternative != nil {
		altBlock = a.buildBlockStmt(e.Alternative)
		altYield = tast.TypeOfBlock(altBlock)
	}

	resultType := types.MergeTypes(consYield, altYield)

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
	ft, ok := tast.TypeOf(funcNode).(*types.FunctionType)
	if !ok {
		a.errorf(e.Pos(), "cannot call non-function type '%s'", tast.TypeOf(funcNode).String())
		return &tast.CallExpr{
			Pos_:      e.Pos_,
			Function:  funcNode,
			Arguments: nil,
			Type:      types.UnknownType{},
		}
	}

	// --- Builtin Type Validation ---
	if funcIdent, isIdent := e.Function.(*ast.Identifier); isIdent {
		if funcIdent.Value == "len" && len(e.Arguments) == 1 {
			argType := tast.TypeOf(a.buildExpr(e.Arguments[0].Argument))
			switch argType.(type) {
			case types.VectorType, types.MapType, types.ArrayType, types.ViewType, types.StringType:
				// Valid collection
			default:
				a.errorf(e.Arguments[0].Pos(), "len() requires a collection type, got '%s'", argType.String())
			}
		} else if funcIdent.Value == "delete" && len(e.Arguments) == 2 {
			mapArg := tast.TypeOf(a.buildExpr(e.Arguments[0].Argument))
			if mapTy, isMap := mapArg.(types.MapType); isMap {
				keyArg := tast.TypeOf(a.buildExpr(e.Arguments[1].Argument))
				if !keyArg.Equals(mapTy.Key) && !types.IsUnknown(keyArg) {
					a.errorf(e.Arguments[1].Pos(), "delete() key type mismatch: expected '%s', got '%s'", mapTy.Key.String(), keyArg.String())
				}
			} else if !types.IsUnknown(mapArg) {
				a.errorf(e.Arguments[0].Pos(), "delete() requires a Map as the first argument, got '%s'", mapArg.String())
			}
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
		})

		// Validate parameter type and mode if within bounds
		if i < len(ft.Params) {
			expected := ft.Params[i]
			got := tast.TypeOf(valNode)
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

func (a *Analyzer) buildAwaitExpr(e *ast.AwaitExpr) *tast.AwaitExpr {
	if a.currentFn == nil || !a.currentFn.IsAsync {
		a.errorf(e.Pos(), "cannot use 'await' outside of an async function")
	}

	valNode := a.buildExpr(e.Value)
	valType := tast.TypeOf(valNode)

	var resultType types.Type = types.UnknownType{}
	if futTy, ok := valType.(types.FutureType); ok {
		resultType = futTy.Base
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

func (a *Analyzer) buildArrayFromComposite(e *ast.CompositeLiteral, ty types.ArrayType) *tast.ArrayLiteral {
	var elems []tast.Expr

	// 1. Guard against broken or unresolvable base types from previous analyzer errors
	if ty.Base == nil || types.IsUnknown(ty.Base) {
		return &tast.ArrayLiteral{Pos_: e.Pos_, Elements: nil, Type: types.UnknownType{}}
	}

	// 2. Fixed-Size Bounds Checking
	// Ensure the user hasn't provided more elements than the array can physically hold
	if len(e.Elements) > ty.Size {
		a.errorf(e.Pos(), "too many elements in array literal: declared size is %d, got %d", ty.Size, len(e.Elements))
		return &tast.ArrayLiteral{Pos_: e.Pos_, Elements: nil, Type: types.UnknownType{}}
	}

	// 3. Element Validation Loop
	for i, el := range e.Elements {
		// Securely reject key-value colons inside fixed array literals
		if el.Key != nil {
			a.errorf(el.Key.Pos(), "array literals cannot have keyed elements")
			continue
		}

		elNode := a.buildExpr(el.Value)
		elType := tast.TypeOf(elNode)
		elems = append(elems, elNode)

		// Validate every individual item directly against the explicit declared target base type
		if !elType.Equals(ty.Base) && !types.IsUnknown(elType) {
			a.errorf(el.Value.Pos(), "array element %d type mismatch: expected '%s', got '%s'", i, ty.Base.String(), elType.String())
		}
	}

	// Note on Partial Initialization:
	// If len(elems) < ty.Size, your code generator should handle filling the remaining
	// slots up to ty.Size with the base type's default zero-value (e.g., 0, false, null).

	return &tast.ArrayLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: elems,
		Type:     ty, // Safely preserve the exact user-declared ArrayType configuration
	}
}

func (a *Analyzer) buildIndexExpr(e *ast.IndexExpr) *tast.IndexExpr {
	leftNode := a.buildExpr(e.Left)
	idxNode := a.buildExpr(e.Index)

	leftType := tast.TypeOf(leftNode)
	idxType := tast.TypeOf(idxNode)

	var resultType types.Type = types.UnknownType{}
	switch ty := leftType.(type) {
	case types.ArrayType:
		resultType = ty.Base
	case types.ViewType:
		resultType = ty.Base
	case types.VectorType:
		resultType = ty.Base
	case types.MapType:
		resultType = types.NewOptionType(ty.Value) // m[k] returns Option<V>
	case types.StringType:
		resultType = types.IntType{} // Characters are currently ints
	default:
		if !types.IsUnknown(leftType) {
			a.errorf(e.Left.Pos(), "cannot index non-array/slice type '%s'", leftType.String())
		}
	}

	// Type check the index expression
	switch ty := leftType.(type) {
	case types.ArrayType, types.ViewType, types.VectorType, types.StringType:
		if !idxType.Equals(types.IntType{}) && !types.IsUnknown(idxType) {
			a.errorf(e.Index.Pos(), "index must be an integer, got '%s'", idxType.String())
		}
	case types.MapType:
		if !idxType.Equals(ty.Key) && !types.IsUnknown(ty.Key) {
			a.errorf(e.Index.Pos(), "index of map must be '%s', got '%s'", ty.Key.String(), idxType.String())
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
	leftType := tast.TypeOf(leftNode)

	var lowNode, highNode tast.Expr

	if e.Low != nil {
		lowNode = a.buildExpr(e.Low)
		if t := tast.TypeOf(lowNode); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.Low.Pos(), "slice low index must be an integer")
		}
	}
	if e.High != nil {
		highNode = a.buildExpr(e.High)
		if t := tast.TypeOf(highNode); !t.Equals(types.IntType{}) && !types.IsUnknown(t) {
			a.errorf(e.High.Pos(), "slice high index must be an integer")
		}
	}

	var resultType types.Type = types.UnknownType{}
	switch ty := leftType.(type) {
	case types.ArrayType:
		resultType = types.ViewType{Base: ty.Base}
	case types.VectorType:
		resultType = types.ViewType{Base: ty.Base}
	case types.ViewType:
		resultType = types.ViewType{Base: ty.Base}
	case types.StringType:
		resultType = types.StringType{}
	default:
		if !types.IsUnknown(leftType) {
			a.errorf(e.Left.Pos(), "cannot slice non-array/vector/view type '%s'", leftType.String())
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

func (a *Analyzer) buildStructFromComposite(e *ast.CompositeLiteral, ty *types.StructType) tast.Expr {
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

		valNode := a.buildExpr(elem.Value)
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

func (a *Analyzer) buildCompositeLiteral(e *ast.CompositeLiteral) tast.Expr {
	// Check if the type name refers to a variant constructor
	// rather than a top-level named type. Variant constructors live
	// in the symbol table, not the type table.
	if named, ok := e.TypeExpr.(*ast.NamedTypeExpr); ok {
		if sym := a.resolve(named.Name.Value); sym != nil && sym.Kind == types.VariantSymbol {
			if sym.SumType != nil {
				return a.buildStructVariantFromComposite(e, sym.SumType)
			}
		}
	}

	resolvedType := a.resolveAstType(e.TypeExpr)
	switch ty := resolvedType.(type) {
	case *types.StructType:
		return a.buildStructFromComposite(e, ty)
	case *types.SumType: // struct-style variant literal, e.g. Circle{radius: 5}
		return a.buildStructVariantFromComposite(e, ty)
	case types.ArrayType:
		return a.buildArrayFromComposite(e, ty)
	case types.VectorType:
		return a.buildVecLiteral(e, ty)
	case types.MapType:
		return a.buildMapLiteral(e, ty)
	default:
		if !types.IsUnknown(resolvedType) {
			a.errorf(e.Pos(), "type '%s' does not support literal instantiation", resolvedType.String())
		}
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}
}

func (a *Analyzer) buildStructVariantFromComposite(e *ast.CompositeLiteral, ty *types.SumType) *tast.VariantLiteral {
	var variantName string
	if named, ok := e.TypeExpr.(*ast.NamedTypeExpr); ok {
		variantName = named.Name.Value
	} else {
		a.errorf(e.TypeExpr.Pos(), "invalid type signature for variant instantiation")
		return nil
	}
	variant := ty.GetVariant(variantName)
	sumType := ty

	var tastFields []tast.VariantField
	seen := make(map[string]bool)

	for _, elems := range e.Elements {
		// 1. Variant fields MUST be keyed with identifiers
		ident, ok := elems.Key.(*ast.Identifier)
		if !ok || elems.Key == nil {
			a.errorf(elems.Value.Pos(), "variant fields must be keyed with identifiers (e.g., field: value)")
			continue
		}

		fieldName := ident.Value

		// 2. Prevent duplicate fields
		if seen[fieldName] {
			a.errorf(elems.Pos_, "duplicate field '%s' in variant literal", fieldName)
			continue
		}
		seen[fieldName] = true

		valNode := a.buildExpr(elems.Value)
		valType := tast.TypeOf(valNode)

		// 3. Find the expected type from the variant's definition
		var expectedType types.Type
		for _, vField := range variant.Fields {
			if vField.Name == fieldName {
				expectedType = vField.Type
				break
			}
		}

		if expectedType == nil {
			a.errorf(elems.Key.Pos(), "field '%s' does not exist on variant '%s'", fieldName, variant.Name)
		} else if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
			a.errorf(elems.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'", fieldName, expectedType.String(), valType.String())
		}

		tastFields = append(tastFields, tast.VariantField{
			Name:  fieldName,
			Value: valNode,
		})
	}

	// 4. Check for missing required fields
	for _, f := range variant.Fields {
		if !seen[f.Name] {
			a.errorf(e.Pos_, "missing field '%s' in variant literal", f.Name)
		}
	}

	return &tast.VariantLiteral{
		Pos_:      e.Pos_,
		End_:      e.End_,
		Variant:   variant,
		Arguments: nil,
		Fields:    tastFields,
		Type:      sumType,
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
			// Unify: Option<T> where T = tast.TypeOf(valNode)
			sumType = types.NewOptionType(tast.TypeOf(valNode))
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
			vType = tast.TypeOf(a.buildExpr(e.Arguments[0].Argument))
		} else if variant.Name == "Err" && len(e.Arguments) == 1 {
			eType = tast.TypeOf(a.buildExpr(e.Arguments[0].Argument))
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
			if !tast.TypeOf(valNode).Equals(variant.TupleTypes[i]) {
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
	objType := tast.TypeOf(objNode)

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
