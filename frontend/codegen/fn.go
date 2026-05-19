package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
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

	// Determine LLVM return type. Async functions return a coroutine handle (i8*)!
	var retLLVMType types.Type
	if n.IsAsync {
		retLLVMType = types.I8Ptr
	} else {
		retLLVMType = c.llvmTypeFor(retSemaType)
	}

	fn := c.module.NewFunc(n.Name, retLLVMType, llvmParams...)
	c.currentBlock = fn.NewBlock("entry")

	c.pushEnv()
	defer c.popEnv()

	// --- COROUTINE PROLOGUE ---
	if n.IsAsync {
		nullPtr := constant.NewNull(types.I8Ptr)
		zero32 := constant.NewInt(types.I32, 0)

		// 1. Get Coroutine ID
		coroId := c.currentBlock.NewCall(c.runtimeFuncs[LLVM_CoroId], zero32, nullPtr, nullPtr, nullPtr)

		// 2. Get Frame Size & Allocate
		coroSize := c.currentBlock.NewCall(c.runtimeFuncs[LLVM_CoroSize])
		coroAlloc := c.currentBlock.NewCall(c.runtimeFuncs[Maml_Alloc], coroSize)

		// 3. Begin Coroutine
		coroHdl := c.currentBlock.NewCall(c.runtimeFuncs[LLVM_CoroBegin], coroId, coroAlloc)

		// Store the handle so `await` and `return` can reference it
		c.setVar("__coro_hdl", coroHdl, false)
		c.setVar("__coro_id", coroId, false)
	}
	// --------------------------

	// Store parameters in allocas
	for i, p := range n.Params {
		alloc := c.currentBlock.NewAlloca(fn.Params[i].Typ)
		c.currentBlock.NewStore(fn.Params[i], alloc)

		isHeap := c.escapeMap[&n.Params[i]] == escape.StateHeap
		c.setVar(p.Name, alloc, isHeap)
	}

	_, err := c.compileBlockStmt(n.Body)
	return err
}
