// sema/expression.go
package sema

import (
	"strings"

	"github.com/mattcarp12/maml/internal/ast"
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

// analyzeIdentifier resolves the symbol and guards against use-after-move.
func (a *Analyzer) analyzeIdentifier(e *ast.Identifier) Type {
	sym := a.resolve(e.Value)
	if sym == nil {
		a.errorf(e.Pos(), "undefined name '%s'", e.Value)
		return UnknownType{}
	}
	if sym.Invalidated {
		a.errorf(e.Pos(), "use of moved variable '%s'", e.Value)
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

// analyzeIfExpr handles an if-expression used as an expression (i.e. it yields
// a value via =>). It performs a single traversal of both branches and returns
// the yielded type. Return-path analysis is NOT done here — see
// analyzeIfExprAsStmt for the statement context.
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

	if !thenYield.Equals(UnknownType{}) && !elseYield.Equals(UnknownType{}) {
		if !thenYield.Equals(elseYield) {
			a.errorf(e.Pos(), "type mismatch: if branches yield different types ('%s' vs '%s')",
				thenYield.String(), elseYield.String())
		}
	}

	if !thenYield.Equals(UnknownType{}) {
		return thenYield
	}
	return elseYield
}

// analyzeIfExprAsStmt handles an if-expression used as a standalone statement.
// It performs a single traversal of both branches, computes the yield type,
// and also returns whether the statement guarantees a return on all paths.
// This replaces the old pattern of calling analyzeExpr + analyzeIfExprReturns
// separately, which caused each block to be analyzed twice.
func (a *Analyzer) analyzeIfExprAsStmt(e *ast.IfExpr) bool {
	cond := a.analyzeExpr(e.Condition)
	if !cond.Equals(BoolType{}) && !cond.Equals(UnknownType{}) {
		a.errorf(e.Pos(), "IF condition must be a boolean")
	}

	// Store the type result so analyzeExpr's TypeMap entry is populated,
	// matching the behaviour callers expect after analyzeExpr(ifExpr).
	consequenceReturns := a.analyzeBlockStmt(e.Consequence)
	thenYield := a.extractYieldType(e.Consequence)

	if e.Alternative == nil {
		a.TypeMap[e] = thenYield
		return false // if without else cannot guarantee return on all paths
	}

	alternativeReturns := a.analyzeBlockStmt(e.Alternative)
	elseYield := a.extractYieldType(e.Alternative)

	if !thenYield.Equals(UnknownType{}) && !elseYield.Equals(UnknownType{}) {
		if !thenYield.Equals(elseYield) {
			a.errorf(e.Pos(), "type mismatch: if branches yield different types ('%s' vs '%s')",
				thenYield.String(), elseYield.String())
		}
	}

	yieldType := elseYield
	if !thenYield.Equals(UnknownType{}) {
		yieldType = thenYield
	}
	a.TypeMap[e] = yieldType

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
			return OptionType{Base: innerType}

		case "None":
			if len(e.Arguments) != 0 {
				a.errorf(e.Pos(), "None takes no arguments")
				return UnknownType{}
			}
			// None without a known inner type — UnknownType base will be resolved
			// by context (the variable declaration or function return type).
			// For now return a sentinel; type inference would fix this properly.
			return OptionType{Base: UnknownType{}}

		case "Ok":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "Ok expects exactly 1 argument, got %d", len(e.Arguments))
				return UnknownType{}
			}
			innerType := a.analyzeExpr(e.Arguments[0].Argument)
			// Error type is unknown at construction — resolved by context.
			return ResultType{Value: innerType, Error: UnknownType{}}

		case "Err":
			if len(e.Arguments) != 1 {
				a.errorf(e.Pos(), "Err expects exactly 1 argument, got %d", len(e.Arguments))
				return UnknownType{}
			}
			errType := a.analyzeExpr(e.Arguments[0].Argument)
			return ResultType{Value: UnknownType{}, Error: errType}
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
		paramMode := fnType.ParamModes[i] // Ensure we look up param mode first!

		// Check if coercion is applicable
		isCoercible := false
		if _, isSlice := expectedType.(SliceType); isSlice {
			if arrTy, isArr := actualType.(ArrayType); isArr && arrTy.Base.Equals(expectedType.(SliceType).Base) {
				isCoercible = true
			}
			if vecTy, isVec := actualType.(VectorType); isVec && vecTy.Base.Equals(expectedType.(SliceType).Base) {
				// Safety Check: Slices are non-owning views. You cannot transfer ownership of a view!
				if paramMode == ParamOwned {
					a.errorf(arg.Argument.Pos(), "cannot pass owning Vec<T> as owned slice type []T; use a borrowed or mutable view")
				}
				isCoercible = true
			}
		}

		// Validate equality using the coercion condition
		if !isCoercible && !actualType.Equals(expectedType) && !actualType.Equals(UnknownType{}) {
			a.errorf(arg.Argument.Pos(), "argument %d type mismatch: expected '%s', got '%s'",
				i, expectedType.String(), actualType.String())
		}

		switch paramMode {
		case ParamBorrow:
			// Only plain pass allowed. mut and own are both illegal.
			if arg.Mut {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter is an immutable borrow, remove 'mut' at call site", i)
				continue
			}
			if arg.Own {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter is an immutable borrow, remove 'own' at call site", i)
				continue
			}
			if ident, ok := arg.Argument.(*ast.Identifier); ok {
				src := a.resolve(ident.Value)
				if src != nil && src.Invalidated {
					a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
				}
			}

		case ParamMutBorrow:
			// Only f(mut x) allowed, and source must be mutable.
			if arg.Own {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter is a mutable borrow, use 'mut' not 'own' at call site", i)
				continue
			}
			if !arg.Mut {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter is declared 'mut', call site must pass with 'mut'", i)
				continue
			}
			ident, ok := arg.Argument.(*ast.Identifier)
			if !ok {
				a.errorf(arg.Argument.Pos(),
					"argument %d: 'mut' can only be applied to a named variable", i)
				continue
			}
			src := a.resolve(ident.Value)
			if src == nil {
				continue
			}
			if src.Invalidated {
				a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
				continue
			}
			if !src.Mutable {
				a.errorf(arg.Argument.Pos(),
					"argument %d: cannot mutably borrow immutable variable '%s'", i, ident.Value)
				continue
			}
			// Mutable borrow: ownership stays with caller, no invalidation.

		case ParamOwned:
			// Only f(own x) allowed. Plain pass and mut are both illegal.
			if arg.Mut {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter requires ownership transfer, use 'own' not 'mut' at call site", i)
				continue
			}
			if !arg.Own {
				a.errorf(arg.Argument.Pos(),
					"argument %d: parameter is declared 'own', call site must pass with 'own'", i)
				continue
			}
			ident, ok := arg.Argument.(*ast.Identifier)
			if !ok {
				a.errorf(arg.Argument.Pos(),
					"argument %d: 'own' can only transfer ownership of a named variable", i)
				continue
			}
			src := a.resolve(ident.Value)
			if src == nil {
				continue
			}
			if src.Invalidated {
				a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
				continue
			}
			// own requires unique ownership — shared immutable values cannot be transferred.
			if a.hasActiveAliases(ident.Value) {
				a.errorf(arg.Argument.Pos(),
					"argument %d: cannot transfer ownership of '%s': value has active immutable aliases", i, ident.Value)
				continue
			}
			// Source may be mutable or immutable — both may be transferred if unique.
			src.Invalidated = true
			a.removeAlias(ident.Value)
		}
	}

	return fnType.Return
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
				Return:     UnknownType{},
			}
		case "push_own":
			// push_own takes ownership of the element.
			return &FunctionType{
				Params:     []Type{vecTy.Base},
				ParamModes: []ParamMode{ParamOwned},
				Return:     UnknownType{},
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
				Return:     UnknownType{},
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
