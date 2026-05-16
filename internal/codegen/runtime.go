package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
)

type RuntimeFunc int

const (
	Maml_Alloc = iota
	Maml_Free
	Maml_Retain
	Maml_Release
)

func (c *Codegen) setupRuntimeFuncs() {
	if c.runtimeFuncs == nil {
		c.runtimeFuncs = make(map[RuntimeFunc]*ir.Func)
	}
	// 1. Declare `maml_alloc`
	// Signature: i8* maml_alloc(i64 size)
	// Notice we use types.I64 for the size to match Zig's `usize` (assuming a 64-bit target)
	c.runtimeFuncs[Maml_Alloc] = c.module.NewFunc(
		"maml_alloc",
		types.I8Ptr, // Returns a raw byte pointer
		ir.NewParam("size", types.I64),
	)

	// 2. Declare `maml_free`
	// Signature: void maml_free(i8* ptr)
	c.runtimeFuncs[Maml_Free] = c.module.NewFunc(
		"maml_free",
		types.Void,
		ir.NewParam("ptr", types.I8Ptr),
	)

	// i8* maml_retain(i8* ptr)
	c.runtimeFuncs[Maml_Retain] = c.module.NewFunc(
		"maml_retain",
		types.Void,
		ir.NewParam("ptr", types.I8Ptr),
	)

	// i8* maml_release(i8* ptr)
	c.runtimeFuncs[Maml_Release] = c.module.NewFunc(
		"maml_release",
		types.Void,
		ir.NewParam("ptr", types.I8Ptr),
	)
}
