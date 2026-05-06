package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
	"github.com/mattcarp12/maml/internal/ast"
)

func (c *Codegen) compileFnDecl(n *ast.FnDecl) error {
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

	return nil
}
