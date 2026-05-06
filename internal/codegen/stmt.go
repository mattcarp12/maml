package codegen

import (
	"github.com/llir/llvm/ir/types"
	"github.com/mattcarp12/maml/internal/ast"
)

// Declare, Return, Yield, Block

func (c *Codegen) compileDeclareStmt(n *ast.DeclareStmt) error {
	val, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}

	// 1. Ask the value what LLVM type it is
	valType := val.Type()

	// 2. If it's ALREADY a pointer (like the struct memory allocated by StructLiteral),
	// we don't need to allocate new memory. Just point the variable to it!
	if _, isPtr := valType.(*types.PointerType); isPtr {
		c.env[n.Name] = val
		return nil
	}

	// 3. If it's a primitive (like an i32 from a math equation), allocate space and store it.
	alloc := c.currentBlock.NewAlloca(valType)
	c.currentBlock.NewStore(val, alloc)
	c.env[n.Name] = alloc
	return nil
}

func (c *Codegen) compileReturnStmt(n *ast.ReturnStmt) error {
	val, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}
	c.currentBlock.NewRet(val)
	return nil
}

func (c *Codegen) compileYieldStmt(n *ast.YieldStmt) error {
	val, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}
	// Save the yielded value into compiler state so the IfExpr can grab it
	c.currentYield = val
	return nil
}

func (c *Codegen) compileBlockStmt(n *ast.BlockStmt) error {
	for _, stmt := range n.Statements {
		if err := c.Compile(stmt); err != nil {
			return err
		}
	}
	return nil
}
