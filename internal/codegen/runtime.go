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
	Maml_VecGrow
	Maml_MapCreate
	Maml_MapGet
	Maml_MapPut
	Maml_StrHash
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

	c.runtimeFuncs[Maml_VecGrow] = c.module.NewFunc(
		"maml_vec_grow",
		types.I8Ptr,
		ir.NewParam("raw_ptr", types.I8Ptr),
		ir.NewParam("len", types.I32),
		ir.NewParam("cap_ptr", types.NewPointer(types.I32)),
		ir.NewParam("item_size", types.I32),
	)

	// Inside setupRuntimeFuncs():
	c.runtimeFuncs[Maml_MapCreate] = c.module.NewFunc(
		"maml_map_create",
		types.I8Ptr,
		ir.NewParam("value_size", types.I32),
		ir.NewParam("is_string_key", types.I8),
	)

	c.runtimeFuncs[Maml_MapPut] = c.module.NewFunc(
		"maml_map_put",
		types.Void,
		ir.NewParam("map_ptr", types.I8Ptr),
		ir.NewParam("key_hash", types.I64),
		ir.NewParam("value_ptr", types.I8Ptr),
		ir.NewParam("str_key_ptr", types.I8Ptr),
		ir.NewParam("str_key_len", types.I32),
	)

	c.runtimeFuncs[Maml_MapGet] = c.module.NewFunc(
		"maml_map_get",
		types.I8Ptr,
		ir.NewParam("map_ptr", types.I8Ptr),
		ir.NewParam("key_hash", types.I64),
		ir.NewParam("str_key_ptr", types.I8Ptr),
		ir.NewParam("str_key_len", types.I32),
	)

	c.runtimeFuncs[Maml_StrHash] = c.module.NewFunc(
		"maml_str_hash",
		types.I64,
		ir.NewParam("ptr", types.I8Ptr),
		ir.NewParam("len", types.I32),
	)
}
