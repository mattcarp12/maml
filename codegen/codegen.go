package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/ast"
)

type Compiler struct {
	module *ir.Module

	// The current block of code we are writing LLVM instructions to
	currentBlock *ir.Block

	// Our "Symbol Table". It maps a MAML variable name (like "x")
	// to an LLVM memory pointer.
	env map[string]value.Value

	currentYield value.Value // Tracks the value of the most recent '=>' statement
}

func New() *Compiler {
	return &Compiler{
		module: ir.NewModule(),
		env:    make(map[string]value.Value),
	}
}

// Compile is the entry point. It takes the root AST node and walks the tree.
func (c *Compiler) Compile(node ast.Node) error {
	switch n := node.(type) {
	case *ast.Program:
		for _, decl := range n.Decls {
			if err := c.Compile(decl); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		// 1. Create LLVM Parameters
		var llvmParams []*ir.Param
		for _, p := range n.Params {
			// e.g., i32 %x
			llvmParams = append(llvmParams, ir.NewParam(p.Name, types.I32))
		}

		// 2. Create the LLVM function
		funcType := ir.NewFunc(n.Name, types.I32, llvmParams...)
		c.module.Funcs = append(c.module.Funcs, funcType)

		// 3. Create the entry block
		c.currentBlock = funcType.NewBlock("entry")

		// 4. THE ALLOCA TRICK: Move parameters to local stack memory
		for i, p := range n.Params {
			// Allocate stack memory: %x_ptr = alloca i32
			alloc := c.currentBlock.NewAlloca(types.I32)

			// Store the incoming parameter into that memory: store i32 %x, i32* %x_ptr
			c.currentBlock.NewStore(funcType.Params[i], alloc)

			// Save the memory pointer in our environment so `evaluateExpression` can find it!
			c.env[p.Name] = alloc
		}

		// 5. Compile the body
		if err := c.Compile(n.Body); err != nil {
			return err
		}

	case *ast.BlockStmt: // Renamed from BlockStatement
		for _, stmt := range n.Statements {
			if err := c.Compile(stmt); err != nil {
				return err
			}
		}

	case *ast.DeclareStmt:
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}

		// 1. Ask the value what LLVM type it is
		valType := val.Type()

		// 2. If it's ALREADY a pointer (like the struct memory allocated by StructLiteral),
		// we don't need to allocate new memory. Just point the variable to it!
		if _, isPtr := valType.(*types.PointerType); isPtr {
			c.env[n.Name] = val
			return nil
		}

		// 3. If it's a primitive (like an i32 from a math equation), allocate space and store it.
		alloc := c.currentBlock.NewAlloca(valType)
		c.currentBlock.NewStore(val, alloc)
		c.env[n.Name] = alloc
		return nil

	case *ast.ReturnStmt: // Renamed from ReturnStatement
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}
		c.currentBlock.NewRet(val)

	case *ast.YieldStmt:
		val, err := c.evaluateExpression(n.Value)
		if err != nil {
			return err
		}
		// Save the yielded value into compiler state so the IfExpr can grab it
		c.currentYield = val
	}

	return nil
}

// String outputs the final LLVM IR text
func (c *Compiler) String() string {
	return c.module.String()
}

