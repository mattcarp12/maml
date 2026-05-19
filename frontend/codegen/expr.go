package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/sema"
)

// evaluateExpression is now a thin dispatcher
func (c *Codegen) evaluateExpression(expr ast.Expr) (CGValue, error) {
	if expr == nil {
		return CGValue{}, nil
	}

	switch e := expr.(type) {
	case *ast.IntLiteral:
		return c.visitIntLiteral(e)
	case *ast.BoolLiteral:
		return c.visitBoolLiteral(e)
	case *ast.Identifier:
		return c.visitIdentifier(e)
	case *ast.InfixExpr:
		return c.visitInfixExpr(e)
	case *ast.PrefixExpr:
		return c.visitPrefixExpr(e)
	case *ast.IfExpr:
		return c.visitIfExpr(e)
	case *ast.MatchExpr:
		return c.visitMatchExpr(e)
	case *ast.CallExpr:
		return c.visitCallExpr(e)
	case *ast.AwaitExpr:
		return c.visitAwaitExpr(e)
	case *ast.StructLiteral:
		return c.visitStructLiteral(e)
	case *ast.FieldAccess:
		return c.visitFieldAccess(e)
	case *ast.StringLiteral:
		return c.visitStringLiteral(e)
	case *ast.ArrayLiteral:
		return c.visitArrayLiteral(e)
	case *ast.IndexExpr:
		return c.visitIndexExpr(e)
	case *ast.SliceExpr:
		return c.visitSliceExpr(e)
	default:
		return CGValue{}, fmt.Errorf("unsupported expression: %T", expr)
	}
}

// ===================================================================
// Individual Visit Methods
// ===================================================================

func (c *Codegen) visitIntLiteral(e *ast.IntLiteral) (CGValue, error) {
	semaType := c.typeMap[e]
	return CGValue{
		V:         constant.NewInt(types.I32, e.Value),
		Type:      semaType,
		IsAddress: false,
	}, nil
}

