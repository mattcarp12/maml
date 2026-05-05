package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
)

type Compiler struct {
	module *ir.Module

	// The current block of code we are writing LLVM instructions to
	currentBlock *ir.Block

	// Our "Symbol Table". It maps a MAML variable name (like "x")
	// to an LLVM memory pointer.
	env map[string]value.Value

	currentYield value.Value // Tracks the value of the most recent '=>' statement
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
		for _, decl := range n.Decls {
			if err := c.Compile(decl); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		// 1. Create LLVM Parameters
		var llvmParams []*ir.Param
		for _, p := range n.Params {
			// e.g., i32 %x
			llvmParams = append(llvmParams, ir.NewParam(p.Name, types.I32))
		}

		// 2. Create the LLVM function
		funcType := ir.NewFunc(n.Name, types.I32, llvmParams...)
		c.module.Funcs = append(c.module.Funcs, funcType)

		// 3. Create the entry block
		c.currentBlock = funcType.NewBlock("entry")

		// 4. THE ALLOCA TRICK: Move parameters to local stack memory
		for i, p := range n.Params {
			// Allocate stack memory: %x_ptr = alloca i32
			alloc := c.currentBlock.NewAlloca(types.I32)

			// Store the incoming parameter into that memory: store i32 %x, i32* %x_ptr
			c.currentBlock.NewStore(funcType.Params[i], alloc)

			// Save the memory pointer in our environment so `evaluateExpression` can find it!
			c.env[p.Name] = alloc
		}

		// 5. Compile the body
		if err := c.Compile(n.Body); err != nil {
			return err
		}

	case *ast.BlockStmt: // Renamed from BlockStatement
		for _, stmt := range n.Statements {
			if err := c.Compile(stmt); err != nil {
				return err
			}
		}

	case *ast.DeclareStmt:
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

	case *ast.ReturnStmt: // Renamed from ReturnStatement
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}
		c.currentBlock.NewRet(val)

	case *ast.YieldStmt:
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}
		// Save the yielded value into compiler state so the IfExpr can grab it
		c.currentYield = val
	}

	return nil
}

// String outputs the final LLVM IR text
func (c *Compiler) String() string {
	return c.module.String()
}
