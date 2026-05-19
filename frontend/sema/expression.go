// sema/expression.go
package sema

import (
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
)

func (a *Analyzer) analyzeExpr(expr ast.Expr) Type {
	var t Type

	switch e := expr.(type) {
	case *ast.IntLiteral:
		t = IntType{}
	case *ast.BoolLiteral:
		t = BoolType{}
	case *ast.StringLiteral:
		t = StringType{}
	case *ast.Identifier:
		t = a.analyzeIdentifier(e)
	case *ast.InfixExpr:
		t = a.analyzeInfixExpr(e)
	case *ast.PrefixExpr:
		t = a.analyzePrefixExpr(e)
	case *ast.IfExpr:
		t = a.analyzeIfExpr(e)
	case *ast.MatchExpr:
		t = a.analyzeMatchExpr(e)
	case *ast.CallExpr:
		t = a.analyzeCallExpr(e)
	case *ast.AwaitExpr:
		t = a.analyzeAwaitExpr(e)
	case *ast.StructLiteral:
		// Check if this is a variant constructor (e.g. Circle{radius: 5})
		// before falling through to struct literal analysis.
		if sym := a.resolve(e.Type.Value); sym != nil && sym.Kind == VariantSymbol {
			t = a.analyzeVariantLiteral(e, sym)
		} else {
			t = a.analyzeStructLiteral(e)
		}
	case *ast.FieldAccess:
		t = a.analyzeFieldAccess(e)
	case *ast.ArrayLiteral:
		t = a.analyzeArrayLiteral(e)
	case *ast.IndexExpr:
		t = a.analyzeIndexExpr(e)
	case *ast.SliceExpr:
		t = a.analyzeSliceExpr(e)
	default:
		a.errorf(expr.Pos(), "expression type not recognized: %T", expr)
		t = UnknownType{}
	}

	a.TypeMap[expr] = t
	return t
}

func (a *Analyzer) analyzeIdentifier(e *ast.Identifier) Type {
	sym := a.resolve(e.Value)
	if sym == nil {
		a.errorf(e.Pos(), "undefined name '%s'", e.Value)
		return UnknownType{}
	}
	return sym.Type
}
func (a *Analyzer) analyzeInfixExpr(e *ast.InfixExpr) Type {
	left := a.analyzeExpr(e.Left)
	right := a.analyzeExpr(e.Right)

	if !left.Equals(UnknownType{}) && !right.Equals(UnknownType{}) && !left.Equals(right) {
		a.errorf(e.Pos(), "type mismatch: cannot %s '%s' and '%s'",
			e.Operator, left.String(), right.String())
	}

	switch e.Operator {
	case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
		return BoolType{}
	default:
		return left
	}
}

func (a *Analyzer) analyzePrefixExpr(e *ast.PrefixExpr) Type {
	right := a.analyzeExpr(e.Right)

	switch e.Operator {
	case "!":
		if !right.Equals(BoolType{}) && !right.Equals(UnknownType{}) {
			a.errorf(e.Pos(), "operator '!' cannot be applied to type '%s'", right.String())
		}
		return BoolType{}
	case "-":
		if !right.Equals(IntType{}) && !right.Equals(UnknownType{}) {
			a.errorf(e.Pos(), "operator '-' cannot be applied to type '%s'", right.String())
		}
		return IntType{}
	default:
		a.errorf(e.Pos(), "unknown prefix operator '%s'", e.Operator)
		return UnknownType{}
	}
}

func (a *Analyzer) analyzeIfExpr(e *ast.IfExpr) Type {
	cond := a.analyzeExpr(e.Condition)
	if !cond.Equals(BoolType{}) && !cond.Equals(UnknownType{}) {
		a.errorf(e.Pos(), "IF condition must be a boolean")
	}

	a.analyzeBlockStmt(e.Consequence)
	thenYield := a.extractYieldType(e.Consequence)

	var elseYield Type = UnknownType{}
	if e.Alternative != nil {
		a.analyzeBlockStmt(e.Alternative)
		elseYield = a.extractYieldType(e.Alternative)
	}

	finalYield := thenYield
	if !thenYield.Equals(UnknownType{}) && !elseYield.Equals(UnknownType{}) {
		if !thenYield.Equals(elseYield) {
			if merged := mergeTypes(thenYield, elseYield); merged != nil {
				finalYield = merged
			} else {
				a.errorf(e.Pos(), "type mismatch: if branches yield different types ('%s' vs '%s')",
					thenYield.String(), elseYield.String())
			}
		}
	} else if thenYield.Equals(UnknownType{}) {
		finalYield = elseYield
	}

	// Back-propagate to ensure phi nodes match perfectly
	a.TypeMap[e] = finalYield
	a.updateBlockYield(e.Consequence, finalYield)
	if e.Alternative != nil {
		a.updateBlockYield(e.Alternative, finalYield)
	}

	return finalYield
}

