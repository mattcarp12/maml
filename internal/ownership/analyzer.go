package ownership

import (
	"fmt"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

type Analyzer struct {
	scope         *Scope
	typeMap       map[ast.Node]sema.Type
	errors        []ast.CompileError
	aliasMap      map[string]string
	aliasRefCount map[string]int
}

func New(typeMap map[ast.Node]sema.Type) *Analyzer {
	return &Analyzer{
		scope:         NewScope(nil),
		typeMap:       typeMap,
		errors:        []ast.CompileError{},
		aliasMap:      make(map[string]string),
		aliasRefCount: make(map[string]int),
	}
}

func (a *Analyzer) Analyze(program *ast.Program) []ast.CompileError {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.analyzeFunction(fn)
		}
	}
	return a.errors
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Ownership",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func (a *Analyzer) pushScope() {
	a.scope = NewScope(a.scope)
}

func (a *Analyzer) popScope() {
	if a.scope == nil || a.scope.Parent == nil {
		return
	}
	for name := range a.scope.Bindings {
		a.removeAlias(name)
	}
	a.scope = a.scope.Parent
}

// Statement Traversal Pipeline

func (a *Analyzer) analyzeBlock(block *ast.BlockStmt) {
	a.pushScope()
	defer a.popScope()
	for _, stmt := range block.Statements {
		a.analyzeStmt(stmt)
	}
}

func (a *Analyzer) analyzeStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		a.analyzeDeclareStmt(s)
	case *ast.AssignStmt:
		a.analyzeAssignStmt(s)
	case *ast.ReturnStmt:
		a.analyzeExpr(s.Value)
	case *ast.YieldStmt:
		a.analyzeExpr(s.Value)
	case *ast.ExprStmt:
		a.analyzeExpr(s.Value)
	case *ast.ForStmt:
		a.analyzeForStmt(s)
	}
}

func (a *Analyzer) analyzeDeclareStmt(s *ast.DeclareStmt) {
	a.analyzeExpr(s.Value)

	if s.Mutable {
		if ident, ok := s.Value.(*ast.Identifier); ok {
			if src, exists := a.scope.Lookup(ident.Value); exists {
				if src.Invalidated {
					a.errorf(s.Pos(), "use of moved variable '%s'", ident.Value)
					return
				}
				if src.Symbol.Kind != sema.VariantSymbol {
					if !src.Symbol.Mutable {
						a.errorf(s.Pos(),
							"cannot acquire mutable ownership of immutable variable '%s': use %s.clone()",
							ident.Value, ident.Value)
						return
					}
					if a.hasActiveAliases(ident.Value) {
						a.errorf(s.Pos(),
							"cannot acquire mutable ownership of '%s': it has active immutable aliases",
							ident.Value)
						return
					}
					src.Invalidated = true
					a.removeAlias(ident.Value)
				}
			}
		}
	} else {
		if ident, ok := s.Value.(*ast.Identifier); ok {
			if src, exists := a.scope.Lookup(ident.Value); exists && !src.Invalidated {
				a.recordAlias(s.Name, ident.Value)
			}
		}
	}

	// Fetch the cached semantic type from the type map to build the local binding state
	var semaType sema.Type = sema.UnknownType{}
	if t, ok := a.typeMap[s.Value]; ok {
		semaType = t
	}

	a.scope.Bindings[s.Name] = &BindingState{
		Symbol: &sema.Symbol{
			Kind:    sema.VarSymbol,
			Name:    s.Name,
			Mutable: s.Mutable,
			Type:    semaType,
		},
		Invalidated: false,
	}
}

func (a *Analyzer) analyzeAssignStmt(s *ast.AssignStmt) {
	a.analyzeExpr(s.LValue)
	a.analyzeExpr(s.RValue)

	if indexExpr, ok := s.LValue.(*ast.IndexExpr); ok {
		if leftType, exists := a.typeMap[indexExpr.Left]; exists {
			if _, isString := leftType.(sema.StringType); isString {
				a.errorf(s.Pos(), "strings are immutable and cannot be modified by index")
				return
			}
		}
	}

	rootIdent := a.getRootIdentifier(s.LValue)
	if rootIdent != nil {
		if state, exists := a.scope.Lookup(rootIdent.Value); exists {
			if state.Invalidated {
				a.errorf(s.Pos(), "use of moved variable '%s'", rootIdent.Value)
				return
			}
			if !state.Symbol.Mutable {
				a.errorf(s.Pos(), "cannot mutate immutable variable '%s'", rootIdent.Value)
			}
		}
	} else {
		a.errorf(s.Pos(), "cannot assign to non-variable expression")
	}
}

