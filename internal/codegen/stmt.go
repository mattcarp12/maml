package codegen

import (
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
)

func (c *Codegen) compileDeclareStmt(n *ast.DeclareStmt) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}

	val := c.load(valCG)
	valType := val.Type()
	if _, isPtr := valType.(*types.PointerType); isPtr {
		c.setVar(n.Name, val) // FIX: Use new Env method
		return nil
	}

	alloc := c.currentBlock.NewAlloca(valType)
	c.currentBlock.NewStore(val, alloc)
	c.setVar(n.Name, alloc) // FIX: Use new Env method
	return nil
}

func (c *Codegen) compileReturnStmt(n *ast.ReturnStmt) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil { return err }
	
	c.currentBlock.NewRet(c.load(valCG)) // Load it before returning
	return nil
}

// FIX: Blocks now return (value.Value, error) and push/pop their own scope!
func (c *Codegen) compileBlockStmt(n *ast.BlockStmt) (value.Value, error) {
	c.pushEnv()
	defer c.popEnv()

	var lastYield value.Value
	for _, stmt := range n.Statements {
		switch s := stmt.(type) {
		case *ast.DeclareStmt:
			if err := c.compileDeclareStmt(s); err != nil {
				return nil, err
			}
		case *ast.ReturnStmt:
			if err := c.compileReturnStmt(s); err != nil {
				return nil, err
			}
		case *ast.YieldStmt:
			// Handle yields directly in the block instead of calling a separate function
			valCG, err := c.evaluateExpression(s.Value)
			if err != nil {
				return nil, err
			}
			lastYield = c.load(valCG)
		case *ast.ExprStmt:
			if err := c.compileExprStmt(s); err != nil {
				return nil, err
			}
		}
	}
	return lastYield, nil
}

func (c *Codegen) compileExprStmt(n *ast.ExprStmt) error {
	_, err := c.evaluateExpression(n.Value)
	return err
}