func (a *Analyzer) analyzeIfExprAsStmt(e *ast.IfExpr) bool {
	cond := a.analyzeExpr(e.Condition)
	if !cond.Equals(BoolType{}) && !cond.Equals(UnknownType{}) {
		a.errorf(e.Pos(), "IF condition must be a boolean")
	}

	consequenceReturns := a.analyzeBlockStmt(e.Consequence)
	thenYield := a.extractYieldType(e.Consequence)

	if e.Alternative == nil {
		a.TypeMap[e] = thenYield
		a.updateBlockYield(e.Consequence, thenYield)
		return false
	}

	alternativeReturns := a.analyzeBlockStmt(e.Alternative)
	elseYield := a.extractYieldType(e.Alternative)

	finalYield := thenYield
	if !thenYield.Equals(UnknownType{}) && !elseYield.Equals(UnknownType{}) {
		if !thenYield.Equals(elseYield) {
			if merged := mergeTypes(thenYield, elseYield); merged != nil {
				finalYield = merged
			} else {
				a.errorf(e.Pos(), "type mismatch: if branches yield different types ('%s' vs '%s')",
					thenYield.String(), elseYield.String())
				finalYield = UnknownType{}
			}
		}
	} else if thenYield.Equals(UnknownType{}) {
		finalYield = elseYield
	}

	a.TypeMap[e] = finalYield
	a.updateBlockYield(e.Consequence, finalYield)
	a.updateBlockYield(e.Alternative, finalYield)

	return consequenceReturns && alternativeReturns
}

func (a *Analyzer) extractYieldType(block *ast.BlockStmt) Type {
	if len(block.Statements) == 0 {
		return UnknownType{}
	}
	last := block.Statements[len(block.Statements)-1]
	if yield, ok := last.(*ast.YieldStmt); ok {
		return a.TypeMap[yield.Value]
	}
	return UnknownType{}
}

// analyzeCallExpr type-checks a call and enforces ownership transfer for own arguments.
func (a *Analyzer) analyzeCallExpr(e *ast.CallExpr) Type {
	// NEW: Check if we are invoking a constructor on a generic type symbol
	if genType, isGen := e.Function.(*ast.GenericType); isGen {
		resolvedType := a.resolveAstType(genType)

		// Ensure constructors are called with 0 arguments for bootstrapping
		if len(e.Arguments) != 0 {
			a.errorf(e.Pos(), "container constructor expects 0 arguments")
		}
		return resolvedType
	}

	// Intercept variant constructors: Some(x), None, Ok(x), Err(e)
	if ident, ok := e.Function.(*ast.Identifier); ok {
		switch ident.Value {
		case "Some":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "Some expects exactly 1 argument, got %d", len(e.Arguments))
				return UnknownType{}
			}
			innerType := a.analyzeExpr(e.Arguments[0].Argument)
			return NewOptionType(innerType)

		case "None":
			if len(e.Arguments) != 0 {
				a.errorf(e.Pos(), "None takes no arguments")
				return UnknownType{}
			}
			// None without a known inner type — UnknownType base will be resolved
			// by context (the variable declaration or function return type).
			// For now return a sentinel; type inference would fix this properly.
			return NewOptionType(UnknownType{})

		case "Ok":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "Ok expects exactly 1 argument, got %d", len(e.Arguments))
				return UnknownType{}
			}
			innerType := a.analyzeExpr(e.Arguments[0].Argument)
			// Error type is unknown at construction — resolved by context.
			return NewResultType(innerType, UnknownType{})

		case "Err":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "Err expects exactly 1 argument, got %d", len(e.Arguments))
				return UnknownType{}
			}
			errType := a.analyzeExpr(e.Arguments[0].Argument)
			return NewResultType(UnknownType{}, errType)

		// NEW: Compiler Built-ins for Concurrency
		case "spawn":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "spawn expects exactly 1 argument (a Task)")
				return UnknownType{}
			}
			argType := a.analyzeExpr(e.Arguments[0].Argument)
			if _, isTask := argType.(TaskType); !isTask {
				a.errorf(e.Arguments[0].Argument.Pos(), "spawn expects a Task, got '%s'", argType.String())
			}
			return IntType{} // Return dummy int (e.g., 0 for success)

		case "run_executor":
			if len(e.Arguments) != 0 {
				a.errorf(e.Pos(), "run_executor expects 0 arguments")
			}
			return IntType{} // Return int so it can be returned out of main
		}
	}

	fnTypeExpr := a.analyzeExpr(e.Function)
	fnType, ok := fnTypeExpr.(*FunctionType)
	if !ok {
		if !fnTypeExpr.Equals(UnknownType{}) {
			a.errorf(e.Pos(), "cannot call non-function type '%s'", fnTypeExpr.String())
		}
		return UnknownType{}
	}

	if len(e.Arguments) != len(fnType.Params) {
		a.errorf(e.Pos(), "wrong number of arguments: expected %d, got %d",
			len(fnType.Params), len(e.Arguments))
		return UnknownType{}
	}

	for i, arg := range e.Arguments {
		actualType := a.analyzeExpr(arg.Argument)
		expectedType := fnType.Params[i]

		isCoercible := false
		if _, isSlice := expectedType.(SliceType); isSlice {
			if arrTy, isArr := actualType.(ArrayType); isArr && arrTy.Base.Equals(expectedType.(SliceType).Base) {
				isCoercible = true
			}
			if vecTy, isVec := actualType.(VectorType); isVec && vecTy.Base.Equals(expectedType.(SliceType).Base) {
				isCoercible = true
			}
		}

		if !isCoercible && !actualType.Equals(expectedType) && !actualType.Equals(UnknownType{}) {
			a.errorf(arg.Argument.Pos(), "argument %d type mismatch: expected '%s', got '%s'",
				i, expectedType.String(), actualType.String())
		}
	}

	return fnType.Return
}

