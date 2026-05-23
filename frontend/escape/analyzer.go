package escape

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

type EscapeState int

const (
	StateStack EscapeState = iota
	StateHeap
)

type Scope struct {
	parent *Scope
	vars   map[string][]ast.Node
}

func newScope(parent *Scope) *Scope {
	return &Scope{
		parent: parent,
		vars:   make(map[string][]ast.Node),
	}
}

func (s *Scope) lookup(name string) []ast.Node {
	for curr := s; curr != nil; curr = curr.parent {
		if nodes, exists := curr.vars[name]; exists {
			return nodes
		}
	}
	return nil
}

func (s *Scope) appendVar(name string, node ast.Node) {
	for curr := s; curr != nil; curr = curr.parent {
		if _, exists := curr.vars[name]; exists {
			curr.vars[name] = append(curr.vars[name], node)
			return
		}
	}
	s.vars[name] = append(s.vars[name], node)
}

type Analyzer struct {
	EscapeMap map[ast.Node]EscapeState
	typeMap   map[ast.Node]sema.Type
	env       *Scope
}

func New(typeMap map[ast.Node]sema.Type) *Analyzer {
	return &Analyzer{
		EscapeMap: make(map[ast.Node]EscapeState),
		typeMap:   typeMap,
	}
}

func (a *Analyzer) Analyze(program *ast.Program) map[ast.Node]EscapeState {
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			a.analyzeFunction(fn)
		}
	}
	return a.EscapeMap
}

func (a *Analyzer) analyzeFunction(fn *ast.FnDecl) {
	a.env = newScope(nil)

	for _, param := range fn.Params {
		a.env.vars[param.Name] = []ast.Node{param}
	}

	a.buildEnvironment(fn.Body)

	changed := true
	for changed {
		changed = false
		// FIXED: Traverse the entire block recursively
		if a.visitBlockStmt(fn.Body) {
			changed = true
		}
	}
}

func (a *Analyzer) buildEnvironment(block *ast.BlockStmt) {
	for _, stmt := range block.Statements {
		switch s := stmt.(type) {
		case *ast.DeclareStmt:
			a.env.vars[s.Name] = []ast.Node{a.stripToAllocation(s.Value)}

		case *ast.AssignStmt:
			if ident := a.getBaseIdentifier(s.LValue); ident != nil {
				a.env.appendVar(ident.Value, a.stripToAllocation(s.RValue))
			}

		case *ast.ForStmt:
			if s.Init != nil {
				a.buildEnvironment(&ast.BlockStmt{Statements: []ast.Stmt{s.Init}})
			}
			if s.Body != nil {
				a.env = newScope(a.env)
				a.buildEnvironment(s.Body)
				a.env = a.env.parent
			}

		case *ast.ExprStmt:
			if ifExpr, ok := s.Value.(*ast.IfExpr); ok {
				a.env = newScope(a.env)
				a.buildEnvironment(ifExpr.Consequence)
				a.env = a.env.parent

				if ifExpr.Alternative != nil {
					a.env = newScope(a.env)
					a.buildEnvironment(ifExpr.Alternative)
					a.env = a.env.parent
				}
			}
		}
	}
}

func (a *Analyzer) getBaseIdentifier(expr ast.Expr) *ast.Identifier {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e
	case *ast.IndexExpr:
		return a.getBaseIdentifier(e.Left)
	case *ast.FieldAccess:
		return a.getBaseIdentifier(e.Object)
	case *ast.SliceExpr:
		return a.getBaseIdentifier(e.Left)
	}
	return nil
}

func (a *Analyzer) stripToAllocation(expr ast.Expr) ast.Node {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		return a.stripToAllocation(e.Left)
	case *ast.FieldAccess:
		return a.stripToAllocation(e.Object)
	case *ast.SliceExpr:
		return a.stripToAllocation(e.Left)
	default:
		return e
	}
}

// NEW HELPER: Recursively checks if a type is or contains a reference type
func (a *Analyzer) containsReferenceType(t sema.Type) bool {
	if t == nil {
		return false
	}
	if t.IsReferenceType() {
		return true
	}
	switch ty := t.(type) {
	case *sema.StructType:
		for _, f := range ty.Fields {
			if a.containsReferenceType(f.Type) {
				return true
			}
		}
	case sema.ArrayType:
		return a.containsReferenceType(ty.Base)
	}
	return false
}

