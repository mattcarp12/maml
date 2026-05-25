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
	case *ast.GenericType:
		return a.buildGenericType(e)
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
	var funcNode tast.Expr
	var funcType types.Type

	// Handle generic constructors (e.g., Vec<int>())
	if genType, isGen := e.Function.(*ast.GenericType); isGen {
		funcNode = a.buildGenericType(genType)
		funcType = typeOf(funcNode)
	} else {
		funcNode = a.buildExpr(e.Function)
		funcType = typeOf(funcNode)
	}

	var resultType types.Type = types.UnknownType{}

	// Validate arity and extract function signature
	ft, isFunc := funcType.(*types.FunctionType)
	if isFunc {
		if len(e.Arguments) != len(ft.Params) {
			a.errorf(e.Pos(), "wrong number of arguments: expected %d, got %d", len(ft.Params), len(e.Arguments))
		}
		resultType = ft.Return
	}

	var args []tast.CallArg
	for i, arg := range e.Arguments {
		argNode := a.buildExpr(arg.Argument)
		argType := typeOf(argNode)

		if isFunc && i < len(ft.Params) {
			// 1. Static Type Check
			if !argType.Equals(ft.Params[i]) && !types.IsUnknown(argType) {
				a.errorf(arg.Pos_, "argument %d type mismatch: expected '%s', got '%s'", i+1, ft.Params[i].String(), argType.String())
			}

			// 2. Static Capability / Modifier Check
			switch ft.ParamModes[i] {
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
	sym := a.resolve(e.Type.Value)
	if sym != nil && sym.Kind == types.VariantSymbol {
		return a.buildVariantLiteral(e, sym)
	}

	structDef := a.lookupStruct(e.Type.Value)
	if structDef == nil {
		a.errorf(e.Type.Pos(), "undefined struct type '%s'", e.Type.Value)
		return &tast.StructLiteral{Pos_: e.Pos_, Type: types.UnknownType{}}
	}

	var tastFields []tast.StructField
	seen := make(map[string]bool)

	for _, field := range e.Fields {
		a.checkStructFieldLiteral(&field, seen)
		valNode := a.buildExpr(field.Value)
		valType := typeOf(valNode)

		// Type check field
		expectedIdx := structDef.GetFieldIndex(field.Name.Value)
		if expectedIdx != -1 {
			expectedType := structDef.Fields[expectedIdx].Type
			if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
				a.errorf(field.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'",
					field.Name.Value, expectedType.String(), valType.String())
			}
		}

		tastFields = append(tastFields, tast.StructField{
			Pos_:  field.Pos_,
			End_:  field.End_,
			Name:  &tast.Identifier{Pos_: field.Name.Pos_, Value: field.Name.Value},
			Value: valNode,
		})
	}

	if missing := a.getMissingFields(structDef, seen); len(missing) > 0 {
		a.errorf(e.Pos(), "missing field '%s' in struct literal", missing)
	}

	return &tast.StructLiteral{
		Pos_:   e.Pos_,
		End_:   e.End_,
		Fields: tastFields,
		Type:   structDef,
	}
}

func (a *Analyzer) buildVariantLiteral(e *ast.StructLiteral, sym *types.Symbol) *tast.StructLiteral {
	variant := sym.Variant
	sumType := sym.SumType

	var tastFields []tast.StructField
	seen := make(map[string]bool)

	for _, field := range e.Fields {
		name := field.Name.Value
		if seen[name] {
			a.errorf(field.Name.Pos(), "duplicate field '%s' in variant literal", name)
			continue
		}
		seen[name] = true

		valNode := a.buildExpr(field.Value)
		valType := typeOf(valNode)

		// Find the expected type
		var expectedType types.Type
		for _, vField := range variant.Fields {
			if vField.Name == name {
				expectedType = vField.Type
				break
			}
		}

		if expectedType == nil {
			a.errorf(field.Name.Pos(), "field '%s' does not exist on variant '%s'", name, variant.Name)
		} else if !valType.Equals(expectedType) && !types.IsUnknown(valType) {
			a.errorf(field.Value.Pos(), "type mismatch for field '%s': expected '%s', got '%s'",
				name, expectedType.String(), valType.String())
		}

		tastFields = append(tastFields, tast.StructField{
			Pos_:  field.Pos_,
			End_:  field.End_,
			Name:  &tast.Identifier{Pos_: field.Name.Pos_, Value: name},
			Value: valNode,
		})
	}

	// Check missing
	for _, f := range variant.Fields {
		if !seen[f.Name] {
			a.errorf(e.Pos(), "missing field '%s' in variant literal", f.Name)
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

func (a *Analyzer) buildGenericType(e *ast.GenericType) *tast.Identifier {
	resolvedType := a.resolveAstType(e)
	return &tast.Identifier{
		Pos_:  e.Pos_,
		Value: e.Name,
		Type:  resolvedType,
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

func (a *Analyzer) checkStructFieldLiteral(literalField *ast.StructField, seen map[string]bool) {
	name := literalField.Name.Value
	if seen[name] {
		a.errorf(literalField.Name.Pos(), "duplicate field '%s' in struct literal", name)
		return
	}
	seen[name] = true
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