func (a *Analyzer) analyzeAwaitExpr(e *ast.AwaitExpr) Type {
	// 1. Context Validation: Must be inside an async function
	if a.currentFn == nil || !a.currentFn.IsAsync {
		a.errorf(e.Pos(), "cannot use 'await' outside of an async function")
		return UnknownType{}
	}

	// 2. Type Validation: Evaluate the right side and ensure it's a Task
	right := a.analyzeExpr(e.Value)

	taskType, isTask := right.(TaskType)
	if !isTask {
		if !right.Equals(UnknownType{}) { // Prevent cascading errors
			a.errorf(e.Pos(), "cannot await non-task type '%s'", right.String())
		}
		return UnknownType{}
	}

	// 3. Unbox: Await yields the inner Base/Result value
	return taskType.Base
}

func (a *Analyzer) analyzeStructLiteral(e *ast.StructLiteral) Type {
	structDef := a.lookupStruct(e.Type.Value)
	if structDef == nil {
		a.errorf(e.Type.Pos(), "undefined struct type '%s'", e.Type.Value)
		return UnknownType{}
	}

	seen := make(map[string]bool)
	for _, f := range e.Fields {
		a.checkStructFieldLiteral(structDef, &f, seen)
	}

	if len(seen) < len(structDef.Fields) {
		missing := a.getMissingFields(structDef, seen)
		a.errorf(e.Pos(), "missing fields in struct literal: %s", strings.Join(missing, ", "))
	}

	return structDef
}

func (a *Analyzer) lookupStruct(name string) *StructType {
	t := a.lookupCustomType(name)
	if t == nil {
		return nil
	}
	st, _ := t.(*StructType)
	return st
}

func (a *Analyzer) checkStructFieldLiteral(st *StructType, literalField *ast.StructField, seen map[string]bool) {
	name := literalField.Name.Value
	if seen[name] {
		a.errorf(literalField.Name.Pos(), "duplicate field '%s' in struct literal", name)
		return
	}
	seen[name] = true

	idx := st.GetFieldIndex(name)
	if idx < 0 {
		a.errorf(literalField.Name.Pos(), "struct '%s' has no field '%s'", st.Name, name)
		return
	}

	expected := st.Fields[idx].Type
	actual := a.analyzeExpr(literalField.Value)
	if !actual.Equals(expected) && !actual.Equals(UnknownType{}) {
		a.errorf(literalField.Value.Pos(),
			"type mismatch for field '%s': expected '%s', got '%s'",
			name, expected.String(), actual.String())
	}
}

