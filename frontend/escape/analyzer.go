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
	EscapeMap   map[ast.Node]EscapeState
	typeMap     map[ast.Node]sema.Type
	env         *Scope
	blockScopes map[*ast.BlockStmt]*Scope // TRACKING FIX: Explicitly maps blocks to scopes
}

func New(typeMap map[ast.Node]sema.Type) *Analyzer {
	return &Analyzer{
		EscapeMap:   make(map[ast.Node]EscapeState),
		typeMap:     typeMap,
		blockScopes: make(map[*ast.BlockStmt]*Scope),
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
		if a.visitBlockStmt(fn.Body) {
			changed = true
		}
	}
}

func (a *Analyzer) buildEnvironment(block *ast.BlockStmt) {
	if block == nil {
		return
	}

	// Bind current scope to the block statement
	a.blockScopes[block] = a.env

	for _, stmt := range block.Statements {
		switch s := stmt.(type) {
		case *ast.DeclareStmt:
			a.env.vars[s.Name] = []ast.Node{a.stripToAllocation(s.Value)}
			a.discoverBlocks(s.Value) // Check for internal block expressions

		case *ast.AssignStmt:
			if ident := a.getBaseIdentifier(s.LValue); ident != nil {
				a.env.appendVar(ident.Value, a.stripToAllocation(s.RValue))
			}
			a.discoverBlocks(s.LValue)
			a.discoverBlocks(s.RValue)

		case *ast.LoopStmt: // LOWERING FIX: Support desugared loop constructs
			if s.Body != nil {
				a.env = newScope(a.env)
				a.buildEnvironment(s.Body)
				a.env = a.env.parent
			}

		case *ast.ForStmt: // Fallback safety layer
			if s.Init != nil {
				a.buildEnvironment(&ast.BlockStmt{Statements: []ast.Stmt{s.Init}})
			}
			if s.Body != nil {
				a.env = newScope(a.env)
				a.buildEnvironment(s.Body)
				a.env = a.env.parent
			}

		case *ast.ExprStmt:
			a.discoverBlocks(s.Value)

		case *ast.BlockStmt: // Handle nested base blocks
			a.env = newScope(a.env)
			a.buildEnvironment(s)
			a.env = a.env.parent

		case *ast.ReturnStmt:
			a.discoverBlocks(s.Value)

		case *ast.YieldStmt:
			a.discoverBlocks(s.Value)
		}
	}
}

// discoverBlocks recursively discovers embedded blocks inside nested expressions
func (a *Analyzer) discoverBlocks(expr ast.Expr) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.BlockStmt:
		a.env = newScope(a.env)
		a.buildEnvironment(e)
		a.env = a.env.parent
	case *ast.IfExpr:
		if e.Condition != nil {
			a.discoverBlocks(e.Condition)
		}
		if e.Consequence != nil {
			a.env = newScope(a.env)
			a.buildEnvironment(e.Consequence)
			a.env = a.env.parent
		}
		if e.Alternative != nil {
			a.env = newScope(a.env)
			a.buildEnvironment(e.Alternative)
			a.env = a.env.parent
		}
	case *ast.CallExpr:
		a.discoverBlocks(e.Function)
		for _, arg := range e.Arguments {
			a.discoverBlocks(arg.Argument)
		}
	case *ast.IndexExpr:
		a.discoverBlocks(e.Left)
		a.discoverBlocks(e.Index)
	case *ast.FieldAccess:
		a.discoverBlocks(e.Object)
	case *ast.SliceExpr:
		a.discoverBlocks(e.Left)
		if e.Low != nil {
			a.discoverBlocks(e.Low)
		}
		if e.High != nil {
			a.discoverBlocks(e.High)
		}
	case *ast.InfixExpr:
		a.discoverBlocks(e.Left)
		a.discoverBlocks(e.Right)
	case *ast.PrefixExpr:
		a.discoverBlocks(e.Right)
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			a.discoverBlocks(f.Value)
		}
	case *ast.ArrayLiteral:
		for _, elem := range e.Elements {
			a.discoverBlocks(elem)
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
	case *ast.BlockStmt: // LOWERING FIX: Trace past block structures to evaluate the yielded allocation target
		if len(e.Statements) > 0 {
			if yield, ok := e.Statements[len(e.Statements)-1].(*ast.YieldStmt); ok {
				return a.stripToAllocation(yield.Value)
			}
		}
		return e
	default:
		return e
	}
}

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
		if a.visitExpr(s.Value) {
			changed = true
		}
		retType := a.typeMap[s.Value]
		if a.containsReferenceType(retType) {
			if a.markEscapes(s.Value) {
				changed = true
			}
		}

	case *ast.YieldStmt: // LOWERING FIX: Support explicit escape evaluation for yield paths
		if a.visitExpr(s.Value) {
			changed = true
		}
		yieldType := a.typeMap[s.Value]
		if a.containsReferenceType(yieldType) {
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
			if a.visitBlockStmt(ifExpr.Consequence) {
				changed = true
			}
			if ifExpr.Alternative != nil {
				if a.visitBlockStmt(ifExpr.Alternative) {
					changed = true
				}
			}
		}

	case *ast.DeclareStmt:
		if a.visitExpr(s.Value) {
			changed = true
		}
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
		if a.visitExpr(s.LValue) {
			changed = true
		}
		if a.visitExpr(s.RValue) {
			changed = true
		}
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

	case *ast.LoopStmt: // LOWERING FIX: Recurse into desugared loop blocks
		if a.visitBlockStmt(s.Body) {
			changed = true
		}

	case *ast.ForStmt: // Fallback safety layer
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

	case *ast.BlockStmt:
		if a.visitBlockStmt(s) {
			changed = true
		}
	}

	return changed
}