func (c *Codegen) visitBoolLiteral(e *ast.BoolLiteral) (CGValue, error) {
	semaType := c.typeMap[e]
	if e.Value {
		return CGValue{V: constant.True, Type: semaType, IsAddress: false}, nil
	}
	return CGValue{V: constant.False, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitIdentifier(e *ast.Identifier) (CGValue, error) {
	// Check bindingTypes first (pattern variable fast path).
	if bt, ok := c.bindingTypes[e.Value]; ok {
		info, exists := c.resolveVar(e.Value)
		if exists {
			return CGValue{V: info.V, Type: bt, IsAddress: true, IsHeap: info.IsHeap}, nil
		}
	}

	info, exists := c.resolveVar(e.Value)
	if !exists {
		// Could be a unit variant constructor: Point, None, etc.
		// These aren't in the var env; they're constructed on the fly.
		semaType := c.typeMap[e]
		if sumTy, ok := semaType.(*sema.SumType); ok {
			variant := sumTy.GetVariant(e.Value)
			if variant != nil && variant.IsUnit() {
				return c.emitVariantConstruct(sumTy, variant, nil)
			}
		}
		return CGValue{}, fmt.Errorf("undefined variable: %s", e.Value)
	}

	semaType := c.typeMap[e]
	if semaType == nil {
		if bt, ok := c.bindingTypes[e.Value]; ok {
			semaType = bt
		}
	}

	return CGValue{V: info.V, Type: semaType, IsAddress: true, IsHeap: info.IsHeap}, nil
}

func (c *Codegen) visitInfixExpr(e *ast.InfixExpr) (CGValue, error) {
	leftCG, err := c.evaluateExpression(e.Left)
	if err != nil {
		return CGValue{}, err
	}

	switch e.Operator {
	case "&&":
		return c.visitLogicalAnd(e, leftCG)
	case "||":
		return c.visitLogicalOr(e, leftCG)
	}

	// Non-short-circuiting operators
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
	case "%":
		res = c.currentBlock.NewSRem(left, right)
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
	default:
		return CGValue{}, fmt.Errorf("unknown infix operator: %s", e.Operator)
	}

	semaType := c.typeMap[e]
	return CGValue{V: res, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitLogicalAnd(e *ast.InfixExpr, leftCG CGValue) (CGValue, error) {
	fn := c.currentBlock.Parent
	rightBlk := fn.NewBlock(c.newLabel("and_right"))
	mergeBlk := fn.NewBlock(c.newLabel("and_merge"))

	leftVal := c.load(leftCG)
	leftExitBlk := c.currentBlock
	c.currentBlock.NewCondBr(leftVal, rightBlk, mergeBlk)

	// Right block
	c.currentBlock = rightBlk
	rightCG, _ := c.evaluateExpression(e.Right)
	rightVal := c.load(rightCG)
	rightExitBlk := c.currentBlock
	c.currentBlock.NewBr(mergeBlk)

	// Merge
	c.currentBlock = mergeBlk
	phi := mergeBlk.NewPhi(
		ir.NewIncoming(constant.False, leftExitBlk),
		ir.NewIncoming(rightVal, rightExitBlk),
	)

	semaType := c.typeMap[e]
	return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitLogicalOr(e *ast.InfixExpr, leftCG CGValue) (CGValue, error) {
	fn := c.currentBlock.Parent
	rightBlk := fn.NewBlock(c.newLabel("or_right"))
	mergeBlk := fn.NewBlock(c.newLabel("or_merge"))

	leftVal := c.load(leftCG)
	leftExitBlk := c.currentBlock
	c.currentBlock.NewCondBr(leftVal, mergeBlk, rightBlk)

	// Right block
	c.currentBlock = rightBlk
	rightCG, err := c.evaluateExpression(e.Right)
	if err != nil {
		return CGValue{}, err
	}
	rightVal := c.load(rightCG)
	rightExitBlk := c.currentBlock
	c.currentBlock.NewBr(mergeBlk)

	// Merge
	c.currentBlock = mergeBlk
	phi := mergeBlk.NewPhi(
		ir.NewIncoming(constant.True, leftExitBlk),
		ir.NewIncoming(rightVal, rightExitBlk),
	)

	semaType := c.typeMap[e]
	return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitPrefixExpr(e *ast.PrefixExpr) (CGValue, error) {
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
	default:
		return CGValue{}, fmt.Errorf("unknown prefix operator: %s", e.Operator)
	}

	semaType := c.typeMap[e]
	return CGValue{V: res, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitIfExpr(e *ast.IfExpr) (CGValue, error) {
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

	// Then branch
	c.currentBlock = thenBlk
	thenYield, err := c.compileBlockStmt(e.Consequence)
	if err != nil {
		return CGValue{}, err
	}
	if c.currentBlock.Term == nil {
		c.currentBlock.NewBr(mergeBlk)
		if thenYield != nil {
			phiIncoming = append(phiIncoming, ir.NewIncoming(thenYield, c.currentBlock))
		}
	}

	// Else branch
	c.currentBlock = elseBlk
	if e.Alternative != nil {
		elseYield, err := c.compileBlockStmt(e.Alternative)
		if err != nil {
			return CGValue{}, err
		}
		if c.currentBlock.Term == nil {
			c.currentBlock.NewBr(mergeBlk)
			if elseYield != nil {
				phiIncoming = append(phiIncoming, ir.NewIncoming(elseYield, c.currentBlock))
			}
		}
	} else {
		c.currentBlock.NewBr(mergeBlk)
	}

	// Merge block
	c.currentBlock = mergeBlk

	semaType := c.typeMap[e]
	if len(phiIncoming) > 0 {
		phi := mergeBlk.NewPhi(phiIncoming...)
		return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
	}

	return CGValue{}, nil
}

func (c *Codegen) visitCallExpr(e *ast.CallExpr) (CGValue, error) {
	// NEW: Check for container constructors
	if genType, isGen := e.Function.(*ast.GenericType); isGen {
		semaType := c.typeMap[genType]
		llvmType := c.llvmTypeFor(semaType)

		// 1. Allocate space on the stack for the header structure
		containerAlloc := c.currentBlock.NewAlloca(llvmType)

		// 2. Zero-initialize the memory block cleanly
		zero := constant.NewInt(types.I32, 0)
		nullPtr := constant.NewNull(types.I8Ptr)

		if _, isVec := semaType.(sema.VectorType); isVec {
			// Vec structural offsets: { raw_ptr, data_ptr, len, cap }
			p0 := c.currentBlock.NewGetElementPtr(llvmType, containerAlloc, zero, zero)
			c.currentBlock.NewStore(nullPtr, p0)

			p1 := c.currentBlock.NewGetElementPtr(llvmType, containerAlloc, zero, constant.NewInt(types.I32, 1))
			c.currentBlock.NewStore(constant.NewNull(types.NewPointer(c.llvmTypeFor(semaType.(sema.VectorType).Base))), p1)

			p2 := c.currentBlock.NewGetElementPtr(llvmType, containerAlloc, zero, constant.NewInt(types.I32, 2))
			c.currentBlock.NewStore(zero, p2)

			p3 := c.currentBlock.NewGetElementPtr(llvmType, containerAlloc, zero, constant.NewInt(types.I32, 3))
			c.currentBlock.NewStore(zero, p3)
		} else if _, isMap := semaType.(sema.MapType); isMap {
			valSize := constant.NewInt(types.I32, int64(semaType.(sema.MapType).Value.SizeInBytes()))

			// Check if the map key type is a string
			isStrKey := constant.NewInt(types.I8, 0)
			if _, isStr := semaType.(sema.MapType).Key.(sema.StringType); isStr {
				isStrKey = constant.NewInt(types.I8, 1)
			}

			runtimeMapPtr := c.currentBlock.NewCall(c.runtimeFuncs[Maml_MapCreate], valSize, isStrKey)
			c.currentBlock.NewStore(runtimeMapPtr, containerAlloc)
			c.trackForRelease(runtimeMapPtr, true)
		}

		return CGValue{V: containerAlloc, Type: semaType, IsAddress: true, IsHeap: false}, nil
	}

	// Intercept variant constructors: Some(x), None, Ok(x), Err(e)
	if ident, ok := e.Function.(*ast.Identifier); ok {
		switch ident.Value {
		case "Some", "None", "Ok", "Err":
			return c.visitVariantConstructor(ident.Value, e)
		// NEW: Codegen for Concurrency Built-ins
		case "spawn":
			argCG, err := c.evaluateExpression(e.Arguments[0].Argument)
			if err != nil {
				return CGValue{}, err
			}

			// Declare and call the Zig runtime spawn function
			spawnTask := c.module.NewFunc("maml_spawn_task", types.Void, ir.NewParam("hdl", types.I8Ptr))
			c.currentBlock.NewCall(spawnTask, c.load(argCG))

			return CGValue{V: constant.NewInt(types.I32, 0), Type: sema.IntType{}, IsAddress: false}, nil

		case "run_executor":
			// Declare and call the Zig runtime executor loop
			runExecutor := c.module.NewFunc("maml_run_executor", types.Void)
			c.currentBlock.NewCall(runExecutor)

			return CGValue{V: constant.NewInt(types.I32, 0), Type: sema.IntType{}, IsAddress: false}, nil
		}
	}

	// NEW: Intercept methods called on containers
	if fieldAccess, ok := e.Function.(*ast.FieldAccess); ok {
		objCG, _ := c.evaluateExpression(fieldAccess.Object)
		methodName := fieldAccess.Field.Value

		if vecTy, isVec := objCG.Type.(sema.VectorType); isVec {
			switch methodName {
			case "push":
				return c.generateVectorPush(objCG, vecTy, e.Arguments[0].Argument)
			case "len":
				// Extracts index 2 directly from the vector's header value!
				vecVal := c.load(objCG)
				length := c.currentBlock.NewExtractValue(vecVal, 2)
				return CGValue{V: length, Type: sema.IntType{}, IsAddress: false}, nil
			}
		}
		if mapTy, isMap := objCG.Type.(sema.MapType); isMap {
			switch methodName {
			case "put":
				return c.generateMapPut(objCG, mapTy, e.Arguments[0].Argument, e.Arguments[1].Argument)
			case "get":
				return c.generateMapGet(objCG, mapTy, e.Arguments[0].Argument)
			}
		}
	}

	ident, isIdent := e.Function.(*ast.Identifier)
	if !isIdent {
		return CGValue{}, fmt.Errorf("unsupported function call: only direct calls to identifiers are supported")
	}

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
		return CGValue{}, fmt.Errorf("function %s not found in LLVM module", ident.Value)
	}

	// We need the function type to check parameter modes
	fnType := c.typeMap[e.Function].(*sema.FunctionType)

	var llvmArgs []value.Value
	for i, arg := range e.Arguments {
		argCG, err := c.evaluateExpression(arg.Argument)
		if err != nil {
			return CGValue{}, err
		}
		val := c.load(argCG)

		// Special case for puts + string
		if ident.Value == "puts" && i == 0 {
			if _, ok := argCG.Type.(sema.StringType); ok {
				val = c.currentBlock.NewExtractValue(val, 0)
			}
		}

		// NEW: Ownership Transfer!
		// If we are passing this argument as 'own' and it's currently on the heap,
		// we must stop tracking it. The callee now owns the responsibility to release it.
		// (We only check custom functions, ignore puts which is C-interop)
		if ident.Value != "puts" && fnType.ParamModes[i] == sema.ParamOwned && argCG.IsHeap {
			rawPtr := c.getRawHeapPointer(argCG)
			c.untrackFromRelease(rawPtr)
		}

		llvmArgs = append(llvmArgs, val)
	}

	res := c.currentBlock.NewCall(targetFunc, llvmArgs...)
	semaType := c.typeMap[e]

	// If the function returns a reference type OR a composite type
	// (Struct/Array), we treat its internal data as heap-managed because it
	// survived the callee's frame.
	isHeap := false
	if semaType != nil {
		isHeap = semaType.IsReferenceType() || c.isCompositeType(semaType)
	}

	return CGValue{V: res, Type: semaType, IsAddress: false, IsHeap: isHeap}, nil
}

func (c *Codegen) visitVariantConstructor(name string, e *ast.CallExpr) (CGValue, error) {
	// 1. Retrieve the under-the-hood SumType resolved by sema
	sumTy, ok := c.typeMap[e].(*sema.SumType)
	if !ok {
		return CGValue{}, fmt.Errorf("expected sum type for variant constructor %s", name)
	}

	// 2. Find the target variant description (e.g., "Some", "None")
	variant := sumTy.GetVariant(name)

	// 3. Map positional CallExpr arguments to named fields expected by emitVariantConstruct
	var fields []ast.StructField
	for i, arg := range e.Arguments {
		fields = append(fields, ast.StructField{
			Name:  &ast.Identifier{Value: variant.Fields[i].Name},
			Value: arg.Argument,
		})
	}

	// 4. Delegate entirely to your unified tagged-union code generator!
	return c.emitVariantConstruct(sumTy, variant, fields)
}

// zeroValue returns the LLVM zero constant for a given sema type.
// Used to zero-initialize unused union fields.
func (c *Codegen) zeroValue(t sema.Type) value.Value {
	switch t.(type) {
	case sema.IntType:
		return constant.NewInt(types.I32, 0)
	case sema.BoolType:
		return constant.False
	case sema.StringType:
		// { i8* ptr, i32 len } — zero both fields via a zero struct constant.
		return constant.NewZeroInitializer(c.llvmTypeFor(t))
	case sema.UnknownType:
		// Fallback for None/Err where inner type is not yet resolved.
		return constant.NewInt(types.I32, 0)
	default:
		return constant.NewZeroInitializer(c.llvmTypeFor(t))
	}
}

func (c *Codegen) visitStructLiteral(e *ast.StructLiteral) (CGValue, error) {
	semaType := c.typeMap[e]

	// If sema classified this as a SumType, it's a variant constructor.
	if sumTy, ok := semaType.(*sema.SumType); ok {
		return c.visitVariantLiteralFromStruct(e, sumTy)
	}

	// Original struct literal path (unchanged).
	llvmType := c.llvmTypeFor(semaType)
	structPtr := c.currentBlock.NewAlloca(llvmType)
	for _, field := range e.Fields {
		valCG, err := c.evaluateExpression(field.Value)
		if err != nil {
			return CGValue{}, err
		}
		val := c.load(valCG)
		index := semaType.(*sema.StructType).GetFieldIndex(field.Name.Value)
		fieldPtr := c.currentBlock.NewGetElementPtr(llvmType, structPtr,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(index)))
		c.currentBlock.NewStore(val, fieldPtr)
	}
	return CGValue{V: structPtr, Type: semaType, IsAddress: true, IsHeap: false}, nil
}

func (c *Codegen) visitVariantLiteralFromStruct(e *ast.StructLiteral, sumTy *sema.SumType) (CGValue, error) {
	variant := sumTy.GetVariant(e.Type.Value)
	if variant == nil {
		return CGValue{}, fmt.Errorf("unknown variant '%s' on type '%s'", e.Type.Value, sumTy.BaseName)
	}
	return c.emitVariantConstruct(sumTy, variant, e.Fields)
}

func (c *Codegen) visitFieldAccess(e *ast.FieldAccess) (CGValue, error) {
	objCG, err := c.evaluateExpression(e.Object)
	if err != nil {
		return CGValue{}, err
	}

	structType := objCG.Type.(*sema.StructType)
	index := structType.GetFieldIndex(e.Field.Value)

	fieldPtr := c.currentBlock.NewGetElementPtr(c.llvmTypeFor(structType), objCG.V,
		constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(index)))

	semaType := c.typeMap[e]
	return CGValue{V: fieldPtr, Type: semaType, IsAddress: true}, nil
}

func (c *Codegen) visitStringLiteral(e *ast.StringLiteral) (CGValue, error) {
	strVal := e.Value
	strLen := len(strVal)
	strBytes := append([]byte(strVal), 0)

	strConst := constant.NewCharArray(strBytes)
	globalDef := c.module.NewGlobalDef(c.newLabel("str"), strConst)
	globalDef.Immutable = true

	semaType := c.typeMap[e]
	strLLVMType := c.llvmTypeFor(semaType)

	// String headers are ALWAYS stack allocated. Backing array is global.
	structAlloc := c.currentBlock.NewAlloca(strLLVMType)

	zero := constant.NewInt(types.I32, 0)
	charPtr := c.currentBlock.NewGetElementPtr(
		types.NewArray(uint64(len(strBytes)), types.I8),
		globalDef, zero, zero,
	)

	p0 := c.currentBlock.NewGetElementPtr(strLLVMType, structAlloc, zero, zero)
	c.currentBlock.NewStore(charPtr, p0)

	p1 := c.currentBlock.NewGetElementPtr(strLLVMType, structAlloc, zero, constant.NewInt(types.I32, 1))
	c.currentBlock.NewStore(constant.NewInt(types.I32, int64(strLen)), p1)

	return CGValue{V: structAlloc, Type: semaType, IsAddress: true, IsHeap: false}, nil
}

func (c *Codegen) visitArrayLiteral(e *ast.ArrayLiteral) (CGValue, error) {
	semaType := c.typeMap[e]
	arrType := c.llvmTypeFor(semaType)

	isHeap := c.escapeMap[e] == escape.StateHeap
	var arrayAlloc value.Value

	if isHeap {
		// HEAP ALLOCATION
		sizeVal := constant.NewInt(types.I64, int64(semaType.SizeInBytes()))
		rawHeapPtr := c.currentBlock.NewCall(c.runtimeFuncs[Maml_Alloc], sizeVal)
		arrayAlloc = c.currentBlock.NewBitCast(rawHeapPtr, types.NewPointer(arrType))

		// Note: We don't trackForRelease here directly for array literals;
		// the DeclareStmt/AssignStmt will handle ARC tracking when bound to a variable.
	} else {
		// STACK ALLOCATION (The fast path!)
		arrayAlloc = c.currentBlock.NewAlloca(arrType)
	}

	for i, elem := range e.Elements {
		elemCG, err := c.evaluateExpression(elem)
		if err != nil {
			return CGValue{}, err
		}
		elemVal := c.load(elemCG)

		index := constant.NewInt(types.I32, int64(i))
		elemPtr := c.currentBlock.NewGetElementPtr(arrType, arrayAlloc,
			constant.NewInt(types.I32, 0), index)
		c.currentBlock.NewStore(elemVal, elemPtr)
	}

	return CGValue{V: arrayAlloc, Type: semaType, IsAddress: true, IsHeap: isHeap}, nil
}

func (c *Codegen) visitIndexExpr(e *ast.IndexExpr) (CGValue, error) {
	leftCG, err := c.evaluateExpression(e.Left)
	if err != nil {
		return CGValue{}, err
	}

	idxCG, err := c.evaluateExpression(e.Index)
	if err != nil {
		return CGValue{}, err
	}
	idxVal := c.load(idxCG)

	semaType := c.typeMap[e]

	switch t := leftCG.Type.(type) {
	case sema.ArrayType:
		return c.visitArrayIndex(leftCG, idxVal, t, semaType)
	case sema.SliceType:
		return c.visitSliceIndex(leftCG, idxVal, t, semaType)
	case sema.StringType:
		return c.visitStringIndex(leftCG, idxVal, semaType)
	default:
		return CGValue{}, fmt.Errorf("cannot index into non-array/slice type %T", leftCG.Type)
	}
}

func (c *Codegen) visitArrayIndex(leftCG CGValue, idxVal value.Value, arrType sema.ArrayType, semaType sema.Type) (CGValue, error) {
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
	elemPtr := c.currentBlock.NewGetElementPtr(
		c.llvmTypeFor(arrType),
		arrayPtr,
		zero,
		idxVal,
	)

	return CGValue{V: elemPtr, Type: semaType, IsAddress: true}, nil
}

func (c *Codegen) visitSliceIndex(leftCG CGValue, idxVal value.Value, sliceType sema.SliceType, semaType sema.Type) (CGValue, error) {
	var sliceStructPtr value.Value
	if leftCG.IsAddress {
		sliceStructPtr = leftCG.V
	} else {
		llvmType := c.llvmTypeFor(leftCG.Type)
		alloc := c.currentBlock.NewAlloca(llvmType)
		c.currentBlock.NewStore(leftCG.V, alloc)
		sliceStructPtr = alloc
	}

	zero := constant.NewInt(types.I32, 0)
	one := constant.NewInt(types.I32, 1)
	dataPtrPtr := c.currentBlock.NewGetElementPtr(
		c.llvmTypeFor(sliceType),
		sliceStructPtr,
		zero,
		one,
	)

	baseLLVMType := c.llvmTypeFor(sliceType.Base)
	rawPointerType := types.NewPointer(baseLLVMType)
	dataPtr := c.currentBlock.NewLoad(rawPointerType, dataPtrPtr)

	elemPtr := c.currentBlock.NewGetElementPtr(
		baseLLVMType,
		dataPtr,
		idxVal,
	)

	return CGValue{V: elemPtr, Type: semaType, IsAddress: true}, nil
}

func (c *Codegen) visitStringIndex(leftCG CGValue, idxVal value.Value, semaType sema.Type) (CGValue, error) {
	var strStructPtr value.Value
	if leftCG.IsAddress {
		strStructPtr = leftCG.V
	} else {
		llvmType := c.llvmTypeFor(leftCG.Type)
		alloc := c.currentBlock.NewAlloca(llvmType)
		c.currentBlock.NewStore(leftCG.V, alloc)
		strStructPtr = alloc
	}

	zero := constant.NewInt(types.I32, 0)
	dataPtrPtr := c.currentBlock.NewGetElementPtr(
		c.llvmTypeFor(leftCG.Type),
		strStructPtr,
		zero,
		zero,
	)

	dataPtr := c.currentBlock.NewLoad(types.NewPointer(types.I8), dataPtrPtr)

	elemPtr := c.currentBlock.NewGetElementPtr(types.I8, dataPtr, idxVal)
	byteVal := c.currentBlock.NewLoad(types.I8, elemPtr)
	int32Val := c.currentBlock.NewZExt(byteVal, types.I32)

	return CGValue{V: int32Val, Type: semaType, IsAddress: false}, nil
}

func (c *Codegen) visitSliceExpr(e *ast.SliceExpr) (CGValue, error) {
	leftCG, err := c.evaluateExpression(e.Left)
	if err != nil {
		return CGValue{}, err
	}

	trueBasePtr := c.getRawHeapPointer(leftCG)

	var sourcePtr value.Value
	if leftCG.IsAddress {
		sourcePtr = leftCG.V
	} else {
		alloc := c.currentBlock.NewAlloca(c.llvmTypeFor(leftCG.Type))
		c.currentBlock.NewStore(leftCG.V, alloc)
		sourcePtr = alloc
	}

	// Low bound
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

	// High bound + capacity
	var highVal, originalCap value.Value
	switch t := leftCG.Type.(type) {
	case sema.ArrayType:
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
		return CGValue{}, fmt.Errorf("slicing a slice is not yet implemented")
	default:
		return CGValue{}, fmt.Errorf("cannot slice type %T", leftCG.Type)
	}

	newLen := c.currentBlock.NewSub(highVal, lowVal)
	newCap := c.currentBlock.NewSub(originalCap, lowVal)

	newPtr := c.currentBlock.NewGetElementPtr(
		c.llvmTypeFor(leftCG.Type),
		sourcePtr,
		constant.NewInt(types.I32, 0),
		lowVal,
	)

	semaType := c.typeMap[e]
	sliceLLVMType := c.llvmTypeFor(semaType)
	sliceAlloc := c.currentBlock.NewAlloca(sliceLLVMType)
	zero := constant.NewInt(types.I32, 0)

	// Field 0: Base pointer
	ptr0 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, zero, zero)
	c.currentBlock.NewStore(trueBasePtr, ptr0)

	// Field 1: Data pointer
	ptr1 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, zero, constant.NewInt(types.I32, 1))
	c.currentBlock.NewStore(newPtr, ptr1)

	// Field 2: Length
	ptr2 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, zero, constant.NewInt(types.I32, 2))
	c.currentBlock.NewStore(newLen, ptr2)

	// Field 3: Capacity
	ptr3 := c.currentBlock.NewGetElementPtr(sliceLLVMType, sliceAlloc, zero, constant.NewInt(types.I32, 3))
	c.currentBlock.NewStore(newCap, ptr3)

	return CGValue{V: sliceAlloc, Type: semaType, IsAddress: true, IsHeap: leftCG.IsHeap}, nil
}

