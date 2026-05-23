package ownership

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

type Analyzer struct {
	scope           *Scope
	typeMap         map[ast.Node]sema.Type
	errors          []ast.CompileError
	aliasMap        map[string]string
	aliasRefCount   map[string]int
	suspensionCount int
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

// --- THE VISITOR IMPL ---
func (a *Analyzer) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.BlockStmt:
		a.pushScope()
		defer a.popScope()
		for _, stmt := range n.Statements {
			ast.Walk(a, stmt)
		}
		return nil

	case *ast.ForStmt:
		a.pushScope()
		defer a.popScope()
		if n.Init != nil {
			ast.Walk(a, n.Init)
		}
		if n.Condition != nil {
			ast.Walk(a, n.Condition)
		}
		if n.Post != nil {
			ast.Walk(a, n.Post)
		}
		if n.Body != nil {
			for _, stmt := range n.Body.Statements {
				ast.Walk(a, stmt)
			}
		}
		return nil

	case *ast.MatchExpr:
		ast.Walk(a, n.Subject)
		for _, arm := range n.Arms {
			a.pushScope()
			a.injectPatternBindings(arm.Pattern)
			for _, stmt := range arm.Body.(*ast.BlockStmt).Statements {
				ast.Walk(a, stmt)
			}
			a.popScope()
		}
		return nil

	case *ast.DeclareStmt:
		ast.Walk(a, n.Value) // Evaluate RHS first

		if n.Mutable {
			if ident, ok := n.Value.(*ast.Identifier); ok {
				if src, exists := a.scope.Lookup(ident.Value); exists {
					if src.Invalidated {
						a.errorf(n.Pos(), "use of moved variable '%s'", ident.Value)
						return nil
					}
					if src.Symbol.Kind != sema.VariantSymbol {
						if !src.Symbol.Mutable {
							a.errorf(n.Pos(), "cannot acquire mutable ownership of immutable variable '%s'", ident.Value)
							return nil
						}
						if a.hasActiveAliases(ident.Value) {
							a.errorf(n.Pos(), "cannot acquire mutable ownership of '%s': it has active immutable aliases", ident.Value)
							return nil
						}
						src.Invalidated = true
						a.removeAlias(ident.Value)
					}
				}
			}
		} else {
			if ident, ok := n.Value.(*ast.Identifier); ok {
				if src, exists := a.scope.Lookup(ident.Value); exists && !src.Invalidated {
					a.recordAlias(n.Name, ident.Value)
				}
			}
		}

		var semaType sema.Type = sema.UnknownType{}
		if t, ok := a.typeMap[n.Value]; ok {
			semaType = t
		}

		a.scope.Bindings[n.Name] = &BindingState{
			Symbol:               &sema.Symbol{Kind: sema.VarSymbol, Name: n.Name, Mutable: n.Mutable, Type: semaType},
			DeclaredAtSuspension: a.suspensionCount,
		}
		return nil

	case *ast.AssignStmt:
		ast.Walk(a, n.LValue)
		ast.Walk(a, n.RValue)

		if indexExpr, ok := n.LValue.(*ast.IndexExpr); ok {
			if leftType, exists := a.typeMap[indexExpr.Left]; exists {
				if _, isString := leftType.(sema.StringType); isString {
					a.errorf(n.Pos(), "strings are immutable and cannot be modified by index")
					return nil
				}
			}
		}

		rootIdent := a.getRootIdentifier(n.LValue)
		if rootIdent != nil {
			if state, exists := a.scope.Lookup(rootIdent.Value); exists {
				if state.Invalidated {
					a.errorf(n.Pos(), "use of moved variable '%s'", rootIdent.Value)
				} else if !state.Symbol.Mutable {
					a.errorf(n.Pos(), "cannot mutate immutable variable '%s'", rootIdent.Value)
				}
			}
		} else {
			a.errorf(n.Pos(), "cannot assign to non-variable expression")
		}
		return nil

	case *ast.CallExpr:
		ast.Walk(a, n.Function)
		fnType, ok := a.typeMap[n.Function].(*sema.FunctionType)

		for i, arg := range n.Arguments {
			ast.Walk(a, arg.Argument)
			if !ok || i >= len(fnType.ParamModes) {
				continue
			}

			paramMode := fnType.ParamModes[i]
			switch paramMode {
			case sema.ParamBorrow:
				if arg.Mut || arg.Own {
					a.errorf(arg.Argument.Pos(), "argument %d: parameter is an immutable borrow, remove modifiers", i)
					continue
				}
				if ident, ok := arg.Argument.(*ast.Identifier); ok {
					if src, exists := a.scope.Lookup(ident.Value); exists && src.Invalidated {
						a.errorf(arg.Argument.Pos(), "use of moved variable '%s'", ident.Value)
					}
				}

			case sema.ParamMutBorrow:
				if !arg.Mut || arg.Own {
					a.errorf(arg.Argument.Pos(), "argument %d: parameter is a mutable borrow, use 'mut'", i)
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
					} else if !src.Symbol.Mutable {
						a.errorf(arg.Argument.Pos(), "argument %d: cannot mutably borrow immutable variable '%s'", i, ident.Value)
					}
				}

			case sema.ParamOwned:
				if !arg.Own || arg.Mut {
					a.errorf(arg.Argument.Pos(), "argument %d: parameter requires ownership transfer, use 'own'", i)
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
					} else if a.hasActiveAliases(ident.Value) {
						a.errorf(arg.Argument.Pos(), "argument %d: cannot transfer ownership of '%s': value has active aliases", i, ident.Value)
					} else {
						src.Invalidated = true
						a.removeAlias(ident.Value)
					}
				}
			}
		}
		return nil

	case *ast.AwaitExpr:
		a.suspensionCount++ // We crossed a suspension boundary!
		ast.Walk(a, n.Value)
		return nil

	case *ast.Identifier:
		if state, exists := a.scope.Lookup(n.Value); exists {
			if state.Invalidated {
				a.errorf(n.Pos(), "use of moved variable '%s'", n.Value)
			}

			// NEW: Cross-Await Borrow Check
			if a.suspensionCount > state.DeclaredAtSuspension {
				if state.Symbol.ParamMode == sema.ParamBorrow || state.Symbol.ParamMode == sema.ParamMutBorrow {
					a.errorf(n.Pos(), "borrowed parameter '%s' cannot survive across an await suspension point (must be 'own' or copied)", n.Value)
				}
			}
		}
		return nil
	}

	return a // Default: let ast.Walk traverse children
}

func (a *Analyzer) analyzeFunction(fn *ast.FnDecl) {
	a.pushScope()
	defer a.popScope()
	a.suspensionCount = 0

	for _, param := range fn.Params {
		mutable := param.Own || param.Mut
		mode := sema.ParamBorrow
		if param.Own {
			mode = sema.ParamOwned
		} else if param.Mut {
			mode = sema.ParamMutBorrow
		}

		var paramType sema.Type = sema.UnknownType{}
		if t, ok := a.typeMap[param.Type]; ok {
			paramType = t
		}

		a.scope.Bindings[param.Name] = &BindingState{
			Symbol: &sema.Symbol{Kind: sema.VarSymbol, Name: param.Name, Mutable: mutable, Type: paramType, ParamMode: mode},
		}
	}

	if fn.Body != nil {
		for _, stmt := range fn.Body.Statements {
			ast.Walk(a, stmt)
		}
	}
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{Stage: "Ownership", Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func (a *Analyzer) pushScope() { a.scope = NewScope(a.scope) }

func (a *Analyzer) popScope() {
	if a.scope == nil || a.scope.Parent == nil {
		return
	}
	for name := range a.scope.Bindings {
		a.removeAlias(name)
	}
	a.scope = a.scope.Parent
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
