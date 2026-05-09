package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
)

func (c *Codegen) getPutsFunc() *ir.Func {
	for _, f := range c.module.Funcs {
		if f.Name() == "puts" {
			return f
		}
	}
	// C declaration: int puts(char* s)
	return c.module.NewFunc("puts", types.I32, ir.NewParam("s", types.I8Ptr))
}
