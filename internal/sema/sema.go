package sema

import (
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/internal/ast"
)

type SymbolKind string

const (
	VarSymbol  SymbolKind = "var"
	FuncSymbol SymbolKind = "func"
)

type Scope struct {
	parent  *Scope
	symbols map[string]*Symbol
	types   map[string]Type
}

type Symbol struct {
	Kind    SymbolKind // NEW: Tells us what this symbol is
	Name    string
	Mutable bool
	Type    Type
}

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	TypeMap        map[ast.Node]Type
	expectedReturn Type
}

func New() *Analyzer {
	globalScope := &Scope{
		symbols: make(map[string]*Symbol),
		types:   make(map[string]Type),
	}

	// Properly register 'puts' as a FuncSymbol
	globalScope.symbols["puts"] = &Symbol{
		Kind:    FuncSymbol,
		Name:    "puts",
		Mutable: false,
		Type: &FunctionType{
			Params: []Type{StringType{}},
			Return: IntType{},
		},
	}

	return &Analyzer{
		scope:          globalScope,
		errors:         []ast.CompileError{},
		TypeMap:        make(map[ast.Node]Type),
		expectedReturn: nil,
	}
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func (a *Analyzer) Analyze(program *ast.Program) ([]ast.CompileError, map[ast.Node]Type) {
	a.pushScope() // Global scope

	// PASS 1: Type Discovery
	// Just register the names so recursive types can find each other.
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, exists := a.scope.types[td.Name.Name]; exists {
				a.errorf(td.Pos(), "type '%s' already defined", td.Name.Name)
				continue
			}
			// Register an empty shell of the struct
			a.scope.types[td.Name.Name] = &StructType{Name: td.Name.Name}
		}
	}

	// PASS 2: Type Resolution
	// Now that all names are known, resolve the fields.
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			a.resolveTypeBody(td)
		}
	}

	// PASS 3: Function Signatures
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.registerFunction(fn)
		}
	}

	// PASS 4: Function Bodies (The final check)
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.analyzeFnBody(fn)
		}
	}

	a.popScope()
	return a.errors, a.TypeMap
}

func (a *Analyzer) pushScope() {
	a.scope = &Scope{
		parent:  a.scope,
		symbols: make(map[string]*Symbol),
		types:   make(map[string]Type),
	}
}

func (a *Analyzer) popScope() {
	if a.scope != nil {
		a.scope = a.scope.parent
	}
}

func (a *Analyzer) resolve(name string) *Symbol {
	for s := a.scope; s != nil; s = s.parent {
		if sym, ok := s.symbols[name]; ok {
			return sym
		}
	}
	return nil
}

func (a *Analyzer) registerFunction(v *ast.FnDecl) {
	var paramTypes []Type
	for _, p := range v.Params {
		paramTypes = append(paramTypes, a.resolveAstType(p.Type))
	}
	retType := a.resolveAstType(v.ReturnType)

	// Register the signature fully populated with real Types!
	a.scope.symbols[v.Name] = &Symbol{
		Kind:    FuncSymbol,
		Name:    v.Name,
		Mutable: false,
		Type:    &FunctionType{Params: paramTypes, Return: retType},
	}
}

func (a *Analyzer) resolveTypeBody(v *ast.TypeDecl) {
	// Look up the shell we created in Pass 1
	existingType := a.scope.types[v.Name.Name]
	st, ok := existingType.(*StructType)
	if !ok {
		return
	}

	if pt, ok := v.Rhs.(*ast.ProductType); ok {
		for _, f := range pt.Fields {
			// Because of Pass 1, resolveAstType can now find 'Node'
			// even while we are still defining 'Node'!
			fieldType := a.resolveAstType(f.Type)
			st.Fields = append(st.Fields, StructField{
				Name: f.Name,
				Type: fieldType,
			})
		}
	}
}

