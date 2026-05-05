package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
)

func (c *Compiler) getPutsFunc() *ir.Func {
	for _, f := range c.module.Funcs {
		if f.Name() == "puts" {
			return f
		}
	}
	// If it doesn't exist, declare it: declare i32 @puts(i8*)
	return c.module.NewFunc("puts", types.I32, ir.NewParam("", types.I8Ptr))
}