func (c *Codegen) generateVectorPush(vecCG CGValue, vecTy sema.VectorType, argExpr ast.Expr) (CGValue, error) {
	llvmType := c.llvmTypeFor(vecTy)
	baseType := c.llvmTypeFor(vecTy.Base)

	zero := constant.NewInt(types.I32, 0)
	lenPtr := c.currentBlock.NewGetElementPtr(llvmType, vecCG.V, zero, constant.NewInt(types.I32, 2))
	capPtr := c.currentBlock.NewGetElementPtr(llvmType, vecCG.V, zero, constant.NewInt(types.I32, 3))

	currentLen := c.currentBlock.NewLoad(types.I32, lenPtr)
	currentCap := c.currentBlock.NewLoad(types.I32, capPtr)

	fn := c.currentBlock.Parent
	growBlk := fn.NewBlock(c.newLabel("vec_grow"))
	insertBlk := fn.NewBlock(c.newLabel("vec_insert"))

	isFull := c.currentBlock.NewICmp(enum.IPredEQ, currentLen, currentCap)
	c.currentBlock.NewCondBr(isFull, growBlk, insertBlk)

	// --- GROW BLOCK ---
	c.currentBlock = growBlk
	rawPtrPtr := c.currentBlock.NewGetElementPtr(llvmType, vecCG.V, zero, zero)
	currentRawPtr := c.currentBlock.NewLoad(types.I8Ptr, rawPtrPtr)
	itemSize := constant.NewInt(types.I32, int64(vecTy.Base.SizeInBytes()))

	// FIXED: We pass capPtr directly (which is an LLVM pointer to i32) instead of currentCap!
	newRawPtr := c.currentBlock.NewCall(c.runtimeFuncs[Maml_VecGrow], currentRawPtr, currentLen, capPtr, itemSize)

	typedDataPtr := c.currentBlock.NewBitCast(newRawPtr, types.NewPointer(baseType))

	c.currentBlock.NewStore(newRawPtr, rawPtrPtr)
	dataPtrPtr := c.currentBlock.NewGetElementPtr(llvmType, vecCG.V, zero, constant.NewInt(types.I32, 1))
	c.currentBlock.NewStore(typedDataPtr, dataPtrPtr)
	c.currentBlock.NewBr(insertBlk)

	// --- INSERT BLOCK ---
	c.currentBlock = insertBlk
	freshDataPtrPtr := c.currentBlock.NewGetElementPtr(llvmType, vecCG.V, zero, constant.NewInt(types.I32, 1))
	freshDataPtr := c.currentBlock.NewLoad(types.NewPointer(baseType), freshDataPtrPtr)
	freshLen := c.currentBlock.NewLoad(types.I32, lenPtr)

	itemCG, _ := c.evaluateExpression(argExpr)
	itemVal := c.load(itemCG)

	targetElemPtr := c.currentBlock.NewGetElementPtr(baseType, freshDataPtr, freshLen)
	c.currentBlock.NewStore(itemVal, targetElemPtr)

	newLen := c.currentBlock.NewAdd(freshLen, constant.NewInt(types.I32, 1))
	c.currentBlock.NewStore(newLen, lenPtr)

	return CGValue{}, nil
}