func (a *Analyzer) analyzeFnBody(v *ast.FnDecl) {
	a.pushScope()

	// Bind parameters into the local scope as Variables
	for _, param := range v.Params {
		pType := a.resolveAstType(param.Type)
		a.scope.symbols[param.Name] = &Symbol{
			Kind:    VarSymbol,
			Name:    param.Name,
			Mutable: false,
			Type:    pType,
		}
		a.TypeMap[param.Type] = pType
	}

	a.expectedReturn = a.resolveAstType(v.ReturnType)

	alwaysReturns := a.analyzeBlockStmt(v.Body)
	if !alwaysReturns {
		a.errorf(v.Pos(), "function '%s' is missing a return statement", v.Name)
	}

	a.expectedReturn = nil
	a.popScope()
}

func (a *Analyzer) resolveAstType(expr ast.TypeExpr) Type {
	switch t := expr.(type) {
	case *ast.NamedType:
		switch t.Name {
		case "int":
			return IntType{}
		case "bool":
			return BoolType{}
		case "string":
			return StringType{}
		default:
			for s := a.scope; s != nil; s = s.parent {
				if custom, ok := s.types[t.Name]; ok {
					return custom
				}
			}
			a.errorf(t.Pos(), "unknown type %s", t.Name)
			return UnknownType{}
		}
	}
	return UnknownType{}
}

// Returns true if the block guarantees a return statement
func (a *Analyzer) analyzeBlockStmt(body *ast.BlockStmt) bool {
	alwaysReturns := false
	for _, stmt := range body.Statements {
		// If any statement in this block returns, the block returns
		if a.analyzeStmt(stmt) {
			alwaysReturns = true
		}
	}
	return alwaysReturns
}

// Returns true if the statement is a guaranteed return
func (a *Analyzer) analyzeStmt(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		exprType := a.analyzeExpr(s.Value)
		if _, exists := a.scope.symbols[s.Name]; exists {
			a.errorf(s.Pos(), "variable '%s' is already declared", s.Name)
			return false
		}
		a.scope.symbols[s.Name] = &Symbol{
			Kind:    VarSymbol,
			Name:    s.Name,
			Mutable: s.Mutable,
			Type:    exprType,
		}
		return false

	case *ast.ReturnStmt:
		retType := a.analyzeExpr(s.Value)

		// NEW: Verify the returned type matches the function's expected type!
		if a.expectedReturn != nil && !a.expectedReturn.Equals(UnknownType{}) {
			if !retType.Equals(a.expectedReturn) && !retType.Equals(UnknownType{}) {
				a.errorf(s.Pos(), "type mismatch: expected return type '%s', got '%s'", a.expectedReturn.String(), retType.String())
			}
		}
		return true // Yes, this statement returns!

	case *ast.YieldStmt:
		a.analyzeExpr(s.Value)
		return false

	case *ast.ExprStmt:
		a.analyzeExpr(s.Value)
		return false
	}

	return false
}