func (a *Analyzer) analyzeForStmt(s *ast.ForStmt) {
	a.pushScope()
	defer a.popScope()

	if s.Init != nil {
		a.analyzeStmt(s.Init)
	}
	if s.Condition != nil {
		a.analyzeExpr(s.Condition)
	}
	if s.Post != nil {
		a.analyzeStmt(s.Post)
	}
	if s.Body != nil {
		// Re-use core loop parsing block
		for _, stmt := range s.Body.Statements {
			a.analyzeStmt(stmt)
		}
	}
}

// Expression Traversal Pipeline

func (a *Analyzer) analyzeExpr(expr ast.Expr) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.Identifier:
		if state, exists := a.scope.Lookup(e.Value); exists {
			if state.Invalidated {
				a.errorf(e.Pos(), "use of moved variable '%s'", e.Value)
			}
		}
	case *ast.InfixExpr:
		a.analyzeExpr(e.Left)
		a.analyzeExpr(e.Right)
	case *ast.PrefixExpr:
		a.analyzeExpr(e.Right)
	case *ast.IfExpr:
		a.analyzeExpr(e.Condition)
		a.analyzeBlock(e.Consequence)
		if e.Alternative != nil {
			a.analyzeBlock(e.Alternative)
		}
	case *ast.MatchExpr:
		a.analyzeMatchExpr(e)
	case *ast.CallExpr:
		a.analyzeCallExpr(e)
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			a.analyzeExpr(f.Value)
		}
	case *ast.FieldAccess:
		a.analyzeExpr(e.Object)
	case *ast.ArrayLiteral:
		for _, elem := range e.Elements {
			a.analyzeExpr(elem)
		}
	case *ast.IndexExpr:
		a.analyzeExpr(e.Left)
		a.analyzeExpr(e.Index)
	case *ast.SliceExpr:
		a.analyzeExpr(e.Left)
		if e.Low != nil {
			a.analyzeExpr(e.Low)
		}
		if e.High != nil {
			a.analyzeExpr(e.High)
		}
	}
}

func (a *Analyzer) analyzeCallExpr(e *ast.CallExpr) {
	a.analyzeExpr(e.Function)

	// Pull cached function type to run linear parameter borrow checking
	fnType, ok := a.typeMap[e.Function].(*sema.FunctionType)
	if !ok {
		// Evaluate argument sub-expressions anyway even if type is unknown to catch cascade bugs
		for _, arg := range e.Arguments {
			a.analyzeExpr(arg.Argument)
		}
		return
	}

	for i, arg := range e.Arguments {
		a.analyzeExpr(arg.Argument)
		if i >= len(fnType.ParamModes) {
			continue
		}

		paramMode := fnType.ParamModes[i]
		switch paramMode {
		case sema.ParamBorrow:
			if arg.Mut {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter is an immutable borrow, remove 'mut' at call site", i)
				continue
			}
			if arg.Own {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter is an immutable borrow, remove 'own' at call site", i)
				continue
			}
			if ident, ok := arg.Argument.(*ast.Identifier); ok {
				if src, exists := a.scope.Lookup(ident.Value); exists && src.Invalidated {
					a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
				}
			}

		case sema.ParamMutBorrow:
			if arg.Own {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter is a mutable borrow, use 'mut' not 'own' at call site", i)
				continue
			}
			if !arg.Mut {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter is declared 'mut', call site must pass with 'mut'", i)
				continue
			}
			ident, ok := arg.Argument.(*ast.Identifier)
			if !ok {
				a.errorf(arg.Argument.Pos(), "argument %d: 'mut' can only be applied to a named variable", i)
				continue
			}
			if src, exists := a.scope.Lookup(ident.Value); exists {
				if src.Invalidated {
					a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
					continue
				}
				if !src.Symbol.Mutable {
					a.errorf(arg.Argument.Pos(), "argument %d: cannot mutably borrow immutable variable '%s'", i, ident.Value)
					continue
				}
			}

		case sema.ParamOwned:
			if arg.Mut {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter requires ownership transfer, use 'own' not 'mut' at call site", i)
				continue
			}
			if !arg.Own {
				a.errorf(arg.Argument.Pos(), "argument %d: parameter is declared 'own', call site must pass with 'own'", i)
				continue
			}
			ident, ok := arg.Argument.(*ast.Identifier)
			if !ok {
				a.errorf(arg.Argument.Pos(), "argument %d: 'own' can only transfer ownership of a named variable", i)
				continue
			}
			if src, exists := a.scope.Lookup(ident.Value); exists {
				if src.Invalidated {
					a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
					continue
				}
				if a.hasActiveAliases(ident.Value) {
					a.errorf(arg.Argument.Pos(), "argument %d: cannot transfer ownership of '%s': value has active immutable aliases", i, ident.Value)
					continue
				}
				src.Invalidated = true
				a.removeAlias(ident.Value)
			}
		}
	}
}

