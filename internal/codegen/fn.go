package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

func (c *Codegen) compileFnDecl(n *ast.FnDecl) error {
	var llvmParams []*ir.Param
	var paramType types.Type
	for _, p := range n.Params {
		paramType = types.I32 // Fallback

		// Map the parameter type dynamically using our TypeMap
		if pType, ok := c.typeMap[p.Type]; ok {
			paramType = c.llvmTypeFor(pType)
		} else if p.Type.String() == "string" {
			paramType = c.llvmTypeFor(sema.StringType{})
		}

		llvmParams = append(llvmParams, ir.NewParam(p.Name, paramType))
	}

	funcType := ir.NewFunc(n.Name, types.I32, llvmParams...)
	c.module.Funcs = append(c.module.Funcs, funcType)
	c.currentBlock = funcType.NewBlock("entry")

	c.pushEnv() // FIX: Push function scope
	defer c.popEnv()

	for i, p := range n.Params {
		alloc := c.currentBlock.NewAlloca(funcType.Params[i].Typ)
		c.currentBlock.NewStore(funcType.Params[i], alloc)
		c.setVar(p.Name, alloc) // FIX: Use new Env method
	}

	// Because blocks return values now, we capture but ignore it for function bodies
	_, err := c.compileBlockStmt(n.Body)
	return err
}
