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
		_ = a.analyzeIfExprReturns(e)
	case *ast.CallExpr:
		t = a.analyzeCallExpr(e)
	case *ast.StructLiteral:
		t = a.analyzeStructLiteral(e)
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

	// Intercept built-in methods on compiler-known Vector types
	if vecTy, isVec := objType.(VectorType); isVec {
		switch e.Field.Value {
		case "push":
			return &FunctionType{
				Params:     []Type{vecTy.Base},
				ParamModes: make([]ParamMode, 1), // Allocates default modes matching arg count
				Return:     UnknownType{},        // Treat as a void expression
			}
		case "len":
			return &FunctionType{
				Params:     []Type{},
				ParamModes: make([]ParamMode, 0),
				Return:     IntType{},
			}
		default:
			a.errorf(e.Field.Pos(), "unknown method '%s' on type '%s'", e.Field.Value, objType.String())
			return UnknownType{}
		}
	}

	// Intercept built-in methods on compiler-known Map types
	if mapTy, isMap := objType.(MapType); isMap {
		switch e.Field.Value {
		case "put":
			return &FunctionType{
				Params:     []Type{mapTy.Key, mapTy.Value},
				ParamModes: make([]ParamMode, 2),
				Return:     UnknownType{},
			}
		case "get":
			return &FunctionType{
				Params:     []Type{mapTy.Key},
				ParamModes: make([]ParamMode, 1),
				Return:     mapTy.Value, // Returns the typed Value description dynamically!
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
