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

	// LLVM Coroutine Intrinsics
	LLVM_CoroId
	LLVM_CoroSize
	LLVM_CoroBegin
	LLVM_CoroSuspend
	LLVM_CoroResume
	LLVM_CoroEnd
	LLVM_CoroFree
	LLVM_CoroDone
	LLVM_CoroDestroy
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

	// token @llvm.coro.id(i32 align, i8* promise, i8* coroaddr, i8* fnaddrs)
	c.runtimeFuncs[LLVM_CoroId] = c.module.NewFunc(
		"llvm.coro.id", types.Token,
		ir.NewParam("align", types.I32),
		ir.NewParam("promise", types.I8Ptr),
		ir.NewParam("coroaddr", types.I8Ptr),
		ir.NewParam("fnaddrs", types.I8Ptr),
	)

	// i64 @llvm.coro.size.i64()
	c.runtimeFuncs[LLVM_CoroSize] = c.module.NewFunc(
		"llvm.coro.size.i64", types.I64,
	)

	// i8* @llvm.coro.begin(token id, i8* mem)
	c.runtimeFuncs[LLVM_CoroBegin] = c.module.NewFunc(
		"llvm.coro.begin", types.I8Ptr,
		ir.NewParam("id", types.Token),
		ir.NewParam("mem", types.I8Ptr),
	)

	// i8 @llvm.coro.suspend(token save, i1 final)
	c.runtimeFuncs[LLVM_CoroSuspend] = c.module.NewFunc(
		"llvm.coro.suspend", types.I8,
		ir.NewParam("save", types.Token),
		ir.NewParam("final", types.I1),
	)

	// i1 @llvm.coro.end(i8* handle, i1 unwind)
	c.runtimeFuncs[LLVM_CoroEnd] = c.module.NewFunc(
		"llvm.coro.end", types.I1,
		ir.NewParam("handle", types.I8Ptr),
		ir.NewParam("unwind", types.I1),
	)

	// i8* @llvm.coro.free(token id, i8* handle)
	c.runtimeFuncs[LLVM_CoroFree] = c.module.NewFunc(
		"llvm.coro.free", types.I8Ptr,
		ir.NewParam("id", types.Token),
		ir.NewParam("handle", types.I8Ptr),
	)

	// i1 @llvm.coro.done(i8* handle)
	c.runtimeFuncs[LLVM_CoroDone] = c.module.NewFunc(
		"llvm.coro.done", types.I1,
		ir.NewParam("handle", types.I8Ptr),
	)

	// void @llvm.coro.resume(i8* handle)
	c.runtimeFuncs[LLVM_CoroResume] = c.module.NewFunc(
		"llvm.coro.resume", types.Void,
		ir.NewParam("handle", types.I8Ptr),
	)

	// void @llvm.coro.destroy(i8* handle)
	c.runtimeFuncs[LLVM_CoroDestroy] = c.module.NewFunc(
		"llvm.coro.destroy", types.Void,
		ir.NewParam("handle", types.I8Ptr),
	)

	// Generate maml_coro_resume_helper
	resumeHelper := c.module.NewFunc("maml_coro_resume_helper", types.Void, ir.NewParam("hdl", types.I8Ptr))
	rb := resumeHelper.NewBlock("")
	rb.NewCall(c.runtimeFuncs[LLVM_CoroResume], resumeHelper.Params[0])
	rb.NewRet(nil)

	// Generate maml_coro_done_helper
	doneHelper := c.module.NewFunc("maml_coro_done_helper", types.I1, ir.NewParam("hdl", types.I8Ptr))
	db := doneHelper.NewBlock("")
	isDone := db.NewCall(c.runtimeFuncs[LLVM_CoroDone], doneHelper.Params[0])
	db.NewRet(isDone)

	// Generate maml_coro_destroy_helper
	destroyHelper := c.module.NewFunc("maml_coro_destroy_helper", types.Void, ir.NewParam("hdl", types.I8Ptr))
	dxb := destroyHelper.NewBlock("")
	dxb.NewCall(c.runtimeFuncs[LLVM_CoroDestroy], destroyHelper.Params[0])
	dxb.NewRet(nil)
}
