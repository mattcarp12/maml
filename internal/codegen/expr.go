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
		// Arithmetic needs the actual VALUES, so we 'load' them
		leftCG, _ := c.evaluateExpression(e.Left)
		rightCG, _ := c.evaluateExpression(e.Right)

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
		}
		return CGValue{V: res, Type: semaType, IsAddress: false}, nil

	case *ast.IfExpr:
		condVal, err := c.evaluateExpression(e.Condition)
		if err != nil {
			return CGValue{}, err
		}

		fn := c.currentBlock.Parent
		thenBlk := fn.NewBlock("then")
		elseBlk := fn.NewBlock("else")
		mergeBlk := fn.NewBlock("merge")

		c.currentBlock.NewCondBr(condVal.V, thenBlk, elseBlk)

		// --- COMPILE THE 'THEN' BRANCH ---
		c.currentBlock = thenBlk

		// FIX: Capture the yield directly from compileBlockStmt!
		thenYield, err := c.compileBlockStmt(e.Consequence)
		if err != nil {
			return CGValue{}, err
		}
		thenExitBlk := c.currentBlock
		c.currentBlock.NewBr(mergeBlk)

		// --- COMPILE THE 'ELSE' BRANCH ---
		c.currentBlock = elseBlk
		var elseYield value.Value
		var elseExitBlk *ir.Block

		if e.Alternative != nil {
			elseYield, err = c.compileBlockStmt(e.Alternative)
			if err != nil {
				return CGValue{}, err
			}
			elseExitBlk = c.currentBlock
			c.currentBlock.NewBr(mergeBlk)
		} else {
			c.currentBlock.NewBr(mergeBlk)
		}

		// --- THE MERGE BLOCK ---
		c.currentBlock = mergeBlk

		if thenYield != nil && elseYield != nil {
			phi := mergeBlk.NewPhi(
				ir.NewIncoming(thenYield, thenExitBlk),
				ir.NewIncoming(elseYield, elseExitBlk),
			)
			return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
		}

		return CGValue{}, nil

	case *ast.CallExpr:
		ident, isIdent := e.Function.(*ast.Identifier)
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
			argCG, _ := c.evaluateExpression(arg)
			val := c.load(argCG) // Get the pointer to the {i8*, i32} struct

			// --- THE STRING LOWERING RULE ---
			// If calling 'puts', convert the MAML Fat Pointer into a raw C pointer
			if isIdent && ident.Value == "puts" && i == 0 {
				if _, ok := argCG.Type.(sema.StringType); ok {
					stringStructType := types.NewStruct(types.I8Ptr, types.I32)

					// Get the address of field 0 (the i8*)
					ptrToRawChar := c.currentBlock.NewGetElementPtr(
						stringStructType,
						val,
						constant.NewInt(types.I32, 0), // Dereference struct
						constant.NewInt(types.I32, 0), // Access field 0
					)
					// Load the raw i8* value
					val = c.currentBlock.NewLoad(types.I8Ptr, ptrToRawChar)
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
			valCG, _ := c.evaluateExpression(field.Value)
			val := c.load(valCG) // Ensure we have the data to store

			index := semaType.(*sema.StructType).GetFieldIndex(field.Name.Value)
			fieldPtr := c.currentBlock.NewGetElementPtr(llvmType, structPtr,
				constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(index)))

			c.currentBlock.NewStore(val, fieldPtr)
		}
		// A struct literal returns an address (the pointer to the alloca)
		return CGValue{V: structPtr, Type: semaType, IsAddress: true}, nil

	case *ast.FieldAccess:
		objCG, _ := c.evaluateExpression(e.Object)
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
		globalDef := c.module.NewGlobalDef(".str", strConst)
		globalDef.Immutable = true

		// Create the MAML Fat Pointer { i8*, i32 } on the stack
		stringStructType := types.NewStruct(types.I8Ptr, types.I32)
		structAlloc := c.currentBlock.NewAlloca(stringStructType)

		zero := constant.NewInt(types.I32, 0)
		charPtr := c.currentBlock.NewGetElementPtr(
			types.NewArray(uint64(len(strBytes)), types.I8),
			globalDef, zero, zero,
		)

		// Set Field 0: Raw Pointer
		p0 := c.currentBlock.NewGetElementPtr(stringStructType, structAlloc, zero, zero)
		c.currentBlock.NewStore(charPtr, p0)

		// Set Field 1: Length (MAML specific)
		p1 := c.currentBlock.NewGetElementPtr(stringStructType, structAlloc, zero, constant.NewInt(types.I32, 1))
		c.currentBlock.NewStore(constant.NewInt(types.I32, int64(strLen)), p1)

		return CGValue{V: structAlloc, Type: semaType, IsAddress: true}, nil
	}

	return CGValue{}, fmt.Errorf("unsupported expression: %T", expr)
}