func (a *Analyzer) visitStmt(stmt ast.Stmt) bool {
	changed := false

	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		retType := a.typeMap[s.Value]
		if a.containsReferenceType(retType) {
			if a.markEscapes(s.Value) {
				changed = true
			}
		}

	case *ast.ExprStmt:
		if call, ok := s.Value.(*ast.CallExpr); ok {
			if a.visitCallExpr(call) {
				changed = true
			}
		} else if ifExpr, ok := s.Value.(*ast.IfExpr); ok {
			// NEW: Recurse into if/else blocks!
			if a.visitBlockStmt(ifExpr.Consequence) {
				changed = true
			}
			if a.visitBlockStmt(ifExpr.Alternative) {
				changed = true
			}
		}

	case *ast.DeclareStmt:
		roots := a.getRootAllocations(s)
		for _, root := range roots {
			if a.EscapeMap[root] == StateHeap {
				valType := a.typeMap[s.Value]
				if a.containsReferenceType(valType) {
					if a.markEscapes(s.Value) {
						changed = true
					}
				}
			}
		}

	case *ast.AssignStmt:
		lvalRoots := a.getRootAllocations(s.LValue)
		rvalType := a.typeMap[s.RValue]

		if a.containsReferenceType(rvalType) {
			for _, root := range lvalRoots {
				if root != nil && a.EscapeMap[root] == StateHeap {
					if a.markEscapes(s.RValue) {
						changed = true
					}
					break
				}
			}
		}

	// NEW: Recurse into For loop blocks and their headers!
	case *ast.ForStmt:
		if s.Init != nil {
			if a.visitStmt(s.Init) {
				changed = true
			}
		}
		if a.visitBlockStmt(s.Body) {
			changed = true
		}
		if s.Post != nil {
			if a.visitStmt(s.Post) {
				changed = true
			}
		}

	// NEW: Support bare nested blocks
	case *ast.BlockStmt:
		if a.visitBlockStmt(s) {
			changed = true
		}
	}

	return changed
}

func (a *Analyzer) visitCallExpr(call *ast.CallExpr) bool {
	changed := false

	// Safely attempt the cast. Compiler built-ins (spawn, Some, run_executor, etc.)
	// won't have a standard FunctionType in the type map.
	fnType, ok := a.typeMap[call.Function].(*sema.FunctionType)
	if !ok {
		return false // It's a built-in, safe to skip!
	}

	for i, arg := range call.Arguments {
		// Bounds check just in case
		if i >= len(fnType.ParamModes) {
			continue
		}

		if fnType.ParamModes[i] == sema.ParamOwned {
			argType := a.typeMap[arg.Argument]
			if a.containsReferenceType(argType) {
				if a.markEscapes(arg.Argument) {
					changed = true
				}
			}
		}
	}
	return changed
}

func (a *Analyzer) markEscapes(node ast.Expr) bool {
	roots := a.getRootAllocations(node)
	if len(roots) == 0 {
		return false
	}

	changed := false
	for _, root := range roots {
		if root != nil && a.EscapeMap[root] != StateHeap {
			a.EscapeMap[root] = StateHeap
			changed = true

			// NEW: Cascade escapes to inner composite literal fields!
			// If a struct literal escapes, any slices we placed inside it must also escape.
			switch comp := root.(type) {
			case *ast.StructLiteral:
				for _, field := range comp.Fields {
					if a.containsReferenceType(a.typeMap[field.Value]) {
						if a.markEscapes(field.Value) {
							changed = true
						}
					}
				}
			case *ast.ArrayLiteral:
				for _, elem := range comp.Elements {
					if a.containsReferenceType(a.typeMap[elem]) {
						if a.markEscapes(elem) {
							changed = true
						}
					}
				}
			}
		}
	}
	return changed
}

func (a *Analyzer) getRootAllocations(expr ast.Node) []ast.Node {
	switch e := expr.(type) {
	case *ast.Identifier:
		origins := a.env.lookup(e.Value)
		var roots []ast.Node
		for _, origin := range origins {
			if originIdent, ok := origin.(*ast.Identifier); ok {
				roots = append(roots, a.getRootAllocations(originIdent)...)
			} else {
				roots = append(roots, origin)
			}
		}
		return roots

	case *ast.DeclareStmt, *ast.ArrayLiteral, *ast.StructLiteral, *ast.StringLiteral:
		return []ast.Node{e}

	case *ast.IndexExpr:
		return a.getRootAllocations(e.Left)
	case *ast.FieldAccess:
		return a.getRootAllocations(e.Object)
	case *ast.SliceExpr:
		return a.getRootAllocations(e.Left)
	}

	return nil
}

func (a *Analyzer) visitBlockStmt(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	changed := false
	for _, stmt := range block.Statements {
		if a.visitStmt(stmt) {
			changed = true
		}
	}
	return changed
}