func (c *Codegen) generateMapPut(mapCG CGValue, mapTy sema.MapType, keyExpr, valExpr ast.Expr) (CGValue, error) {
	mapPtr := c.load(mapCG)
	keyCG, _ := c.evaluateExpression(keyExpr)
	keyVal := c.load(keyCG)

	var keyHash value.Value
	var strPtr value.Value = constant.NewNull(types.I8Ptr)
	var strLen value.Value = constant.NewInt(types.I32, 0)

	if _, isStr := keyCG.Type.(sema.StringType); isStr {
		strPtr = c.currentBlock.NewExtractValue(keyVal, 0)
		strLen = c.currentBlock.NewExtractValue(keyVal, 1)
		keyHash = c.currentBlock.NewCall(c.runtimeFuncs[Maml_StrHash], strPtr, strLen)
	} else {
		keyHash = c.currentBlock.NewZExt(keyVal, types.I64)
	}

	valCG, _ := c.evaluateExpression(valExpr)
	valVal := c.load(valCG)
	valAlloc := c.currentBlock.NewAlloca(c.llvmTypeFor(valCG.Type))
	c.currentBlock.NewStore(valVal, valAlloc)
	valRawPtr := c.currentBlock.NewBitCast(valAlloc, types.I8Ptr)

	// Forward string attributes securely
	c.currentBlock.NewCall(c.runtimeFuncs[Maml_MapPut], mapPtr, keyHash, valRawPtr, strPtr, strLen)
	return CGValue{}, nil
}

