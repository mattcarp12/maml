package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
)

func (c *Codegen) compileDeclareStmt(n *ast.DeclareStmt) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}

	if valCG.IsAddress {
		// If it's already an address, we can just store it directly
		c.setVar(n.Name, valCG.V)
		return nil
	}

	llvmType := c.llvmTypeFor(valCG.Type)
	alloc := c.currentBlock.NewAlloca(llvmType)
	c.currentBlock.NewStore(valCG.V, alloc)
	c.setVar(n.Name, alloc)
	return nil
}

func (c *Codegen) compileAssignStmt(n *ast.AssignStmt) error {
	rvalCG, err := c.evaluateExpression(n.RValue)
	if err != nil {
		return err
	}

	rval := c.load(rvalCG)

	lvalVal, err := c.evaluateExpression(n.LValue)
	if err != nil {
		return err
	}

	if !lvalVal.IsAddress {
		return fmt.Errorf("left-hand side of assignment must be an addressable variable")
	}

	c.currentBlock.NewStore(rval, lvalVal.V)
	return nil

}

func (c *Codegen) compileReturnStmt(n *ast.ReturnStmt) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}

	c.currentBlock.NewRet(c.load(valCG)) // Load it before returning
	return nil
}

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
			valCG, err := c.evaluateExpression(s.Value)
			if err != nil {
				return nil, err
			}
			lastYield = c.load(valCG)
		case *ast.ExprStmt:
			if err := c.compileExprStmt(s); err != nil {
				return nil, err
			}
		case *ast.AssignStmt:
			if err := c.compileAssignStmt(s); err != nil {
				return nil, err
			}
		case *ast.ForStmt:
			if err := c.compileForStmt(s); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported statement type: %T", stmt)
		}
	}
	return lastYield, nil
}

func (c *Codegen) compileExprStmt(n *ast.ExprStmt) error {
	_, err := c.evaluateExpression(n.Value)
	return err
}

func (c *Codegen) compileForStmt(s *ast.ForStmt) error {
    // 1. Run Init (if it exists) in the current block
    if s.Init != nil {
        // You'll need to call your statement dispatcher here
        // e.g., c.compileDeclareStmt or c.compileAssignStmt
		switch initStmt := s.Init.(type) {
		case *ast.DeclareStmt:
			if err := c.compileDeclareStmt(initStmt); err != nil {
				return err
			}
		case *ast.AssignStmt:
			if err := c.compileAssignStmt(initStmt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported for-loop init statement type: %T", s.Init)
		}
    }

    // 2. Create the LLVM blocks
    fn := c.currentBlock.Parent
    condBlk := fn.NewBlock(c.newLabel("for_cond"))
    bodyBlk := fn.NewBlock(c.newLabel("for_body"))
    exitBlk := fn.NewBlock(c.newLabel("for_exit"))

    // Jump into the condition block
    c.currentBlock.NewBr(condBlk)

    // --- 3. CONDITION BLOCK ---
    c.currentBlock = condBlk
    if s.Condition != nil {
        condCG, err := c.evaluateExpression(s.Condition)
        if err != nil { return err }
        c.currentBlock.NewCondBr(c.load(condCG), bodyBlk, exitBlk)
    } else {
        // Infinite loop: unconditionally jump to body
        c.currentBlock.NewBr(bodyBlk)
    }

    // --- 4. BODY BLOCK ---
    c.currentBlock = bodyBlk
    _, err := c.compileBlockStmt(s.Body)
    if err != nil { return err }
    
    if s.Post != nil {
         // Execute post statement (e.g., assignment)
		switch postStmt := s.Post.(type) {
		case *ast.AssignStmt:
			if err := c.compileAssignStmt(postStmt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported for-loop post statement type: %T", s.Post)
		}
    }
    
    // Jump BACK to the top of the loop!
    c.currentBlock.NewBr(condBlk)

    // --- 5. EXIT BLOCK ---
    c.currentBlock = exitBlk
    return nil
}