func (a *Analyzer) analyzeExpr(expr ast.Expr) Type {
	var t Type = UnknownType{}

	switch e := expr.(type) {

	case *ast.IntLiteral:
		t = IntType{}

	case *ast.BoolLiteral:
		t = BoolType{}

	case *ast.Identifier:
		sym := a.resolve(e.Value)
		if sym == nil {
			a.errorf(e.Pos(), "undefined name '%s'", e.Value)
			t = UnknownType{}
		} else {
			t = sym.Type
		}

	case *ast.InfixExpr:
		left := a.analyzeExpr(e.Left)
		right := a.analyzeExpr(e.Right)

		if !left.Equals(UnknownType{}) && !right.Equals(UnknownType{}) && !left.Equals(right) {
			a.errorf(e.Pos(), "type mismatch: cannot %s '%s' and '%s'", e.Operator, left.String(), right.String())
		}
		t = left

	case *ast.IfExpr:
		cond := a.analyzeExpr(e.Condition)
		if !cond.Equals(BoolType{}) && !cond.Equals(UnknownType{}) {
			a.errorf(e.Pos(), "IF condition must be a boolean")
		}
		a.analyzeBlockStmt(e.Consequence)
		if e.Alternative != nil {
			a.analyzeBlockStmt(e.Alternative)
		}
		t = IntType{} // simplified for now

	case *ast.CallExpr:
		// 1. Evaluate whatever is being called
		fnTypeExpr := a.analyzeExpr(e.Function)

		// 2. Ensure it evaluates to a FunctionType
		fnType, ok := fnTypeExpr.(*FunctionType)
		if !ok {
			if !fnTypeExpr.Equals(UnknownType{}) {
				a.errorf(e.Pos(), "cannot call non-function type '%s'", fnTypeExpr.String())
			}
			t = UnknownType{}
		} else {
			// 3. Arity check
			if len(e.Arguments) != len(fnType.Params) {
				a.errorf(e.Pos(), "wrong number of arguments: expected %d, got %d", len(fnType.Params), len(e.Arguments))
				t = UnknownType{}
			} else {
				// 4. Typecheck arguments against the FunctionType signature
				for i, arg := range e.Arguments {
					argType := a.analyzeExpr(arg)
					paramType := fnType.Params[i]

					if !argType.Equals(paramType) && !argType.Equals(UnknownType{}) {
						a.errorf(e.Pos(), "argument %d type mismatch: expected '%s', got '%s'", i, paramType.String(), argType.String())
					}
				}
				t = fnType.Return
			}
		}

	case *ast.StructLiteral:
		// 1. Resolve the base struct type
		var structDef *StructType
		for s := a.scope; s != nil; s = s.parent {
			if custom, ok := s.types[e.Type.Value]; ok {
				if st, ok := custom.(*StructType); ok {
					structDef = st
					t = st
					break
				}
			}
		}

		if structDef == nil {
			a.errorf(e.Type.Pos(), "undefined struct type '%s'", e.Type.Value)
			return UnknownType{}
		}

		// 2. Track seen fields to catch duplicates
		seenFields := make(map[string]bool)

		for _, literalField := range e.Fields {
			fieldName := literalField.Name.Value

			// Check for duplicates in the literal: Point{x: 1, x: 2}
			if seenFields[fieldName] {
				a.errorf(literalField.Name.Pos(), "duplicate field '%s' in struct literal", fieldName)
				continue
			}
			seenFields[fieldName] = true

			// 3. Verify field exists in the definition
			fieldIdx := structDef.GetFieldIndex(fieldName)
			if fieldIdx < 0 {
				a.errorf(literalField.Name.Pos(), "struct '%s' has no field '%s'", structDef.Name, fieldName)
				continue
			}

			// 4. Type-check the value against the field definition
			expectedFieldType := structDef.Fields[fieldIdx].Type
			actualValType := a.analyzeExpr(literalField.Value)

			if !actualValType.Equals(expectedFieldType) && !actualValType.Equals(UnknownType{}) {
				a.errorf(literalField.Value.Pos(),
					"type mismatch for field '%s': expected '%s', got '%s'",
					fieldName, expectedFieldType.String(), actualValType.String())
			}
		}

		// 5. (Optional but recommended) Check for missing fields
		if len(seenFields) < len(structDef.Fields) {
			var missing []string
			for _, f := range structDef.Fields {
				if !seenFields[f.Name] {
					missing = append(missing, f.Name)
				}
			}
			// In many languages this is an error; in others, it uses defaults.
			// For MAML, let's make it a warning or error.
			a.errorf(e.Pos(), "missing fields in struct literal: %s", strings.Join(missing, ", "))
		}

	case *ast.FieldAccess:
		objType := a.analyzeExpr(e.Object)

		// If the object's type is already unknown, don't cascade errors
		if objType.Equals(UnknownType{}) {
			return UnknownType{}
		}

		st, ok := objType.(*StructType)
		if !ok {
			a.errorf(e.Object.Pos(), "cannot access field '%s' on non-struct type '%s'", e.Field.Value, objType.String())
			return UnknownType{}
		}

		idx := st.GetFieldIndex(e.Field.Value)
		if idx < 0 {
			a.errorf(e.Field.Pos(), "field '%s' does not exist on struct '%s'", e.Field.Value, st.Name)
			return UnknownType{}
		}

		t = st.Fields[idx].Type

	case *ast.StringLiteral:
		t = StringType{}

	default:
		a.errorf(expr.Pos(), "expression type not recognized: %T", expr)
		t = UnknownType{}
	}

	// NEW: Save the resolved type so Codegen can use it!
	a.TypeMap[expr] = t
	return t
}