func (c *Codegen) generateMapGet(mapCG CGValue, mapTy sema.MapType, keyExpr ast.Expr) (CGValue, error) {
	mapPtr := c.load(mapCG)
	keyCG, _ := c.evaluateExpression(keyExpr)
	keyVal := c.load(keyCG)

	var keyHash value.Value
	var strPtr value.Value = constant.NewNull(types.I8Ptr)
	var strLen value.Value = constant.NewInt(types.I32, 0)

	if _, isStr := keyCG.Type.(sema.StringType); isStr {
		strPtr = c.currentBlock.NewExtractValue(keyVal, 0)
		strLen = c.currentBlock.NewExtractValue(keyVal, 1)
		keyHash = c.currentBlock.NewCall(c.runtimeFuncs[Maml_StrHash], strPtr, strLen)
	} else {
		keyHash = c.currentBlock.NewZExt(keyVal, types.I64)
	}

	// Forward string attributes securely
	rawPtr := c.currentBlock.NewCall(c.runtimeFuncs[Maml_MapGet], mapPtr, keyHash, strPtr, strLen)
	typedValPtr := c.currentBlock.NewBitCast(rawPtr, types.NewPointer(c.llvmTypeFor(mapTy.Value)))

	return CGValue{V: typedValPtr, Type: mapTy.Value, IsAddress: true}, nil
}

