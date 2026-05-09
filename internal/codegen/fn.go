package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/mattcarp12/maml/internal/ast"
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

	// Dynamically resolve the return type instead of hardcoding I32
	retSemaType, ok := c.typeMap[n.ReturnType]
	if !ok {
		return fmt.Errorf("codegen error: unresolved return type for function '%s'", n.Name)
	}
	retLLVMType := c.llvmTypeFor(retSemaType)

	funcType := ir.NewFunc(n.Name, retLLVMType, llvmParams...)
	c.module.Funcs = append(c.module.Funcs, funcType)
	c.currentBlock = funcType.NewBlock("entry")

	c.pushEnv()
	defer c.popEnv()

	for i, p := range n.Params {
		alloc := c.currentBlock.NewAlloca(funcType.Params[i].Typ)
		c.currentBlock.NewStore(funcType.Params[i], alloc)
		c.setVar(p.Name, alloc)
	}

	_, err := c.compileBlockStmt(n.Body)
	return err
}
