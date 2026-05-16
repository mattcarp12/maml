package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

func (c *Codegen) evaluateExpression(expr ast.Expr) (CGValue, error) {
	semaType := c.typeMap[expr]

	switch e := expr.(type) {
	case *ast.IntLiteral:
		return CGValue{V: constant.NewInt(types.I32, e.Value), Type: semaType, IsAddress: false}, nil

	case *ast.BoolLiteral:
		if e.Value {
			return CGValue{V: constant.True, Type: semaType, IsAddress: false}, nil
		}
		return CGValue{V: constant.False, Type: semaType, IsAddress: false}, nil

	case *ast.Identifier:
		alloc, exists := c.resolveVar(e.Value)
		if !exists {
			return CGValue{}, fmt.Errorf("undefined variable: %s", e.Value)
		}
		// Identifiers ALWAYS return an address (the pointer to the variable)
		return CGValue{V: alloc, Type: semaType, IsAddress: true}, nil

	case *ast.InfixExpr:
		leftCG, err := c.evaluateExpression(e.Left)
		if err != nil {
			return CGValue{}, err
		}

		switch e.Operator {
		case "&&":
			// Short-circuiting AND: if left is false, return false immediately
			// Otherwise, evaluate and return the right side
			fn := c.currentBlock.Parent
			rightBlk := fn.NewBlock(c.newLabel("and_right"))
			mergeBlk := fn.NewBlock(c.newLabel("and_merge"))

			leftVal := c.load(leftCG)
			leftExitBlk := c.currentBlock
			c.currentBlock.NewCondBr(leftVal, rightBlk, mergeBlk)

			// --- EVALUATE RIGHT BLOCK ---
			c.currentBlock = rightBlk
			rightCG, _ := c.evaluateExpression(e.Right)
			rightVal := c.load(rightCG)
			rightExitBlk := c.currentBlock // Remember where right finished!
			c.currentBlock.NewBr(mergeBlk)

			// --- MERGE BLOCK ---
			c.currentBlock = mergeBlk
			// Phi: If from left block, result is false. If from right block, result is rightVal.
			phi := mergeBlk.NewPhi(
				ir.NewIncoming(constant.False, leftExitBlk),
				ir.NewIncoming(rightVal, rightExitBlk),
			)
			return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
		case "||":
			// Short-circuiting OR: if left is true, return true immediately
			// Otherwise, evaluate and return the right side
			fn := c.currentBlock.Parent
			rightBlk := fn.NewBlock(c.newLabel("or_right"))
			mergeBlk := fn.NewBlock(c.newLabel("or_merge"))

			leftVal := c.load(leftCG)
			leftExitBlk := c.currentBlock
			c.currentBlock.NewCondBr(leftVal, mergeBlk, rightBlk)

			// --- Evaluate the Right Block ---
			c.currentBlock = rightBlk
			rightCG, err := c.evaluateExpression(e.Right)
			if err != nil {
				return CGValue{}, err
			}
			rightVal := c.load(rightCG)
			rightExitBlk := c.currentBlock
			c.currentBlock.NewBr(mergeBlk)

			// --- Merge Block ---
			c.currentBlock = mergeBlk
			phi := mergeBlk.NewPhi(
				ir.NewIncoming(constant.True, leftExitBlk),
				ir.NewIncoming(rightVal, rightExitBlk),
			)
			return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
		}

		rightCG, err := c.evaluateExpression(e.Right)
		if err != nil {
			return CGValue{}, err
		}

		left := c.load(leftCG)
		right := c.load(rightCG)

		var res value.Value
		switch e.Operator {
		case "+":
			res = c.currentBlock.NewAdd(left, right)
		case "-":
			res = c.currentBlock.NewSub(left, right)
		case "*":
			res = c.currentBlock.NewMul(left, right)
		case "/":
			res = c.currentBlock.NewSDiv(left, right)
		case "<":
			res = c.currentBlock.NewICmp(enum.IPredSLT, left, right)
		case ">":
			res = c.currentBlock.NewICmp(enum.IPredSGT, left, right)
		case "<=":
			res = c.currentBlock.NewICmp(enum.IPredSLE, left, right)
		case ">=":
			res = c.currentBlock.NewICmp(enum.IPredSGE, left, right)
		case "==":
			res = c.currentBlock.NewICmp(enum.IPredEQ, left, right)
		case "!=":
			res = c.currentBlock.NewICmp(enum.IPredNE, left, right)
		}
		return CGValue{V: res, Type: semaType, IsAddress: false}, nil

	case *ast.PrefixExpr:
		rightCG, err := c.evaluateExpression(e.Right)
		if err != nil {
			return CGValue{}, err
		}
		right := c.load(rightCG)

		var res value.Value
		switch e.Operator {
		case "-":
			res = c.currentBlock.NewSub(constant.NewInt(types.I32, 0), right)
		case "!":
			res = c.currentBlock.NewXor(right, constant.True)
		}
		return CGValue{V: res, Type: semaType, IsAddress: false}, nil

	case *ast.IfExpr:
		condVal, err := c.evaluateExpression(e.Condition)
		if err != nil {
			return CGValue{}, err
		}

		fn := c.currentBlock.Parent
		thenBlk := fn.NewBlock(c.newLabel("then"))
		elseBlk := fn.NewBlock(c.newLabel("else"))
		mergeBlk := fn.NewBlock(c.newLabel("merge"))

		c.currentBlock.NewCondBr(c.load(condVal), thenBlk, elseBlk)

		var phiIncoming []*ir.Incoming

		// --- COMPILE THE 'THEN' BRANCH ---
		c.currentBlock = thenBlk
		thenYield, err := c.compileBlockStmt(e.Consequence)
		if err != nil {
			return CGValue{}, err
		}

		// FIX: Only branch to merge if there is no terminator (like a return!)
		if c.currentBlock.Term == nil {
			c.currentBlock.NewBr(mergeBlk)
			if thenYield != nil {
				phiIncoming = append(phiIncoming, ir.NewIncoming(thenYield, c.currentBlock))
			}
		}

		// --- COMPILE THE 'ELSE' BRANCH ---
		c.currentBlock = elseBlk
		var elseYield value.Value

		if e.Alternative != nil {
			elseYield, err = c.compileBlockStmt(e.Alternative)
			if err != nil {
				return CGValue{}, err
			}

			// FIX: Only branch to merge if there is no terminator
			if c.currentBlock.Term == nil {
				c.currentBlock.NewBr(mergeBlk)
				if elseYield != nil {
					phiIncoming = append(phiIncoming, ir.NewIncoming(elseYield, c.currentBlock))
				}
			}
		} else {
			// No else block, so we just branch straight to merge
			c.currentBlock.NewBr(mergeBlk)
		}

		// --- THE MERGE BLOCK ---
		c.currentBlock = mergeBlk

		// Only create a phi node if branches actually flowed into this block with values
		if len(phiIncoming) > 0 {
			phi := mergeBlk.NewPhi(phiIncoming...)
			return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
		}

		return CGValue{}, nil

	case *ast.CallExpr:
		ident, isIdent := e.Function.(*ast.Identifier)
		if !isIdent {
			return CGValue{}, fmt.Errorf("unsupported function call: only direct calls to identifiers are supported")
		}
		// TODO - eventually support more complex call expressions (e.g. function pointers, methods, etc.)
		var targetFunc *ir.Func

		if ident.Value == "puts" {
			targetFunc = c.getPutsFunc()
		} else {
			for _, f := range c.module.Funcs {
				if f.Name() == ident.Value {
					targetFunc = f
					break
				}
			}
		}

		if targetFunc == nil {
			return CGValue{}, fmt.Errorf("function %s not found in LLVM module", ident)
		}

		var llvmArgs []value.Value
		for i, arg := range e.Arguments {
			argCG, err := c.evaluateExpression(arg)
			if err != nil {
				return CGValue{}, err
			}
			val := c.load(argCG)

			// --- THE STRING LOWERING RULE ---
			// If calling 'puts', convert the MAML Fat Pointer into a raw C pointer
			if isIdent && ident.Value == "puts" && i == 0 {
				if _, ok := argCG.Type.(sema.StringType); ok {
					// 'val' is now the loaded struct VALUE {i8*, i32}.
					// Extract the first field (the raw i8*) directly!
					val = c.currentBlock.NewExtractValue(val, 0)
				}
			}

			llvmArgs = append(llvmArgs, val)
		}

		res := c.currentBlock.NewCall(targetFunc, llvmArgs...)
		return CGValue{V: res, Type: semaType, IsAddress: false}, nil

	case *ast.StructLiteral:
		llvmType := c.llvmTypeFor(semaType)
		structPtr := c.currentBlock.NewAlloca(llvmType)

		for _, field := range e.Fields {
			valCG, err := c.evaluateExpression(field.Value)
			if err != nil {
				return CGValue{}, err
			}
			val := c.load(valCG) // Ensure we have the data to store

			index := semaType.(*sema.StructType).GetFieldIndex(field.Name.Value)
			fieldPtr := c.currentBlock.NewGetElementPtr(llvmType, structPtr,
				constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(index)))

			c.currentBlock.NewStore(val, fieldPtr)
		}
		// A struct literal returns an address (the pointer to the alloca)
		return CGValue{V: structPtr, Type: semaType, IsAddress: true}, nil

	case *ast.FieldAccess:
		objCG, err := c.evaluateExpression(e.Object)
		if err != nil {
			return CGValue{}, err
		}
		// We need the address of the struct to find the field address
		objPtr := objCG.V

		structType := objCG.Type.(*sema.StructType)
		index := structType.GetFieldIndex(e.Field.Value)

		fieldPtr := c.currentBlock.NewGetElementPtr(c.llvmTypeFor(structType), objPtr,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(index)))

		// FieldAccess returns the address of the field
		return CGValue{V: fieldPtr, Type: semaType, IsAddress: true}, nil

	case *ast.StringLiteral:
		strVal := e.Value
		strLen := len(strVal)

		// Append a null terminator so libc functions like 'puts' work
		strBytes := append([]byte(strVal), 0)
		strConst := constant.NewCharArray(strBytes)
		globalDef := c.module.NewGlobalDef(c.newLabel("str"), strConst)
		globalDef.Immutable = true

		// Create the MAML Fat Pointer { i8*, i32 } on the stack
		strLLVMType := c.llvmTypeFor(semaType)
		structAlloc := c.currentBlock.NewAlloca(strLLVMType)

		zero := constant.NewInt(types.I32, 0)
		charPtr := c.currentBlock.NewGetElementPtr(
			types.NewArray(uint64(len(strBytes)), types.I8),
			globalDef, zero, zero,
		)

		// Set Field 0: Raw Pointer
		p0 := c.currentBlock.NewGetElementPtr(strLLVMType, structAlloc, zero, zero)
		c.currentBlock.NewStore(charPtr, p0)

		// Set Field 1: Length (MAML specific)
		p1 := c.currentBlock.NewGetElementPtr(strLLVMType, structAlloc, zero, constant.NewInt(types.I32, 1))
		c.currentBlock.NewStore(constant.NewInt(types.I32, int64(strLen)), p1)

		return CGValue{V: structAlloc, Type: semaType, IsAddress: true}, nil

	case *ast.ArrayLiteral:
		arrType := c.llvmTypeFor(semaType)

		// Stack allocation
		// arrayAlloc := c.currentBlock.NewAlloca(arrType)

		// --- HEAP ALLOCATION ---
		// 1. Calculate the total size in bytes.
		// (Assuming 32-bit/4-byte integers for this example.
		// In a real compiler, you'd ask LLVM for the TargetData size of the element!)
		byteSize := int64(len(e.Elements) * 4)
		sizeVal := constant.NewInt(types.I64, byteSize)

		// 2. Call our runtime allocator!
		// Returns an i8* (raw byte pointer)
		rawHeapPtr := c.currentBlock.NewCall(c.runtimeAlloc, sizeVal)

		// 3. Bitcast the i8* pointer into our actual Array Pointer type (e.g., [3 x i32]*)
		// so the rest of our GEP logic works perfectly!
		arrayAlloc := c.currentBlock.NewBitCast(rawHeapPtr, types.NewPointer(arrType))

		for i, elem := range e.Elements {
			elemCG, err := c.evaluateExpression(elem)
			if err != nil {
				return CGValue{}, err
			}
			elemVal := c.load(elemCG)

			index := constant.NewInt(types.I32, int64(i))
			elemPtr := c.currentBlock.NewGetElementPtr(arrType, arrayAlloc, constant.NewInt(types.I32, 0), index)
			c.currentBlock.NewStore(elemVal, elemPtr)
		}
		return CGValue{V: arrayAlloc, Type: semaType, IsAddress: true}, nil

	case *ast.IndexExpr:
		// 1. Evaluate the left side (array or slice)
		leftCG, err := c.evaluateExpression(e.Left)
		if err != nil {
			return CGValue{}, err
		}

		// 2. Evaluate and load the index
		idxCG, err := c.evaluateExpression(e.Index)
		if err != nil {
			return CGValue{}, err
		}
		idxVal := c.load(idxCG)

		var elemPtr value.Value

		switch t := leftCG.Type.(type) {
		case sema.ArrayType:
			// --- ARRAY INDEXING ---
			var arrayPtr value.Value
			if leftCG.IsAddress {
				arrayPtr = leftCG.V
			} else {
				llvmType := c.llvmTypeFor(leftCG.Type)
				alloc := c.currentBlock.NewAlloca(llvmType)
				c.currentBlock.NewStore(leftCG.V, alloc)
				arrayPtr = alloc
			}

			zero := constant.NewInt(types.I32, 0)
			elemPtr = c.currentBlock.NewGetElementPtr(
				c.llvmTypeFor(t),
				arrayPtr,
				zero,
				idxVal, // Second index steps along the array
			)

		case sema.SliceType:
			// --- SLICE INDEXING ---
			var sliceStructPtr value.Value
			if leftCG.IsAddress {
				sliceStructPtr = leftCG.V
			} else {
				llvmType := c.llvmTypeFor(leftCG.Type)
				alloc := c.currentBlock.NewAlloca(llvmType)
				c.currentBlock.NewStore(leftCG.V, alloc)
				sliceStructPtr = alloc
			}

			// A. Get the address of Field 0 (The raw pointer)
			zero := constant.NewInt(types.I32, 0)
			dataPtrPtr := c.currentBlock.NewGetElementPtr(
				c.llvmTypeFor(t),
				sliceStructPtr,
				zero,
				zero, // Field 0
			)

			// B. Load the raw pointer from the struct!
			baseLLVMType := c.llvmTypeFor(t.Base)
			rawPointerType := types.NewPointer(baseLLVMType)
			dataPtr := c.currentBlock.NewLoad(rawPointerType, dataPtrPtr)

			// C. Index directly into the raw pointer (Notice: Only ONE index here!)
			elemPtr = c.currentBlock.NewGetElementPtr(
				baseLLVMType,
				dataPtr,
				idxVal,
			)

		case sema.StringType:
			// --- STRING INDEXING ---
			var strStructPtr value.Value
			if leftCG.IsAddress {
				strStructPtr = leftCG.V
			} else {
				llvmType := c.llvmTypeFor(leftCG.Type)
				alloc := c.currentBlock.NewAlloca(llvmType)
				c.currentBlock.NewStore(leftCG.V, alloc)
				strStructPtr = alloc
			}

			// A. Get the address of Field 0 (The raw i8* pointer)
			zero := constant.NewInt(types.I32, 0)
			dataPtrPtr := c.currentBlock.NewGetElementPtr(
				c.llvmTypeFor(t),
				strStructPtr,
				zero,
				zero, // Field 0 is the pointer
			)

			// B. Load the raw pointer from the struct!
			// We explicitly know the base type is i8 (byte)
			rawPointerType := types.NewPointer(types.I8)
			dataPtr := c.currentBlock.NewLoad(rawPointerType, dataPtrPtr)

			// C. Index directly into the raw pointer
			elemPtr = c.currentBlock.NewGetElementPtr(
				types.I8, // explicitly step by 1 byte (i8) at a time
				dataPtr,
				idxVal,
			)

			// D. Load the 8-bit byte immediately!
			byteVal := c.currentBlock.NewLoad(types.I8, elemPtr)

			// E. Zero-Extend (ZExt) the 8-bit byte into a 32-bit integer
			// so the rest of MAML's math system (which expects i32) is happy.
			int32Val := c.currentBlock.NewZExt(byteVal, types.I32)

			// F. Return the 32-bit integer directly!
			// Notice IsAddress is FALSE. Because it's a value, the compiler
			// won't ever try to call c.load() on it later, and it physically
			// prevents users from doing `s[0] = 5` in Codegen!
			return CGValue{V: int32Val, Type: semaType, IsAddress: false}, nil

		default:
			return CGValue{}, fmt.Errorf("cannot index into non-array/slice type %T", leftCG.Type)
		}

		// 3. Return the element pointer!
		return CGValue{V: elemPtr, Type: semaType, IsAddress: true}, nil

	case *ast.SliceExpr:
		// 1. Evaluate the array/slice we are slicing
		leftCG, err := c.evaluateExpression(e.Left)
		if err != nil {
			return CGValue{}, err
		}

		// Ensure we have an addressable pointer to the source
		var sourcePtr value.Value
		if leftCG.IsAddress {
			sourcePtr = leftCG.V
		} else {
			alloc := c.currentBlock.NewAlloca(c.llvmTypeFor(leftCG.Type))
			c.currentBlock.NewStore(leftCG.V, alloc)
			sourcePtr = alloc
		}

		// 2. Evaluate LOW bound (default to 0 if not provided)
		var lowVal value.Value
		if e.Low != nil {
			lowCG, err := c.evaluateExpression(e.Low)
			if err != nil {
				return CGValue{}, err
			}
			lowVal = c.load(lowCG)
		} else {
			lowVal = constant.NewInt(types.I32, 0)
		}

		// 3. Evaluate HIGH bound (default to array size if not provided)
		var highVal value.Value
		var originalCap value.Value

		switch t := leftCG.Type.(type) {
		case sema.ArrayType:
			// For arrays, the original capacity is exactly its fixed size
			originalCap = constant.NewInt(types.I32, int64(t.Size))
			if e.High != nil {
				highCG, err := c.evaluateExpression(e.High)
				if err != nil {
					return CGValue{}, err
				}
				highVal = c.load(highCG)
			} else {
				highVal = originalCap
			}
		case sema.SliceType:
			// For slices, we must load the existing length/capacity from the fat pointer
			// (We will implement this edge case next! For now, let's assume we are slicing an array)
			return CGValue{}, fmt.Errorf("slicing a slice is not yet implemented")
		}

		// 4. Calculate new Length (High - Low) and Capacity (OriginalCap - Low)
		newLen := c.currentBlock.NewSub(highVal, lowVal)
		newCap := c.currentBlock.NewSub(originalCap, lowVal)

		// 5. Calculate the new starting pointer using GEP!
		// We step into the array (0), then step forward by 'lowVal'
		newPtr := c.currentBlock.NewGetElementPtr(
			c.llvmTypeFor(leftCG.Type),
			sourcePtr,
			constant.NewInt(types.I32, 0),
			lowVal,
		)

		// 6. Allocate our new Slice Fat Pointer struct
		sliceLLVMType := c.llvmTypeFor(semaType)
		sliceAlloc := c.currentBlock.NewAlloca(sliceLLVMType)

		// 7. Store the Pointer (Field 0)
		ptr0 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 0))
		c.currentBlock.NewStore(newPtr, ptr0)

		// 8. Store the Length (Field 1)
		ptr1 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 1))
		c.currentBlock.NewStore(newLen, ptr1)

		// 9. Store the Capacity (Field 2)
		ptr2 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 2))
		c.currentBlock.NewStore(newCap, ptr2)

		// 10. Return the new Slice!
		return CGValue{V: sliceAlloc, Type: semaType, IsAddress: true}, nil

	}

	return CGValue{}, fmt.Errorf("unsupported expression: %T", expr)
}