// emitVariantConstruct allocates a sum type tagged union and fills in the
// discriminant and payload fields for the given variant.
// fields may be nil for unit variants.
func (c *Codegen) emitVariantConstruct(
	sumTy *sema.SumType,
	variant *sema.SumVariant,
	fields []ast.StructField,
) (CGValue, error) {
	llvmType := c.llvmTypeFor(sumTy)
	alloc := c.currentBlock.NewAlloca(llvmType)
	zero := constant.NewInt(types.I32, 0)

	// Field 0: discriminant (i32)
	discrimPtr := c.currentBlock.NewGetElementPtr(llvmType, alloc, zero, zero)
	c.currentBlock.NewStore(
		constant.NewInt(types.I32, int64(variant.Discriminant)),
		discrimPtr,
	)

	// Field 1: payload ([MaxPayload x i8]) — only written if variant has fields.
	if len(variant.Fields) > 0 && len(fields) > 0 {
		payloadPtr := c.currentBlock.NewGetElementPtr(llvmType, alloc, zero,
			constant.NewInt(types.I32, 1))

		// Build a struct of the variant's fields and memcpy it into the payload.
		// Construct a named struct type for the payload to get field offsets.
		var payloadFieldTypes []types.Type
		for _, vf := range variant.Fields {
			payloadFieldTypes = append(payloadFieldTypes, c.llvmTypeFor(vf.Type))
		}
		payloadStructType := types.NewStruct(payloadFieldTypes...)

		payloadAlloc := c.currentBlock.NewAlloca(payloadStructType)

		for _, f := range fields {
			valCG, err := c.evaluateExpression(f.Value)
			if err != nil {
				return CGValue{}, err
			}
			val := c.load(valCG)

			// Find field index in variant definition.
			fieldIndex := -1
			for i, vf := range variant.Fields {
				if vf.Name == f.Name.Value {
					fieldIndex = i
					break
				}
			}
			if fieldIndex < 0 {
				return CGValue{}, fmt.Errorf("unknown field '%s' in variant '%s'",
					f.Name.Value, variant.Name)
			}

			fieldPtr := c.currentBlock.NewGetElementPtr(payloadStructType, payloadAlloc,
				zero, constant.NewInt(types.I32, int64(fieldIndex)))
			c.currentBlock.NewStore(val, fieldPtr)
		}

		// Bitcast payload struct pointer to i8* and memcpy into the union payload slot.
		srcRaw := c.currentBlock.NewBitCast(payloadAlloc, types.I8Ptr)
		dstRaw := c.currentBlock.NewBitCast(payloadPtr, types.I8Ptr)
		payloadSize := constant.NewInt(types.I32, int64(variant.PayloadSize()))
		c.emitMemcpy(dstRaw, srcRaw, payloadSize)
	}

	return CGValue{V: alloc, Type: sumTy, IsAddress: true, IsHeap: false}, nil
}

