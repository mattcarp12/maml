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

func (c *Codegen) evaluateExpression(expr ast.Expr) (value.Value, error) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		return constant.NewInt(types.I32, e.Value), nil

	case *ast.BoolLiteral:
		if e.Value {
			return constant.True, nil
		}
		return constant.False, nil

	case *ast.Identifier:
		alloc, exists := c.resolveVar(e.Value)
		if !exists {
			return nil, fmt.Errorf("undefined variable: %s", e.Value)
		}

		if allocaInst, ok := alloc.(*ir.InstAlloca); ok {
			if _, isStruct := allocaInst.ElemType.(*types.StructType); isStruct {
				return alloc, nil
			}
			return c.currentBlock.NewLoad(allocaInst.ElemType, alloc), nil
		}

		return alloc, nil

	case *ast.InfixExpr:
		left, err := c.evaluateExpression(e.Left)
		if err != nil {
			return nil, err
		}
		right, err := c.evaluateExpression(e.Right)
		if err != nil {
			return nil, err
		}

		switch e.Operator {
		case "+":
			return c.currentBlock.NewAdd(left, right), nil
		case "-":
			return c.currentBlock.NewSub(left, right), nil
		case "*":
			return c.currentBlock.NewMul(left, right), nil
		case "/":
			return c.currentBlock.NewSDiv(left, right), nil
		default:
			return nil, fmt.Errorf("unsupported operator: %s", e.Operator)
		}

	case *ast.IfExpr:
		condVal, err := c.evaluateExpression(e.Condition)
		if err != nil {
			return nil, err
		}

		fn := c.currentBlock.Parent
		thenBlk := fn.NewBlock("then")
		elseBlk := fn.NewBlock("else")
		mergeBlk := fn.NewBlock("merge")

		c.currentBlock.NewCondBr(condVal, thenBlk, elseBlk)

		// --- COMPILE THE 'THEN' BRANCH ---
		c.currentBlock = thenBlk

		// FIX: Capture the yield directly from compileBlockStmt!
		thenYield, err := c.compileBlockStmt(e.Consequence)
		if err != nil {
			return nil, err
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
				return nil, err
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
			return phi, nil
		}

		return nil, nil

	case *ast.CallExpr:
		ident := e.Function.(*ast.Identifier).Value
		var targetFunc *ir.Func

		if ident == "puts" {
			targetFunc = c.getPutsFunc()
		} else {
			for _, f := range c.module.Funcs {
				if f.Name() == ident {
					targetFunc = f
					break
				}
			}
		}

		if targetFunc == nil {
			return nil, fmt.Errorf("function %s not found in LLVM module", ident)
		}

		var llvmArgs []value.Value
		for _, arg := range e.Arguments {
			val, err := c.evaluateExpression(arg)
			if err != nil {
				return nil, err
			}
			llvmArgs = append(llvmArgs, val)
		}

		if ident == "puts" {
			stringStructPtr := llvmArgs[0]
			dataFieldPtr := c.currentBlock.NewGetElementPtr(
				types.NewStruct(types.I8Ptr, types.I32),
				stringStructPtr,
				constant.NewInt(types.I32, 0),
				constant.NewInt(types.I32, 0),
			)
			rawCharPtr := c.currentBlock.NewLoad(types.I8Ptr, dataFieldPtr)
			llvmArgs[0] = rawCharPtr
		}

		return c.currentBlock.NewCall(targetFunc, llvmArgs...), nil

	case *ast.StructLiteral:
		// 1. Look up the true semantic type from our TypeMap!
		semaType, ok := c.typeMap[e]
		if !ok {
			return nil, fmt.Errorf("could not resolve struct type for literal in codegen")
		}

		// Ensure it's actually a struct
		structType, isStruct := semaType.(*sema.StructType)
		if !isStruct {
			return nil, fmt.Errorf("type in map is not a struct")
		}

		// 2. Ask our new helper for the exact LLVM memory layout
		llvmType := c.llvmTypeFor(structType)

		// 3. ALLOCA: Ask LLVM for a block of stack memory
		structPtr := c.currentBlock.NewAlloca(llvmType)

		// 4. INITIALIZE FIELDS
		for _, field := range e.Fields {
			val, err := c.evaluateExpression(field.Value)
			if err != nil {
				return nil, err
			}

			// Look up the exact memory slot for this specific field!
			index := structType.GetFieldIndex(field.Name.Value)
			if index < 0 {
				return nil, fmt.Errorf("unknown field '%s' on struct '%s'", field.Name.Value, structType.Name)
			}

			// Calculate the memory address
			fieldPtr := c.currentBlock.NewGetElementPtr(
				llvmType,
				structPtr,
				constant.NewInt(types.I32, 0),
				constant.NewInt(types.I32, int64(index)),
			)

			// Store the value
			c.currentBlock.NewStore(val, fieldPtr)
		}

		return structPtr, nil

	case *ast.FieldAccess:
		// 1. Evaluate the object to get its pointer
		objPtr, err := c.evaluateExpression(e.Object)
		if err != nil {
			return nil, err
		}

		// 2. Ask the TypeMap what the object's type actually is
		semaType, ok := c.typeMap[e.Object]
		if !ok {
			return nil, fmt.Errorf("could not resolve object type in codegen")
		}
		structType, isStruct := semaType.(*sema.StructType)
		if !isStruct {
			return nil, fmt.Errorf("cannot access field on non-struct")
		}

		// 3. Get the LLVM type and find the field's index
		llvmType := c.llvmTypeFor(structType)
		index := structType.GetFieldIndex(e.Field.Value)
		if index < 0 {
			return nil, fmt.Errorf("unknown field '%s' on struct '%s'", e.Field.Value, structType.Name)
		}

		// Calculate the exact memory address of the field
		fieldPtr := c.currentBlock.NewGetElementPtr(
			llvmType,
			objPtr,
			constant.NewInt(types.I32, 0),
			constant.NewInt(types.I32, int64(index)),
		)

		// 4. LOAD: Read the actual value. We must tell LLVM exactly what type we are loading!
		// We use llvmTypeFor again on the specific field's type.
		fieldType := c.llvmTypeFor(structType.Fields[index].Type)
		return c.currentBlock.NewLoad(fieldType, fieldPtr), nil

	case *ast.StringLiteral:
		strLen := len(e.Value)
		strBytes := append([]byte(e.Value), 0)
		strConst := constant.NewCharArray(strBytes)
		globalDef := c.module.NewGlobalDef(".str", strConst)
		globalDef.Immutable = true

		zero := constant.NewInt(types.I32, 0)
		charPtr := c.currentBlock.NewGetElementPtr(
			types.NewArray(uint64(strLen+1), types.I8),
			globalDef,
			zero,
			zero,
		)

		stringStructType := types.NewStruct(types.I8Ptr, types.I32)
		structAlloc := c.currentBlock.NewAlloca(stringStructType)

		dataFieldPtr := c.currentBlock.NewGetElementPtr(
			stringStructType, structAlloc,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 0),
		)
		c.currentBlock.NewStore(charPtr, dataFieldPtr)

		lenFieldPtr := c.currentBlock.NewGetElementPtr(
			stringStructType, structAlloc,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 1),
		)
		c.currentBlock.NewStore(constant.NewInt(types.I32, int64(strLen)), lenFieldPtr)

		return structAlloc, nil
	}

	return nil, fmt.Errorf("unsupported expression: %T", expr)
}
