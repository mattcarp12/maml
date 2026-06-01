// frontend/desugar/desugar.go
package desugar

import (
	"github.com/mattcarp12/maml/frontend/tast"
)

// Desugar performs desugaring transformations on the Typed AST (TAST).
// All transformations mutate nodes in place where possible and return
// replacement nodes for expressions/statements that need structural changes.
type Desugar struct{}

// New creates a new desugarer.
func New() *Desugar {
	return &Desugar{}
}

// DesugarProgram is the entry point. It desugars every function body in the program.
func (d *Desugar) DesugarProgram(prog *tast.Program) {
	if prog == nil {
		return
	}
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*tast.FnDecl); ok {
			if fn.Body != nil {
				fn.Body = d.desugarBlockStmt(fn.Body)
			}
		}
	}
}

// desugarBlockStmt desugars a block and all nested statements.
func (d *Desugar) desugarBlockStmt(block *tast.BlockStmt) *tast.BlockStmt {
	if block == nil {
		return nil
	}

	var newStmts []tast.Stmt
	for _, stmt := range block.Statements {
		newStmt := d.desugarStmt(stmt)
		if newStmt != nil {
			newStmts = append(newStmts, newStmt)
		}
	}

	block.Statements = newStmts
	return block
}
