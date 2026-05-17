package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/sema"
)

// llvmTypeFor translates MAML semantic types to LLVM types with caching.
func (c *Codegen) llvmTypeFor(t sema.Type) types.Type {
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
		result = types.NewStruct(types.I8Ptr, types.I32)
	case *sema.StructType:
		var fields []types.Type
		for _, f := range v.Fields {
			fields = append(fields, c.llvmTypeFor(f.Type))
		}
		result = types.NewStruct(fields...)
	case sema.ArrayType:
		base := c.llvmTypeFor(v.Base)
		result = types.NewArray(uint64(v.Size), base)
	case sema.SliceType:
		baseLLVM := c.llvmTypeFor(v.Base)
		result = types.NewStruct(types.I8Ptr, types.NewPointer(baseLLVM), types.I32, types.I32)
	default:
		result = types.I32
	}

	c.typeCache[t.String()] = result
	return result
}

// load converts an L-value (address) to R-value (data)
func (c *Codegen) load(val CGValue) value.Value {
	if !val.IsAddress {
		return val.V
	}
	return c.currentBlock.NewLoad(c.llvmTypeFor(val.Type), val.V)
}

// newLabel generates unique labels
func (c *Codegen) newLabel(prefix string) string {
	c.labelCounter++
	return fmt.Sprintf("%s_%d", prefix, c.labelCounter)
}

// Scope / ARC helpers
func (c *Codegen) pushScope() {
	c.scopeStack = append(c.scopeStack, []value.Value{})
}

func (c *Codegen) trackForRelease(ptr value.Value) {
	topIdx := len(c.scopeStack) - 1
	c.scopeStack[topIdx] = append(c.scopeStack[topIdx], ptr)
}

func (c *Codegen) popScope() {
	topIdx := len(c.scopeStack) - 1
	topPage := c.scopeStack[topIdx]

	for i := len(topPage) - 1; i >= 0; i-- {
		ptr := topPage[i]
		rawPtr := c.currentBlock.NewBitCast(ptr, types.I8Ptr)
		c.currentBlock.NewCall(c.runtimeFuncs[Maml_Release], rawPtr)
	}

	c.scopeStack = c.scopeStack[:topIdx]
}

func (c *Codegen) getRawHeapPointer(valCG CGValue) value.Value {
	switch t := valCG.Type.(type) {
	case sema.ArrayType:
		return c.currentBlock.NewBitCast(valCG.V, types.I8Ptr)

	case sema.SliceType:
		zero := constant.NewInt(types.I32, 0)
		structType := c.llvmTypeFor(t)
		field0Ptr := c.currentBlock.NewGetElementPtr(structType, valCG.V, zero, zero)
		return c.currentBlock.NewLoad(types.I8Ptr, field0Ptr)

	default:
		return constant.NewNull(types.I8Ptr)
	}
}