func (c *Codegen) visitAwaitExpr(e *ast.AwaitExpr) (CGValue, error) {
	// 1. Evaluate the Task/Future being awaited
	_, err := c.evaluateExpression(e.Value)
	if err != nil {
		return CGValue{}, err
	}

	fn := c.currentBlock.Parent

	// Define our Coroutine State Machine Blocks
	resumeBlk := fn.NewBlock(c.newLabel("await_resume"))
	cleanupBlk := fn.NewBlock(c.newLabel("await_cleanup"))
	suspendBlk := fn.NewBlock(c.newLabel("await_suspend"))

	// 2. Emit the Suspend Intrinsic!
	suspendResult := c.currentBlock.NewCall(
		c.runtimeFuncs[LLVM_CoroSuspend],
		constant.None,  // Dummy save token
		constant.False, // Is this the final suspend? No.
	)

	// 3. Emit the State Machine Switch
	sw := c.currentBlock.NewSwitch(suspendResult, suspendBlk)
	sw.Cases = append(sw.Cases, ir.NewCase(constant.NewInt(types.I8, 0), resumeBlk))
	sw.Cases = append(sw.Cases, ir.NewCase(constant.NewInt(types.I8, 1), cleanupBlk))

	// --- CLEANUP BLOCK ---
	c.currentBlock = cleanupBlk
	coroIdInfo, _ := c.resolveVar("__coro_id")
	coroHdlInfo, _ := c.resolveVar("__coro_hdl")

	freeMem := c.currentBlock.NewCall(c.runtimeFuncs[LLVM_CoroFree], coroIdInfo.V, coroHdlInfo.V)
	c.currentBlock.NewCall(c.runtimeFuncs[Maml_Free], freeMem)
	c.currentBlock.NewBr(suspendBlk)

	// --- SUSPEND BLOCK ---
	c.currentBlock = suspendBlk
	c.currentBlock.NewRet(coroHdlInfo.V)

	// --- RESUME BLOCK ---
	c.currentBlock = resumeBlk

	// 4. FIX: Yield a dummy value of the correct LLVM type!
	// (If we yield the taskCG.V here, LLVM panics trying to store an i8* into an i32*)
	semaType := c.typeMap[e]
	dummyVal := c.zeroValue(semaType)

	return CGValue{V: dummyVal, Type: semaType, IsAddress: false}, nil
}
