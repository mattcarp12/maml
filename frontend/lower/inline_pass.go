package lower

import (
	"github.com/mattcarp12/maml/frontend/ast"
)

// InlinePass replaces CallExprs with the cloned body of the target function.
type InlinePass struct {
	functions map[string]*ast.FnDecl
	callStack map[string]bool // Tracks active inlines to prevent infinite recursion
}

// NewInlinePass scans the parsed program to cache all available functions
func NewInlinePass(prog *ast.Program) *InlinePass {
	funcs := make(map[string]*ast.FnDecl)
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok {
			funcs[fn.Name] = fn
		}
	}
	return &InlinePass{
		functions: funcs,
		callStack: make(map[string]bool),
	}
}

func (p *InlinePass) Rewrite(node ast.Node) ast.Node {
	if call, ok := node.(*ast.CallExpr); ok {
		return p.inlineCall(call)
	}
	return node
}

func (p *InlinePass) inlineCall(call *ast.CallExpr) ast.Node {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return call // Only inline direct identifier calls, not closures/methods
	}

	targetFn, exists := p.functions[ident.Value]
	if !exists || targetFn.Body == nil {
		return call // External or built-in function
	}

	// Safety: Do not inline main, async functions, or recursive calls
	if ident.Value == "main" || targetFn.IsAsync || p.callStack[ident.Value] {
		return call
	}

	// Safety: Do not inline functions with early returns to preserve control flow semantics
	counter := &ReturnCountVisitor{}
	ast.Walk(counter, targetFn.Body)
	if counter.Count > 1 {
		return call
	}

	// Mark function as actively being inlined
	p.callStack[ident.Value] = true
	defer func() { p.callStack[ident.Value] = false }()

	// 1. Deep copy the function body to avoid AST aliasing
	clonedBody := targetFn.Body.Clone().(*ast.BlockStmt)

	// 2. Transform ReturnStmt -> YieldStmt
	retMutator := &ReturnToYieldPass{}
	rewrittenBody := ast.Rewrite(retMutator, clonedBody).(*ast.BlockStmt)

	// 3. Create the inline execution block
	inlineBlock := &ast.BlockStmt{
		Statements: []ast.Stmt{},
		Pos_:       call.Pos(),
		End_:       call.End(),
	}

	// 4. Inject parameters as local variables bound to the call arguments
	for i, param := range targetFn.Params {
		decl := &ast.DeclareStmt{
			Name:    param.Name,
			Mutable: param.Mut,
			Value:   call.Arguments[i].Argument, // Evaluate the passed argument
			Pos_:    call.Pos(),
		}
		inlineBlock.Statements = append(inlineBlock.Statements, decl)
	}

	// 5. Append the rewritten body
	inlineBlock.Statements = append(inlineBlock.Statements, rewrittenBody.Statements...)

	// Recursively rewrite to inline any nested calls inside the new block
	return ast.Rewrite(p, inlineBlock)
}

// ReturnCountVisitor safely checks for multiple return statements
type ReturnCountVisitor struct {
	Count int
}

func (v *ReturnCountVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if _, ok := node.(*ast.ReturnStmt); ok {
		v.Count++
	}
	return v
}

// ReturnToYieldPass converts explicit Returns into block Yields
type ReturnToYieldPass struct{}

func (r *ReturnToYieldPass) Rewrite(node ast.Node) ast.Node {
	if ret, ok := node.(*ast.ReturnStmt); ok {
		return &ast.YieldStmt{
			Value: ret.Value,
			Pos_:  ret.Pos(),
		}
	}
	return node
}
