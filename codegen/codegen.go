package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/ast"
)

type Compiler struct {
	module *ir.Module

	// The current block of code we are writing LLVM instructions to
	currentBlock *ir.Block

	// Our "Symbol Table". It maps a MAML variable name (like "x")
	// to an LLVM memory pointer.
	env map[string]value.Value
}

func New() *Compiler {
	return &Compiler{
		module: ir.NewModule(),
		env:    make(map[string]value.Value),
	}
}

// Compile is the entry point. It takes the root AST node and walks the tree.
func (c *Compiler) Compile(node ast.Node) error {
	switch n := node.(type) {
	case *ast.Program:
		// Program now holds Decls, not Statements
		for _, decl := range n.Decls {
			if err := c.Compile(decl); err != nil {
				return err
			}
		}

	case *ast.FnDecl: // Renamed from FunctionDecl
		// 1. Create the LLVM function returning an i32 (integer)
		funcType := ir.NewFunc(n.Name, types.I32)
		c.module.Funcs = append(c.module.Funcs, funcType)

		// 2. Create the first "Basic Block" (the entry point of the function)
		c.currentBlock = funcType.NewBlock("entry")

		// 3. Compile the body of the function
		if err := c.Compile(n.Body); err != nil {
			return err
		}

	case *ast.BlockStmt: // Renamed from BlockStatement
		for _, stmt := range n.Statements {
			if err := c.Compile(stmt); err != nil {
				return err
			}
		}

	case *ast.DeclareStmt: // Renamed from DeclareStatement
		// Evaluate the right side
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}

		// Allocate memory for an i32
		alloc := c.currentBlock.NewAlloca(types.I32)

		// Store the value into that memory
		c.currentBlock.NewStore(val, alloc)

		// Save the memory pointer in our symbol table so we can find it later
		c.env[n.Name] = alloc

	case *ast.ReturnStmt: // Renamed from ReturnStatement
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}
		c.currentBlock.NewRet(val)
	}

	return nil
}

// String outputs the final LLVM IR text
func (c *Compiler) String() string {
	return c.module.String()
}

// evaluateExpression now takes the new ast.Expr interface
func (c *Compiler) evaluateExpression(expr ast.Expr) (value.Value, error) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		// Convert MAML int to LLVM i32 constant
		return constant.NewInt(types.I32, e.Value), nil

	case *ast.Identifier:
		// Look up the pointer in our symbol table
		ptr, ok := c.env[e.Value]
		if !ok {
			// Note: The Semantic Pass should have caught this already!
			return nil, fmt.Errorf("undefined variable: %s", e.Value)
		}
		// In LLVM, you can't just use a pointer; you have to Load the value from memory
		load := c.currentBlock.NewLoad(types.I32, ptr)
		return load, nil

	case *ast.InfixExpr: // Renamed from InfixExpression
		// Actually check the errors returned by the recursive calls
		left, err := c.evaluateExpression(e.Left)
		if err != nil {
			return nil, err
		}
		
		right, err := c.evaluateExpression(e.Right)
		if err != nil {
			return nil, err
		}

		// Map MAML operators to LLVM IR instructions
		switch e.Operator {
		case "+":
			return c.currentBlock.NewAdd(left, right), nil
		case "-":
			return c.currentBlock.NewSub(left, right), nil
		case "*":
			return c.currentBlock.NewMul(left, right), nil
		case "/":
			// SDiv is "Signed Division"
			return c.currentBlock.NewSDiv(left, right), nil
		default:
			return nil, fmt.Errorf("unsupported operator: %s", e.Operator)
		}
	}
	
	return nil, fmt.Errorf("unsupported expression: %T", expr)
}