func (a *Analyzer) getMissingFields(st *StructType, seen map[string]bool) []string {
	var missing []string
	for _, f := range st.Fields {
		if !seen[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	return missing
}

func (a *Analyzer) analyzeFieldAccess(e *ast.FieldAccess) Type {
	objType := a.analyzeExpr(e.Object)
	if objType.Equals(UnknownType{}) {
		return UnknownType{}
	}

	// Built-in methods on Vec<T>
	if vecTy, isVec := objType.(VectorType); isVec {
		switch e.Field.Value {
		case "push":
			// push takes the element by value (immutable borrow — caller retains ownership).
			return &FunctionType{
				Params:     []Type{vecTy.Base},
				ParamModes: []ParamMode{ParamBorrow},
				Return:     UnitType{},
			}
		case "push_own":
			// push_own takes ownership of the element.
			return &FunctionType{
				Params:     []Type{vecTy.Base},
				ParamModes: []ParamMode{ParamOwned},
				Return:     UnitType{},
			}
		case "len":
			return &FunctionType{
				Params:     []Type{},
				ParamModes: []ParamMode{},
				Return:     IntType{},
			}
		default:
			a.errorf(e.Field.Pos(), "unknown method '%s' on type '%s'", e.Field.Value, objType.String())
			return UnknownType{}
		}
	}

	// Built-in methods on Map<K, V>
	if mapTy, isMap := objType.(MapType); isMap {
		switch e.Field.Value {
		case "put":
			// put takes both key and value by immutable borrow.
			return &FunctionType{
				Params:     []Type{mapTy.Key, mapTy.Value},
				ParamModes: []ParamMode{ParamBorrow, ParamBorrow},
				Return:     UnitType{},
			}
		case "get":
			// get takes the key by immutable borrow, returns the value type.
			return &FunctionType{
				Params:     []Type{mapTy.Key},
				ParamModes: []ParamMode{ParamBorrow},
				Return:     mapTy.Value,
			}
		default:
			a.errorf(e.Field.Pos(), "unknown method '%s' on type '%s'", e.Field.Value, objType.String())
			return UnknownType{}
		}
	}

	st, ok := objType.(*StructType)
	if !ok {
		a.errorf(e.Object.Pos(), "cannot access field '%s' on non-struct type '%s'",
			e.Field.Value, objType.String())
		return UnknownType{}
	}

	idx := st.GetFieldIndex(e.Field.Value)
	if idx < 0 {
		a.errorf(e.Field.Pos(), "field '%s' does not exist on struct '%s'", e.Field.Value, st.Name)
		return UnknownType{}
	}

	return st.Fields[idx].Type
}

func (a *Analyzer) analyzeArrayLiteral(e *ast.ArrayLiteral) Type {
	if len(e.Elements) == 0 {
		a.errorf(e.Pos(), "cannot infer type of empty array literal")
		return UnknownType{}
	}

	firstType := a.analyzeExpr(e.Elements[0])
	for i, elem := range e.Elements[1:] {
		elemType := a.analyzeExpr(elem)
		if !elemType.Equals(firstType) && !elemType.Equals(UnknownType{}) {
			a.errorf(elem.Pos(), "array element %d type mismatch: expected '%s', got '%s'",
				i+1, firstType.String(), elemType.String())
		}
	}

	return ArrayType{Base: firstType, Size: len(e.Elements)}
}

func (a *Analyzer) analyzeIndexExpr(e *ast.IndexExpr) Type {
	leftType := a.analyzeExpr(e.Left)
	indexType := a.analyzeExpr(e.Index)

	if leftType.Equals(UnknownType{}) || indexType.Equals(UnknownType{}) {
		return UnknownType{}
	}

	var baseType Type

	switch t := leftType.(type) {
	case ArrayType:
		baseType = t.Base
	case SliceType:
		baseType = t.Base
	case StringType:
		baseType = IntType{}
	default:
		a.errorf(e.Left.Pos(), "cannot index non-array/slice type '%s'", leftType.String())
		return UnknownType{}
	}

	if !indexType.Equals(IntType{}) {
		a.errorf(e.Index.Pos(), "index must be an integer, got '%s'", indexType.String())
		return UnknownType{}
	}

	return baseType
}

func (a *Analyzer) analyzeSliceExpr(e *ast.SliceExpr) Type {
	leftType := a.analyzeExpr(e.Left)
	if leftType.Equals(UnknownType{}) {
		return UnknownType{}
	}

	var baseType Type

	switch t := leftType.(type) {
	case ArrayType:
		baseType = t.Base
	case SliceType:
		baseType = t.Base
	default:
		a.errorf(e.Left.Pos(), "cannot slice non-array/slice type '%s'", leftType.String())
		return UnknownType{}
	}

	if e.Low != nil {
		lowType := a.analyzeExpr(e.Low)
		if !lowType.Equals(IntType{}) && !lowType.Equals(UnknownType{}) {
			a.errorf(e.Low.Pos(), "slice low index must be an integer, got '%s'", lowType.String())
		}
	}

	if e.High != nil {
		highType := a.analyzeExpr(e.High)
		if !highType.Equals(IntType{}) && !highType.Equals(UnknownType{}) {
			a.errorf(e.High.Pos(), "slice high index must be an integer, got '%s'", highType.String())
		}
	}

	return SliceType{Base: baseType}
}

func (a *Analyzer) analyzeVariantLiteral(e *ast.StructLiteral, sym *Symbol) Type {
	variant := sym.Variant
	sumType := sym.SumType

	if variant.IsUnit() && len(e.Fields) > 0 {
		a.errorf(e.Pos(), "unit variant '%s' takes no fields", variant.Name)
		return sumType
	}

	// Check fields match the variant's declared fields.
	seen := make(map[string]bool)
	for _, f := range e.Fields {
		name := f.Name.Value
		if seen[name] {
			a.errorf(f.Name.Pos(), "duplicate field '%s' in variant literal", name)
			continue
		}
		seen[name] = true

		// Find the field in the variant definition.
		found := false
		for _, vf := range variant.Fields {
			if vf.Name == name {
				found = true
				actual := a.analyzeExpr(f.Value)
				if !actual.Equals(vf.Type) && !actual.Equals(UnknownType{}) {
					a.errorf(f.Value.Pos(),
						"type mismatch for field '%s': expected '%s', got '%s'",
						name, vf.Type.String(), actual.String())
				}
				break
			}
		}
		if !found {
			a.errorf(f.Name.Pos(), "variant '%s' has no field '%s'", variant.Name, name)
		}
	}

	// Check for missing fields.
	for _, vf := range variant.Fields {
		if !seen[vf.Name] {
			a.errorf(e.Pos(), "missing field '%s' in variant '%s'", vf.Name, variant.Name)
		}
	}

	return sumType
}

func mergeTypes(a, b Type) Type {
	if a == nil || b == nil {
		return nil
	}

	// 1. Direct Unknown resolution
	if isUnknown(a) {
		return b
	}
	if isUnknown(b) {
		return a
	}

	// 2. Generic SumType Unification
	stA, okA := a.(*SumType)
	stB, okB := b.(*SumType)
	if okA && okB {
		// Structurally verify they are the same base type with the same number of arguments
		if stA.BaseName == stB.BaseName && len(stA.TypeArgs) == len(stB.TypeArgs) {

			// Unify the generic type arguments
			mergedArgs := make([]Type, len(stA.TypeArgs))
			for i := range stA.TypeArgs {
				mergedArg := mergeTypes(stA.TypeArgs[i], stB.TypeArgs[i])
				if mergedArg == nil {
					return nil // Type arguments are incompatible
				}
				mergedArgs[i] = mergedArg
			}

			// If it's a built-in generic, use the unified arguments to regenerate the type
			switch stA.BaseName {
			case "Option":
				return NewOptionType(mergedArgs[0])
			case "Result":
				return NewResultType(mergedArgs[0], mergedArgs[1])
			}

			// For user-defined SumTypes, return a merged copy
			merged := &SumType{
				BaseName: stA.BaseName,
				TypeArgs: mergedArgs,
			}

			// ... (keep your existing loop that unifies stA.Variants and stB.Variants here) ...

			return merged
		}
	}

	// 3. Fallback for primitives / basic structs
	if a.Equals(b) {
		return a
	}

	return nil
}

// Helper: Propagates a fully unified type down to nested branching expressions.
func (a *Analyzer) propagateType(expr ast.Expr, t Type) {
	if expr == nil {
		return
	}
	a.TypeMap[expr] = t

	switch e := expr.(type) {
	case *ast.IfExpr:
		if e.Consequence != nil {
			a.updateBlockYield(e.Consequence, t)
		}
		if e.Alternative != nil {
			a.updateBlockYield(e.Alternative, t)
		}
	case *ast.MatchExpr:
		for _, arm := range e.Arms {
			a.updateBlockYield(arm.Body, t)
		}
	}
}

// Helper: Finds the final YieldStmt in a block and updates its type.
func (a *Analyzer) updateBlockYield(block *ast.BlockStmt, t Type) {
	if len(block.Statements) == 0 {
		return
	}
	last := block.Statements[len(block.Statements)-1]
	if yield, ok := last.(*ast.YieldStmt); ok {
		a.propagateType(yield.Value, t)
	}
}
