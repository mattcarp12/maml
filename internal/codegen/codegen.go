package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

type Env struct {
	parent *Env
	vars   map[string]value.Value
}

type Codegen struct {
	module       *ir.Module
	currentBlock *ir.Block
	currentEnv   *Env
	typeMap      map[ast.Node]sema.Type
	typeCache    map[string]types.Type
	labelCounter int
	runtimeFuncs map[RuntimeFunc]*ir.Func
	scopeStack   [][]value.Value
}

// CGValue wraps an LLVM value with its MAML type and memory category.
type CGValue struct {
	V         value.Value
	Type      sema.Type
	IsAddress bool // True if V is a pointer to the actual data
}

func New() *Codegen {
	c := &Codegen{
		module:       ir.NewModule(),
		currentEnv:   &Env{vars: make(map[string]value.Value)},
		typeCache:    make(map[string]types.Type),
		runtimeFuncs: make(map[RuntimeFunc]*ir.Func),
		scopeStack:   make([][]value.Value, 0),
	}
	c.scopeStack = append(c.scopeStack, []value.Value{})
	c.setupRuntimeFuncs()
	return c
}

// Module returns the underlying LLVM IR module object.
func (c *Codegen) Module() *ir.Module {
	return c.module
}

func (c *Codegen) pushEnv() {
	c.currentEnv = &Env{parent: c.currentEnv, vars: make(map[string]value.Value)}
}

func (c *Codegen) popEnv() {
	if c.currentEnv != nil {
		c.currentEnv = c.currentEnv.parent
	}
}

func (c *Codegen) resolveVar(name string) (value.Value, bool) {
	for e := c.currentEnv; e != nil; e = e.parent {
		if val, ok := e.vars[name]; ok {
			return val, true
		}
	}
	return nil, false
}

func (c *Codegen) setVar(name string, val value.Value) {
	c.currentEnv.vars[name] = val
}

// Generate is the entry point. We now accept the typeMap!
func (c *Codegen) Generate(node ast.Node, typeMap map[ast.Node]sema.Type) error {
	// Set unconditionally if provided
	if typeMap != nil {
		c.typeMap = typeMap
	}

	var err error
	switch n := node.(type) {
	case *ast.Program:
		for _, decl := range n.Decls {
			// Pass nil recursively; we rely on c.typeMap now
			if err := c.Generate(decl, nil); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		err = c.compileFnDecl(n)

	}

	return err
}

func (c *Codegen) String() string {
	return c.module.String()
}

// llvmTypeFor dynamically translates our semantic types into LLVM memory layouts.
func (c *Codegen) llvmTypeFor(t sema.Type) types.Type {
	// 1. Check if we've already created this LLVM type
	if cached, ok := c.typeCache[t.String()]; ok {
		return cached
	}

	var result types.Type

	switch v := t.(type) {
	case sema.IntType:
		result = types.I32
	case sema.BoolType:
		result = types.I1
	case sema.StringType:
		// Cache the "fat pointer" struct for strings
		result = types.NewStruct(types.I8Ptr, types.I32)
	case *sema.StructType:
		// For structs, we must build the fields
		var fields []types.Type
		for _, f := range v.Fields {
			fields = append(fields, c.llvmTypeFor(f.Type))
		}
		// Use Module.NewStruct for named types so LLVM prints them nicely (e.g., %Point)
		res := types.NewStruct(fields...)
		result = res
	case sema.ArrayType:
		baseLLVMType := c.llvmTypeFor(v.Base)
		result = types.NewArray(uint64(v.Size), baseLLVMType)
	case sema.SliceType:
		baseLLVMType := c.llvmTypeFor(v.Base)
		basePtr := types.I8Ptr
		slicePtr := types.NewPointer(baseLLVMType)
		sliceLen, sliceCap := types.I32, types.I32
		result = types.NewStruct(basePtr, slicePtr, sliceLen, sliceCap)
	default:
		result = types.I32
	}

	// 2. Save the result in the cache for next time
	c.typeCache[t.String()] = result
	return result
}

// load converts an L-value (address) into an R-value (data).
// If the value is already data, it returns it as-is.
func (c *Codegen) load(val CGValue) value.Value {
	if !val.IsAddress {
		return val.V
	}

	// Always load the value, regardless of whether it's a primitive or a struct.
	// This enforces true pass-by-value semantics in LLVM!
	return c.currentBlock.NewLoad(c.llvmTypeFor(val.Type), val.V)
}

func (c *Codegen) newLabel(prefix string) string {
	c.labelCounter++
	return fmt.Sprintf("%s_%d", prefix, c.labelCounter)
}

// pushScope adds a new blank page when entering a { block
func (c *Codegen) pushScope() {
	c.scopeStack = append(c.scopeStack, []value.Value{})
}

// trackForRelease writes a heap pointer onto the current page
func (c *Codegen) trackForRelease(ptr value.Value) {
	topIdx := len(c.scopeStack) - 1
	c.scopeStack[topIdx] = append(c.scopeStack[topIdx], ptr)
}

// popScope exits a } block, releases all memory on the current page, and rips it out
func (c *Codegen) popScope() {
	topIdx := len(c.scopeStack) - 1
	topPage := c.scopeStack[topIdx]

	// 1. Loop backwards through the pointers and emit release calls!
	// (Releasing in reverse order of creation is standard practice)
	for i := len(topPage) - 1; i >= 0; i-- {
		ptr := topPage[i]
		// Bitcast whatever pointer it is (e.g. [3 x i32]*) back to a raw i8* for the runtime
		rawPtr := c.currentBlock.NewBitCast(ptr, types.I8Ptr)
		c.currentBlock.NewCall(c.runtimeFuncs[Maml_Release], rawPtr)
	}

	// 2. Rip the page out of the notebook
	c.scopeStack = c.scopeStack[:topIdx]
}

func isHeapManaged(t sema.Type) bool {
	switch t.(type) {
	case sema.ArrayType, sema.SliceType:
		return true
	default:
		return false // Ints and Bools are just copied by value, no ARC needed!
	}
}

// getRawHeapPointer safely extracts the underlying heap allocation pointer
func (c *Codegen) getRawHeapPointer(valCG CGValue) value.Value {
	switch t := valCG.Type.(type) {
	case sema.ArrayType:
		// Arrays in MAML are direct pointers to the heap allocation
		return c.currentBlock.NewBitCast(valCG.V, types.I8Ptr)

	case sema.SliceType:
		// Slices are Fat Pointers on the stack. Field 0 contains the heap pointer.
		zero := constant.NewInt(types.I32, 0)
		structType := c.llvmTypeFor(t)

		// GEP to Field 0
		field0Ptr := c.currentBlock.NewGetElementPtr(structType, valCG.V, zero, zero)

		return c.currentBlock.NewLoad(types.I8Ptr, field0Ptr)

	default:
		return constant.NewNull(types.I8Ptr)
	}
}
