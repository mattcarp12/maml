package lower

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

// AsyncPass injects an explicit AsyncPrologueExpr at the beginning of
// any function marked as asynchronous.
type AsyncPass struct {
	typeMap map[ast.Node]sema.Type
}

func NewAsyncPass(typeMap map[ast.Node]sema.Type) *AsyncPass {
	return &AsyncPass{typeMap: typeMap}
}

func (p *AsyncPass) Rewrite(node ast.Node) ast.Node {
	// Intercept Function Declarations
	if fn, ok := node.(*ast.FnDecl); ok && fn.IsAsync {
		return p.lowerAsyncFn(fn)
	}
	return node
}

func (p *AsyncPass) lowerAsyncFn(fn *ast.FnDecl) ast.Node {
	if fn.Body == nil {
		return fn
	}

	// 1. Create the explicit prologue expression
	prologue := &ast.AsyncPrologueExpr{Pos_: fn.Pos()}

	// Assign the Task type to the prologue so the backend knows it resolves to a task handle
	p.typeMap[prologue] = &sema.TaskType{Base: p.typeMap[fn.ReturnType]}

	// 2. Wrap it in an expression statement
	prologueStmt := &ast.ExprStmt{
		Value: prologue,
		Pos_:  fn.Pos(),
	}

	// 3. Inject it as the very first statement in the function body
	// We allocate a new slice to avoid modifying the original underlying array
	newStmts := make([]ast.Stmt, 0, len(fn.Body.Statements)+1)
	newStmts = append(newStmts, prologueStmt)
	newStmts = append(newStmts, fn.Body.Statements...)

	fn.Body.Statements = newStmts

	return fn
}
