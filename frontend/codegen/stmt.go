package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/frontend/ast"
)

// compileBlockStmt is the main entry point for compiling blocks.
// It returns the last yield value (used by IfExpr, etc.)
func (c *Codegen) compileBlockStmt(n *ast.BlockStmt) (value.Value, error) {
	c.pushEnv()
	defer c.popEnv()
	c.pushScope()
	defer c.popScope()

	var lastYield value.Value

	for _, stmt := range n.Statements {
		if err := c.visitStmt(stmt, &lastYield); err != nil {
			return nil, err
		}
	}

	return lastYield, nil
}

// visitStmt now accepts a pointer to lastYield so YieldStmt can update it
func (c *Codegen) visitStmt(stmt ast.Stmt, lastYield *value.Value) error {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *ast.DeclareStmt:
		return c.visitDeclareStmt(s)
	case *ast.AssignStmt:
		return c.visitAssignStmt(s)
	case *ast.ReturnStmt:
		return c.visitReturnStmt(s)
	case *ast.YieldStmt:
		return c.visitYieldStmt(s, lastYield)
	case *ast.ExprStmt:
		return c.visitExprStmt(s)
	case *ast.ForStmt:
		return c.visitForStmt(s)
	case *ast.BlockStmt:
		// Nested block: compile it but ignore its yield (only top-level yields matter for IfExpr etc.)
		_, err := c.compileBlockStmt(s)
		return err
	default:
		return fmt.Errorf("unsupported statement type: %T", stmt)
	}
}

// ===================================================================
// Individual Visit Methods
// ===================================================================

func (c *Codegen) visitDeclareStmt(n *ast.DeclareStmt) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}

	if valCG.IsAddress {
		c.setVar(n.Name, valCG.V, valCG.IsHeap)
	} else {
		llvmType := c.llvmTypeFor(valCG.Type)
		alloc := c.currentBlock.NewAlloca(llvmType)
		c.currentBlock.NewStore(valCG.V, alloc)
		c.setVar(n.Name, alloc, valCG.IsHeap)
	}

	// NEW: Only Retain and Track if it's ACTUALLY on the heap!
	c.trackDeepForRelease(valCG)

	return nil
}

func (c *Codegen) visitAssignStmt(n *ast.AssignStmt) error {
	rvalCG, err := c.evaluateExpression(n.RValue)
	if err != nil {
		return err
	}

	rval := c.load(rvalCG)

	lvalCG, err := c.evaluateExpression(n.LValue)
	if err != nil {
		return err
	}

	if !lvalCG.IsAddress {
		return fmt.Errorf("left-hand side of assignment must be an addressable variable")
	}

	c.currentBlock.NewStore(rval, lvalCG.V)

	c.trackDeepForRelease(rvalCG)

	return nil
}

func (c *Codegen) visitReturnStmt(s *ast.ReturnStmt) error {
	var valCG CGValue
	var err error

	// 1. Evaluate the expression the user is returning (e.g., 42)
	if s.Value != nil {
		valCG, err = c.evaluateExpression(s.Value)
		if err != nil {
			return err
		}
	}

	// 2. NEW: Check if we are inside an async function!
	// If __coro_hdl exists in the environment, we are inside a coroutine.
	if coroHdlInfo, err := c.resolveVar("__coro_hdl"); err {

		// (PHASE 2 NOTE: We will store valCG.V into the Coroutine Promise struct right here!)

		// 3. To satisfy LLVM, an async function must ALWAYS return its handle
		c.currentBlock.NewRet(coroHdlInfo.V)
		return nil
	}

	// 4. Standard synchronous function return
	if s.Value != nil {
		c.currentBlock.NewRet(c.load(valCG))
	} else {
		c.currentBlock.NewRet(nil) // Void return
	}

	return nil
}

func (c *Codegen) visitYieldStmt(n *ast.YieldStmt, lastYield *value.Value) error {
	valCG, err := c.evaluateExpression(n.Value)
	if err != nil {
		return err
	}
	*lastYield = c.load(valCG) // Update the last yield value
	return nil
}

func (c *Codegen) visitExprStmt(n *ast.ExprStmt) error {
	_, err := c.evaluateExpression(n.Value)
	return err
}

func (c *Codegen) visitForStmt(s *ast.ForStmt) error {
	// Init
	if s.Init != nil {
		switch initStmt := s.Init.(type) {
		case *ast.DeclareStmt:
			if err := c.visitDeclareStmt(initStmt); err != nil {
				return err
			}
		case *ast.AssignStmt:
			if err := c.visitAssignStmt(initStmt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported for-loop init statement type: %T", s.Init)
		}
	}

	fn := c.currentBlock.Parent
	condBlk := fn.NewBlock(c.newLabel("for_cond"))
	bodyBlk := fn.NewBlock(c.newLabel("for_body"))
	exitBlk := fn.NewBlock(c.newLabel("for_exit"))

	c.currentBlock.NewBr(condBlk)

	// Condition
	c.currentBlock = condBlk
	if s.Condition != nil {
		condCG, err := c.evaluateExpression(s.Condition)
		if err != nil {
			return err
		}
		c.currentBlock.NewCondBr(c.load(condCG), bodyBlk, exitBlk)
	} else {
		c.currentBlock.NewBr(bodyBlk)
	}

	// Body
	c.currentBlock = bodyBlk
	_, err := c.compileBlockStmt(s.Body)
	if err != nil {
		return err
	}

	// Post
	if s.Post != nil {
		switch postStmt := s.Post.(type) {
		case *ast.AssignStmt:
			if err := c.visitAssignStmt(postStmt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported for-loop post statement type: %T", s.Post)
		}
	}

	c.currentBlock.NewBr(condBlk)

	c.currentBlock = exitBlk
	return nil
}
