package lower

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

// ARCPass injects explicit RetainStmt and ReleaseStmt nodes into the AST
// based on the type information resolved by the Semantic Analyzer.
type ARCPass struct {
	typeMap map[ast.Node]sema.Type
}

func NewARCPass(typeMap map[ast.Node]sema.Type) *ARCPass {
	return &ARCPass{typeMap: typeMap}
}

func (p *ARCPass) Rewrite(node ast.Node) ast.Node {
	switch n := node.(type) {
	case *ast.AssignStmt:
		return p.lowerAssign(n)
	case *ast.BlockStmt:
		return p.lowerBlock(n)
	}
	return node
}

func (p *ARCPass) lowerAssign(assign *ast.AssignStmt) ast.Node {
	t := p.typeMap[assign.RValue]
	// If the type is unknown or not a reference type, do nothing.
	if t == nil || !t.IsReferenceType() {
		return assign
	}

	// For reference types, assignment means replacing an old reference with a new one.
	// Rewrite: x = y  =>  { release(x); x = y; retain(x); }
	block := &ast.BlockStmt{
		Statements: []ast.Stmt{
			&ast.ReleaseStmt{Value: assign.LValue, Pos_: assign.Pos()},
			assign,
			&ast.RetainStmt{Value: assign.LValue, Pos_: assign.Pos()},
		},
		Pos_: assign.Pos(),
		End_: assign.End(),
	}

	// Ensure the backend still knows the type of this new block
	p.typeMap[block] = t

	return block
}

func (p *ARCPass) lowerBlock(block *ast.BlockStmt) ast.Node {
	var releases []ast.Stmt
	var newStmts []ast.Stmt

	// 1. Identify all reference-type variables declared in this block
	for _, stmt := range block.Statements {
		newStmts = append(newStmts, stmt)

		if decl, ok := stmt.(*ast.DeclareStmt); ok {
			t := p.typeMap[decl.Value]
			if t != nil && t.IsReferenceType() {
				// We need to release this variable at the end of the block
				ident := &ast.Identifier{Value: decl.Name, Pos_: decl.Pos()}
				p.typeMap[ident] = t
				releases = append(releases, &ast.ReleaseStmt{Value: ident, Pos_: decl.Pos()})
			}
		}
	}

	// 2. If we have nothing to release, leave the block as is
	if len(releases) == 0 {
		return block
	}

	// 3. Inject the releases right before the block exits
	lastIdx := len(newStmts) - 1
	lastStmt := newStmts[lastIdx]

	switch lastStmt.(type) {
	case *ast.ReturnStmt, *ast.YieldStmt, *ast.BreakStmt, *ast.ContinueStmt:
		// If the block ends in a terminator, we MUST insert the releases BEFORE it.
		finalStmts := append([]ast.Stmt{}, newStmts[:lastIdx]...)
		finalStmts = append(finalStmts, releases...)
		finalStmts = append(finalStmts, lastStmt)
		block.Statements = finalStmts
	default:
		// Otherwise, just append the releases to the very end of the block.
		block.Statements = append(newStmts, releases...)
	}

	return block
}