// evaluateExpression now takes the new ast.Expr interface
func (c *Compiler) evaluateExpression(expr ast.Expr) (value.Value, error) {
	switch e := expr.(type) {
	case *ast.IntLiteral:
		// Convert MAML int to LLVM i32 constant
		return constant.NewInt(types.I32, e.Value), nil

	case *ast.BoolLiteral:
		if e.Value {
			return constant.True, nil
		}
		return constant.False, nil

	case *ast.Identifier:
		alloc, exists := c.env[e.Value]
		if !exists {
			return nil, fmt.Errorf("undefined variable: %s", e.Value)
		}

		// If the environment variable is an Alloca instruction...
		if allocaInst, ok := alloc.(*ir.InstAlloca); ok {
			// Check what kind of data lives inside this memory allocation
			if _, isStruct := allocaInst.ElemType.(*types.StructType); isStruct {
				// It's a struct! Return the raw pointer so GEP can use it.
				return alloc, nil
			}
			// It's a primitive (int/bool)! Load the actual value out of memory.
			return c.currentBlock.NewLoad(allocaInst.ElemType, alloc), nil
		}

		// Fallback for pointers assigned directly
		return alloc, nil

	case *ast.InfixExpr: // Renamed from InfixExpression
		// Actually check the errors returned by the recursive calls
		left, err := c.evaluateExpression(e.Left)
		if err != nil {
			return nil, err
		}

		right, err := c.evaluateExpression(e.Right)
		if err != nil {
			return nil, err
		}

		// Map MAML operators to LLVM IR instructions
		switch e.Operator {
		case "+":
			return c.currentBlock.NewAdd(left, right), nil
		case "-":
			return c.currentBlock.NewSub(left, right), nil
		case "*":
			return c.currentBlock.NewMul(left, right), nil
		case "/":
			// SDiv is "Signed Division"
			return c.currentBlock.NewSDiv(left, right), nil
		default:
			return nil, fmt.Errorf("unsupported operator: %s", e.Operator)
		}

	case *ast.IfExpr:
		// 1. Evaluate the condition (must be an i1 boolean)
		condVal, err := c.evaluateExpression(e.Condition)
		if err != nil {
			return nil, err
		}

		// Grab the parent function we are currently inside
		fn := c.currentBlock.Parent

		// 2. Create our Basic Blocks (Islands of code)
		thenBlk := fn.NewBlock("then")
		elseBlk := fn.NewBlock("else")
		mergeBlk := fn.NewBlock("merge")

		// 3. Create the conditional branch! If true -> thenBlk, If false -> elseBlk
		c.currentBlock.NewCondBr(condVal, thenBlk, elseBlk)

		// --- COMPILE THE 'THEN' BRANCH ---
		c.currentBlock = thenBlk
		c.currentYield = nil // Reset yield state
		if err := c.Compile(e.Consequence); err != nil {
			return nil, err
		}
		thenYield := c.currentYield
		thenExitBlk := c.currentBlock  // Track exactly which block we are leaving from
		c.currentBlock.NewBr(mergeBlk) // Unconditionally jump to the merge block when done

		// --- COMPILE THE 'ELSE' BRANCH ---
		c.currentBlock = elseBlk
		c.currentYield = nil
		var elseYield value.Value
		var elseExitBlk *ir.Block

		if e.Alternative != nil {
			if err := c.Compile(e.Alternative); err != nil {
				return nil, err
			}
			elseYield = c.currentYield
			elseExitBlk = c.currentBlock
			c.currentBlock.NewBr(mergeBlk)
		} else {
			// If there is no else block, just jump straight to the merge block
			c.currentBlock.NewBr(mergeBlk)
		}

		// --- THE MERGE BLOCK ---
		c.currentBlock = mergeBlk

		// 4. The Phi Node!
		// If both branches yielded a value, we must create a Phi node to select the correct one
		if thenYield != nil && elseYield != nil {
			phi := mergeBlk.NewPhi(
				ir.NewIncoming(thenYield, thenExitBlk),
				ir.NewIncoming(elseYield, elseExitBlk),
			)
			return phi, nil
		}

		// If this was just a control-flow IF with no yields, return nil
		return nil, nil

	case *ast.CallExpr:
		ident := e.Function.(*ast.Identifier).Value

		// 1. Find the target function in our LLVM module
		var targetFunc *ir.Func
		for _, f := range c.module.Funcs {
			if f.Name() == ident {
				targetFunc = f
				break
			}
		}
		if targetFunc == nil {
			return nil, fmt.Errorf("function %s not found in LLVM module", ident)
		}

		// 2. Evaluate all arguments
		var llvmArgs []value.Value
		for _, arg := range e.Arguments {
			val, err := c.evaluateExpression(arg)
			if err != nil {
				return nil, err
			}
			llvmArgs = append(llvmArgs, val)
		}

		// 3. Generate the Call instruction!
		return c.currentBlock.NewCall(targetFunc, llvmArgs...), nil

	case *ast.StructLiteral:
		structName := e.Type.Value
		llvmType := c.getLLVMStructType(structName)

		// 1. ALLOCA: Ask LLVM for a block of stack memory large enough to hold this struct
		structPtr := c.currentBlock.NewAlloca(llvmType)

		// 2. INITIALIZE FIELDS: Loop through the fields the user provided
		for _, field := range e.Fields {
			// Evaluate the value (e.g., the '10' or '10 + 2')
			val, err := c.evaluateExpression(field.Value)
			if err != nil {
				return nil, err
			}

			// Find out which slot this field belongs in
			index, err := c.getFieldIndex(structName, field.Name.Value)
			if err != nil {
				return nil, err
			}

			// 3. THE GEP: Calculate the exact memory address of this specific slot
			// We use NewGetElementPtr to step into the struct (0) and go to the field (index)
			fieldPtr := c.currentBlock.NewGetElementPtr(
				llvmType,
				structPtr,
				constant.NewInt(types.I32, 0),            // Dereference the pointer
				constant.NewInt(types.I32, int64(index)), // Select the field index
			)

			// 4. STORE: Save the value into that calculated memory address
			c.currentBlock.NewStore(val, fieldPtr)
		}

		// Return the pointer to the whole struct so it can be assigned to a variable!
		return structPtr, nil

	case *ast.FieldAccess:
		// 1. Evaluate the object (this will return the pointer we created in StructLiteral!)
		objPtr, err := c.evaluateExpression(e.Object)
		if err != nil {
			return nil, err
		}

		// (For the tracer bullet, we assume the object is our 'Point')
		structName := "Point"
		llvmType := c.getLLVMStructType(structName)

		// 2. Find the index of the field we want to read
		index, err := c.getFieldIndex(structName, e.Field.Value)
		if err != nil {
			return nil, err
		}

		// 3. THE GEP: Calculate the memory address of the field
		fieldPtr := c.currentBlock.NewGetElementPtr(
			llvmType,
			objPtr,
			constant.NewInt(types.I32, 0),
			constant.NewInt(types.I32, int64(index)),
		)

		// 4. LOAD: Read the actual value out of that memory address
		// Note: We need to tell LLVM what type of data we are pulling out (types.I32)
		return c.currentBlock.NewLoad(types.I32, fieldPtr), nil

	}

	return nil, fmt.Errorf("unsupported expression: %T", expr)
}

// getFieldIndex looks up the struct definition in your AST/Sema registry
// and returns the integer index of the requested field.
func (c *Compiler) getFieldIndex(structName string, fieldName string) (int, error) {
	// For our tracer bullet, we will hardcode the 'Point' lookup.
	// (Later, you will pull this directly from the `sema.Scope.types` map you just built!)
	if structName == "Point" {
		if fieldName == "x" {
			return 0, nil
		}
		if fieldName == "y" {
			return 1, nil
		}
	}
	return -1, fmt.Errorf("unknown field '%s' on struct '%s'", fieldName, structName)
}

// getLLVMStructType returns the LLVM memory layout for a custom type
func (c *Compiler) getLLVMStructType(structName string) types.Type {
	if structName == "Point" {
		// Point is just two 32-bit integers sitting next to each other
		return types.NewStruct(types.I32, types.I32)
	}
	return types.I32 // Fallback
}
