package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
)

type Codegen struct {
	module *ir.Module

	// The current block of code we are writing LLVM instructions to
	currentBlock *ir.Block

	// Our "Symbol Table". It maps a MAML variable name (like "x")
	// to an LLVM memory pointer.
	env map[string]value.Value

	currentYield value.Value // Tracks the value of the most recent '=>' statement
}

func New() *Codegen {
	return &Codegen{
		module: ir.NewModule(),
		env:    make(map[string]value.Value),
	}
}

// Compile is the entry point. It takes the root AST node and walks the tree.
func (c *Codegen) Compile(node ast.Node) error {
	var err error
	switch n := node.(type) {
	case *ast.Program:
		for _, decl := range n.Decls {
			if err := c.Compile(decl); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		err = c.compileFnDecl(n)

	case *ast.BlockStmt:
		err = c.compileBlockStmt(n)

	case *ast.DeclareStmt:
		err = c.compileDeclareStmt(n)

	case *ast.ReturnStmt: // Renamed from ReturnStatement
		err = c.compileReturnStmt(n)

	case *ast.YieldStmt:
		err = c.compileYieldStmt(n)
	}

	return err
}

// String outputs the final LLVM IR text
func (c *Codegen) String() string {
	return c.module.String()
}