func (a *Analyzer) analyzeMatchExpr(e *ast.MatchExpr) {
	a.analyzeExpr(e.Subject)
	for _, arm := range e.Arms {
		a.pushScope()
		// Re-inject pattern bindings into match-arm execution context branch
		a.injectPatternBindings(arm.Pattern)
		for _, stmt := range arm.Body.Statements {
			a.analyzeStmt(stmt)
		}
		a.popScope()
	}
}

func (a *Analyzer) injectPatternBindings(pat ast.Pattern) {
	vp, ok := pat.(*ast.VariantPattern)
	if !ok {
		return
	}

	// Single binding match syntax
	if vp.Binding != nil {
		a.scope.Bindings[vp.Binding.Value] = &BindingState{
			Symbol: &sema.Symbol{
				Kind:    sema.VarSymbol,
				Name:    vp.Binding.Value,
				Mutable: false,
				Type:    sema.UnknownType{},
			},
			Invalidated: false,
		}
		return
	}

	// Structural multi-field destructured variable definitions
	for _, fb := range vp.Fields {
		if fb.Binding != nil {
			a.scope.Bindings[fb.Binding.Value] = &BindingState{
				Symbol: &sema.Symbol{
					Kind:    sema.VarSymbol,
					Name:    fb.Binding.Value,
					Mutable: false,
					Type:    sema.UnknownType{},
				},
				Invalidated: false,
			}
		}
	}
}

// Global Tracking Utilities

func (a *Analyzer) analyzeFunction(fn *ast.FnDecl) {
	a.pushScope()
	defer a.popScope()

	for _, param := range fn.Params {
		mutable := param.Own || param.Mut
		var mode sema.ParamMode
		switch {
		case param.Own:
			mode = sema.ParamOwned
		case param.Mut:
			mode = sema.ParamMutBorrow
		default:
			mode = sema.ParamBorrow
		}

		var paramType sema.Type = sema.UnknownType{}
		if t, ok := a.typeMap[param.Type]; ok {
			paramType = t
		}

		a.scope.Bindings[param.Name] = &BindingState{
			Symbol: &sema.Symbol{
				Kind:      sema.VarSymbol,
				Name:      param.Name,
				Mutable:   mutable,
				Type:      paramType,
				ParamMode: mode,
			},
			Invalidated: false,
		}
	}

	if fn.Body != nil {
		for _, stmt := range fn.Body.Statements {
			a.analyzeStmt(stmt)
		}
	}
}

func (a *Analyzer) getRootIdentifier(expr ast.Expr) *ast.Identifier {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e
	case *ast.IndexExpr:
		return a.getRootIdentifier(e.Left)
	case *ast.SliceExpr:
		return a.getRootIdentifier(e.Left)
	case *ast.FieldAccess:
		return a.getRootIdentifier(e.Object)
	default:
		return nil
	}
}

func (a *Analyzer) canonicalName(name string) string {
	for {
		src, ok := a.aliasMap[name]
		if !ok {
			return name
		}
		name = src
	}
}

func (a *Analyzer) recordAlias(alias, source string) {
	canon := a.canonicalName(source)
	a.aliasMap[alias] = canon
	if _, ok := a.aliasRefCount[canon]; !ok {
		a.aliasRefCount[canon] = 1
	}
	a.aliasRefCount[canon]++
}

func (a *Analyzer) removeAlias(name string) {
	canon, isAlias := a.aliasMap[name]
	if isAlias {
		a.aliasRefCount[canon]--
		delete(a.aliasMap, name)
	} else {
		if count, ok := a.aliasRefCount[name]; ok {
			if count <= 1 {
				delete(a.aliasRefCount, name)
			} else {
				a.aliasRefCount[name] = count - 1
			}
		}
	}
}

func (a *Analyzer) hasActiveAliases(name string) bool {
	canon := a.canonicalName(name)
	return a.aliasRefCount[canon] > 1
}