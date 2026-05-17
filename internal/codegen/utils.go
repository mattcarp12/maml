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
	case sema.VectorType:
		// Vec<T> shares the exact same layout as a SliceType under the hood:
		// { i8* raw_heap_ptr, T* data_ptr, i32 len, i32 cap }
		baseLLVM := c.llvmTypeFor(v.Base)
		result = types.NewStruct(types.I8Ptr, types.NewPointer(baseLLVM), types.I32, types.I32)

	case sema.MapType:
		// Map<K,V> is represented as a pointer to a runtime-managed hash table struct.
		// The actual layout of the hash table is opaque to us; we just treat it as an i8*.
		result = types.I8Ptr

	case sema.OptionType:
		// Represented as a tagged union struct: { T value, i32 discriminant }
		baseLLVM := c.llvmTypeFor(v.Base)
		result = types.NewStruct(baseLLVM, types.I32)

	case sema.ResultType:
		// Represented as a tagged union padding layout or sequential pairing for bootstrap:
		// { val_type, err_type, i32 discriminant }
		valLLVM := c.llvmTypeFor(v.Value)
		errLLVM := c.llvmTypeFor(v.Error)
		result = types.NewStruct(valLLVM, errLLVM, types.I32)
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

func (c *Codegen) trackForRelease(ptr value.Value, isHeap bool) {
	if !isHeap {
		return
	}
	topIdx := len(c.scopeStack) - 1
	c.scopeStack[topIdx] = append(c.scopeStack[topIdx], ptr)
}

func (c *Codegen) popScope() {
	topIdx := len(c.scopeStack) - 1
	topPage := c.scopeStack[topIdx]

	// NEW: Only emit release instructions if the block hasn't been terminated yet.
	// If a ReturnStmt already closed this block, inserting here creates invalid LLVM IR.
	if c.currentBlock.Term == nil {
		for i := len(topPage) - 1; i >= 0; i-- {
			ptr := topPage[i]
			rawPtr := c.currentBlock.NewBitCast(ptr, types.I8Ptr)
			c.currentBlock.NewCall(c.runtimeFuncs[Maml_Release], rawPtr)
		}
	}

	c.scopeStack = c.scopeStack[:topIdx]
}

func (c *Codegen) getRawHeapPointer(valCG CGValue) value.Value {
	switch t := valCG.Type.(type) {
	case sema.ArrayType:
		var ptr value.Value
		if valCG.IsAddress {
			ptr = valCG.V
		} else {
			// Dump the raw array to a temporary stack allocation to get a pointer
			llvmType := c.llvmTypeFor(t)
			alloc := c.currentBlock.NewAlloca(llvmType)
			c.currentBlock.NewStore(valCG.V, alloc)
			ptr = alloc
		}
		return c.currentBlock.NewBitCast(ptr, types.I8Ptr)

	case sema.SliceType:
		if !valCG.IsAddress {
			// If it's a raw struct value (e.g., just returned from a function),
			// extract the pointer at index 0 directly from the value!
			return c.currentBlock.NewExtractValue(valCG.V, 0)
		}

		// If it's an addressable pointer, use GEP and Load
		zero := constant.NewInt(types.I32, 0)
		structType := c.llvmTypeFor(t)
		field0Ptr := c.currentBlock.NewGetElementPtr(structType, valCG.V, zero, zero)
		return c.currentBlock.NewLoad(types.I8Ptr, field0Ptr)

	case sema.MapType:
		// If it's passed by address (stored in a local variable), load the pointer value.
		// If it's a direct R-Value (like a function return), return it directly.
		return c.load(valCG)

	case sema.VectorType:
		// Vec<T> shares the exact same layout as a SliceType under the hood:
		// { i8* raw_heap_ptr, T* data_ptr, i32 len, i32 cap }
		return c.load(valCG)

	default:
		return constant.NewNull(types.I8Ptr)
	}
}

// untrackFromRelease removes a pointer from the ARC tracker.
// Used when ownership is transferred (e.g., passed as 'own' or returned).
func (c *Codegen) untrackFromRelease(ptr value.Value) {
	// Search from the top of the scope stack downwards
	for pageIdx := len(c.scopeStack) - 1; pageIdx >= 0; pageIdx-- {
		page := c.scopeStack[pageIdx]
		for i := len(page) - 1; i >= 0; i-- {
			if page[i] == ptr {
				// Fast remove: swap with the last element and shrink the slice
				lastIdx := len(page) - 1
				page[i] = page[lastIdx]
				c.scopeStack[pageIdx] = page[:lastIdx]
				return
			}
		}
	}
}