// visitExpr deeply runs escape analysis across blocks trapped inside expression hierarchies
func (a *Analyzer) visitExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	changed := false
	switch e := expr.(type) {
	case *ast.BlockStmt:
		if a.visitBlockStmt(e) {
			changed = true
		}
	case *ast.IfExpr:
		if e.Condition != nil && a.visitExpr(e.Condition) {
			changed = true
		}
		if e.Consequence != nil && a.visitBlockStmt(e.Consequence) {
			changed = true
		}
		if e.Alternative != nil && a.visitBlockStmt(e.Alternative) {
			changed = true
		}
	case *ast.CallExpr:
		if a.visitExpr(e.Function) {
			changed = true
		}
		for _, arg := range e.Arguments {
			if a.visitExpr(arg.Argument) {
				changed = true
			}
		}
	case *ast.IndexExpr:
		if a.visitExpr(e.Left) {
			changed = true
		}
		if a.visitExpr(e.Index) {
			changed = true
		}
	case *ast.FieldAccess:
		if a.visitExpr(e.Object) {
			changed = true
		}
	case *ast.SliceExpr:
		if a.visitExpr(e.Left) {
			changed = true
		}
		if e.Low != nil && a.visitExpr(e.Low) {
			changed = true
		}
		if e.High != nil && a.visitExpr(e.High) {
			changed = true
		}
	case *ast.InfixExpr:
		if a.visitExpr(e.Left) {
			changed = true
		}
		if a.visitExpr(e.Right) {
			changed = true
		}
	case *ast.PrefixExpr:
		if a.visitExpr(e.Right) {
			changed = true
		}
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			if a.visitExpr(f.Value) {
				changed = true
			}
		}
	case *ast.ArrayLiteral:
		for _, elem := range e.Elements {
			if a.visitExpr(elem) {
				changed = true
			}
		}
	}
	return changed
}

func (a *Analyzer) visitCallExpr(call *ast.CallExpr) bool {
	changed := false

	fnType, ok := a.typeMap[call.Function].(*sema.FunctionType)
	if !ok {
		return false
	}

	for i, arg := range call.Arguments {
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

	// SCOPE RE-ALIGNMENT: Track the correct environment context for this specific block
	oldEnv := a.env
	if scope, exists := a.blockScopes[block]; exists {
		a.env = scope
	}
	defer func() { a.env = oldEnv }()

	changed := false
	for _, stmt := range block.Statements {
		if a.visitStmt(stmt) {
			changed = true
		}
	}
	return changed
}
