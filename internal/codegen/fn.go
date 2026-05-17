package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/escape"
)

func (c *Codegen) compileFnDecl(n *ast.FnDecl) error {
	var llvmParams []*ir.Param
	for _, p := range n.Params {
		pType, ok := c.typeMap[p.Type]
		if !ok {
			return fmt.Errorf("codegen error: unresolved type for parameter '%s'", p.Name)
		}
		paramType := c.llvmTypeFor(pType)
		llvmParams = append(llvmParams, ir.NewParam(p.Name, paramType))
	}

	retSemaType, ok := c.typeMap[n.ReturnType]
	if !ok {
		return fmt.Errorf("codegen error: unresolved return type for function '%s'", n.Name)
	}
	retLLVMType := c.llvmTypeFor(retSemaType)

	fn := c.module.NewFunc(n.Name, retLLVMType, llvmParams...)
	c.currentBlock = fn.NewBlock("entry")

	c.pushEnv()
	defer c.popEnv()

	// Store parameters in allocas
	for i, p := range n.Params {
		alloc := c.currentBlock.NewAlloca(fn.Params[i].Typ)
		c.currentBlock.NewStore(fn.Params[i], alloc)

		// NEW: Look up the heap status of this parameter from the escape analyzer
		isHeap := c.escapeMap[&n.Params[i]] == escape.StateHeap

		// Pass the isHeap flag to setVar!
		c.setVar(p.Name, alloc, isHeap)
	}

	_, err := c.compileBlockStmt(n.Body)
	return err
}