// trackDeepForRelease recursively inspects a value. If it contains any heap-managed
// reference types (even buried inside structs), it retains and tracks them for release.
func (c *Codegen) trackDeepForRelease(valCG CGValue) {
	// Base Case: It's a direct reference type (Slice, String)
	if valCG.Type.IsReferenceType() {
		if valCG.IsHeap {
			rawPtr := c.getRawHeapPointer(valCG)
			c.currentBlock.NewCall(c.runtimeFuncs[Maml_Retain], rawPtr)
			c.trackForRelease(rawPtr, true)
		}
		return
	}

	// Composite Case: Structs
	if structType, ok := valCG.Type.(*sema.StructType); ok {
		structLLVMType := c.llvmTypeFor(structType)

		// We need an addressable pointer to GEP into the struct's fields.
		var basePtr value.Value
		if valCG.IsAddress {
			basePtr = valCG.V
		} else {
			// Dump the by-value struct into a temporary alloca so we can index it
			alloc := c.currentBlock.NewAlloca(structLLVMType)
			c.currentBlock.NewStore(valCG.V, alloc)
			basePtr = alloc
		}

		zero := constant.NewInt(types.I32, 0)

		for i, field := range structType.Fields {
			// Optimization: Only recurse if the field is a reference type or another composite
			if field.Type.IsReferenceType() || c.isCompositeType(field.Type) {
				idx := constant.NewInt(types.I32, int64(i))
				fieldPtr := c.currentBlock.NewGetElementPtr(structLLVMType, basePtr, zero, idx)

				fieldCG := CGValue{
					V:         fieldPtr,
					Type:      field.Type,
					IsAddress: true,
					IsHeap:    valCG.IsHeap, // Inner fields inherit the container's heap status
				}
				c.trackDeepForRelease(fieldCG)
			}
		}
	}

	// Composite Case: Arrays (Fixed-size arrays passed by value)
	if arrType, ok := valCG.Type.(sema.ArrayType); ok {
		// Only bother recursing if the array holds reference types or composites
		if arrType.Base.IsReferenceType() || c.isCompositeType(arrType.Base) {
			arrLLVMType := c.llvmTypeFor(arrType)

			var basePtr value.Value
			if valCG.IsAddress {
				basePtr = valCG.V
			} else {
				alloc := c.currentBlock.NewAlloca(arrLLVMType)
				c.currentBlock.NewStore(valCG.V, alloc)
				basePtr = alloc
			}

			// Unroll the array elements to track them
			for i := 0; i < arrType.Size; i++ {
				idx := constant.NewInt(types.I32, int64(i))
				elemPtr := c.currentBlock.NewGetElementPtr(arrLLVMType, basePtr,
					constant.NewInt(types.I32, 0), idx)

				elemCG := CGValue{
					V:         elemPtr,
					Type:      arrType.Base,
					IsAddress: true,
					IsHeap:    valCG.IsHeap,
				}
				c.trackDeepForRelease(elemCG)
			}
		}
	}
}

// Helper to identify types that might contain reference types
func (c *Codegen) isCompositeType(t sema.Type) bool {
	switch t.(type) {
	case *sema.StructType, sema.ArrayType:
		return true
	default:
		return false
	}
}

// untrackDeepFromRelease recursively inspects a value being returned or moved.
// It removes any internal heap pointers from the ARC tracker so they survive the frame boundary.
func (c *Codegen) untrackDeepFromRelease(valCG CGValue) {
	// Base Case: It's a direct reference type
	if valCG.Type.IsReferenceType() {
		if valCG.IsHeap {
			rawPtr := c.getRawHeapPointer(valCG)
			c.untrackFromRelease(rawPtr)
		}
		return
	}

	// Composite Case: Structs
	if structType, ok := valCG.Type.(*sema.StructType); ok {
		structLLVMType := c.llvmTypeFor(structType)
		var basePtr value.Value
		if valCG.IsAddress {
			basePtr = valCG.V
		} else {
			alloc := c.currentBlock.NewAlloca(structLLVMType)
			c.currentBlock.NewStore(valCG.V, alloc)
			basePtr = alloc
		}

		zero := constant.NewInt(types.I32, 0)
		for i, field := range structType.Fields {
			if field.Type.IsReferenceType() || c.isCompositeType(field.Type) {
				idx := constant.NewInt(types.I32, int64(i))
				fieldPtr := c.currentBlock.NewGetElementPtr(structLLVMType, basePtr, zero, idx)

				fieldCG := CGValue{
					V:         fieldPtr,
					Type:      field.Type,
					IsAddress: true,
					IsHeap:    valCG.IsHeap,
				}
				c.untrackDeepFromRelease(fieldCG)
			}
		}
	}

	// Composite Case: Arrays
	if arrType, ok := valCG.Type.(sema.ArrayType); ok {
		if arrType.Base.IsReferenceType() || c.isCompositeType(arrType.Base) {
			arrLLVMType := c.llvmTypeFor(arrType)
			var basePtr value.Value
			if valCG.IsAddress {
				basePtr = valCG.V
			} else {
				alloc := c.currentBlock.NewAlloca(arrLLVMType)
				c.currentBlock.NewStore(valCG.V, alloc)
				basePtr = alloc
			}

			for i := 0; i < arrType.Size; i++ {
				idx := constant.NewInt(types.I32, int64(i))
				elemPtr := c.currentBlock.NewGetElementPtr(arrLLVMType, basePtr, constant.NewInt(types.I32, 0), idx)
				elemCG := CGValue{
					V:         elemPtr,
					Type:      arrType.Base,
					IsAddress: true,
					IsHeap:    valCG.IsHeap,
				}
				c.untrackDeepFromRelease(elemCG)
			}
		}
	}
}
