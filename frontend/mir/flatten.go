package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

// newTemp generates a unique temporary register name for the flattened MIR.
func (b *Builder) newTemp() string {
	b.tempCount++
	return fmt.Sprintf("_t%d", b.tempCount)
}

// =============================================================================
// Expression Mappers
// =============================================================================

func (b *Builder) LowerIdentifier(e *hir.Identifier) Value {
	regName := e.Value
	if e.Symbol != nil && e.Symbol.Kind == types.VarSymbol {
		regName = b.getSymbolName(e.Symbol)
	}

	// Use the lowered type from locals if available; fallback to e.Type.
	t := e.Type
	if localType, ok := b.locals[regName]; ok {
		t = localType
	}

	// IMPLICIT LOAD: The MIR tracks a pointer (e.g., mut parameter),
	// but the AST expects the underlying value type.
	if _, isPtr := t.(types.PtrType); isPtr && e.Type != nil {
		ptrReg := &Register{Name: regName, Type: types.PtrType{}}
		return b.emitLoad(ptrReg, e.Type)
	}

	return &Register{Name: regName, Type: t}
}

func (b *Builder) LowerIntLiteral(e *hir.IntLiteral) Value {
	return &IntConstant{Value: e.Value, Type: e.Type}
}

func (b *Builder) LowerBoolLiteral(e *hir.BoolLiteral) Value {
	return &BoolConstant{Value: e.Value, Type: e.Type}
}

func (b *Builder) LowerStringLiteral(e *hir.StringLiteral) Value {
	tmp := b.newTemp()
	b.locals[tmp] = e.Type
	obj := &Register{Name: tmp, Type: e.Type}

	rawStrPtr := &StringConstant{Value: e.Value, Type: types.PtrType{}}

	b.storeField(obj, e.Type, rawStrPtr, "ptr", 0, types.PtrType{})
	b.storeField(obj, e.Type, &IntConstant{Value: int64(len(e.Value)), Type: types.I32Type{}}, "len", 1, types.I32Type{})
	b.storeField(obj, e.Type, &BoolConstant{Value: false, Type: types.BoolType{}}, "is_owned", 2, types.BoolType{})

	return obj
}

func (b *Builder) LowerInfixExpr(e *hir.InfixExpr) Value {
	flatLeft := hir.LowerNode(e.Left, b)
	flatRight := hir.LowerNode(e.Right, b)

	if hir.TypeOf(e.Left).Equals(types.StringType{}) && (e.Operator == "==" || e.Operator == "!=") {
		return b.flattenStringEq(e, flatLeft, flatRight)
	}
	tmp := b.newTemp()
	return b.emit(&BinaryOpInst{
		Dst:      tmp,
		Operator: e.Operator,
		Left:     flatLeft,
		Right:    flatRight,
		Type:     e.Type,
	}, tmp, e.Type)
}

func (b *Builder) LowerPrefixExpr(e *hir.PrefixExpr) Value {
	flatRight := hir.LowerNode(e.Right, b)
	tmp := b.newTemp()
	return b.emit(&UnaryOpInst{
		Dst:      tmp,
		Operator: e.Operator,
		Operand:  flatRight,
		Type:     e.Type,
	}, tmp, e.Type)
}

func (b *Builder) LowerIndexExpr(e *hir.IndexExpr) Value {
	ptrVal := b.addressOf(e)

	isString := false
	loadType := e.Type
	if _, ok := hir.TypeOf(e.Left).(types.StringType); ok {
		isString = true
		loadType = types.U8Type{}
	}

	rawVal := b.emitLoad(ptrVal, loadType)

	if isString {
		castTmp := b.newTemp()
		return b.emit(&CastInst{Dst: castTmp, Src: rawVal, Type: e.Type}, castTmp, e.Type)
	}

	return rawVal
}

func (b *Builder) LowerFieldAccess(e *hir.FieldAccess) Value {
	ptrVal := b.addressOf(e)
	return b.emitLoad(ptrVal, e.Type)
}

func (b *Builder) LowerSliceExpr(e *hir.SliceExpr) Value {
	basePtr := b.addressOf(e.Left)
	sourceType := hir.TypeOf(e.Left)

	var origPtr Value
	var origLen Value
	var elemType types.Type

	switch t := sourceType.(type) {
	case types.StringType:
		origPtr = b.loadField(basePtr, sourceType, "ptr", 0, types.PtrType{})
		origLen = b.loadField(basePtr, sourceType, "len", 1, types.I64Type{})
		elemType = types.U8Type{}
	case *types.ViewType:
		origPtr = b.loadField(basePtr, sourceType, "ptr", 0, types.PtrType{})
		origLen = b.loadField(basePtr, sourceType, "len", 1, types.I64Type{})
		elemType = t.Base
	case *types.VectorType:
		origPtr = b.loadField(basePtr, sourceType, "buffer", 0, types.PtrType{})
		origLen = b.loadField(basePtr, sourceType, "len", 2, types.I64Type{})
		elemType = t.Base
	case *types.ArrayType:
		origPtr = basePtr
		origLen = &IntConstant{Value: int64(t.Size), Type: types.I64Type{}}
		elemType = t.Base
	default:
		panic("Unsupported slice container type in MIR builder")
	}

	var lowVal Value
	if e.Low != nil {
		lowVal = hir.LowerNode(e.Low, b)
	} else {
		lowVal = &IntConstant{Value: 0, Type: types.I64Type{}}
	}

	var highVal Value
	if e.High != nil {
		highVal = hir.LowerNode(e.High, b)
	} else {
		highVal = origLen
	}

	newLenTmp := b.newTemp()
	newLen := b.emit(&BinaryOpInst{
		Dst:      newLenTmp,
		Left:     highVal,
		Operator: "-",
		Right:    lowVal,
		Type:     types.I64Type{},
	}, newLenTmp, types.I64Type{})

	newDataPtrTmp := b.newTemp()
	newDataPtr := b.emit(&IndexAddrInst{
		Dst:        newDataPtrTmp,
		Source:     origPtr,
		SourceType: types.PtrType{},
		Index:      lowVal,
		Type:       elemType,
	}, newDataPtrTmp, types.PtrType{})

	resultTmp := b.newTemp()
	b.locals[resultTmp] = e.Type
	result := &Register{Name: resultTmp, Type: e.Type}

	b.storeField(result, e.Type, newDataPtr, "ptr", 0, types.PtrType{})
	b.storeField(result, e.Type, newLen, "len", 1, types.I64Type{})

	if _, isStr := e.Type.(types.StringType); isStr {
		b.storeField(result, e.Type, &BoolConstant{Value: false, Type: types.BoolType{}}, "is_owned", 2, types.BoolType{})
	}

	return result
}

func (b *Builder) LowerVariantDiscriminantExpr(e *hir.VariantDiscriminantExpr) Value {
	basePtr := b.addressOf(e.Object)
	return b.loadField(basePtr, hir.TypeOf(e.Object), "discriminant", 0, types.I32Type{})
}

func (b *Builder) LowerVariantReadExpr(e *hir.VariantReadExpr) Value {
	basePtr := b.addressOf(e.Object)
	payloadArrPtr := b.emitFieldAddr(basePtr, hir.TypeOf(e.Object), "payload", 1, types.UnknownType{})

	variantStructTy := b.getVariantPayloadStructType(hir.TypeOf(e.Object), e.VariantName)

	castTmp := b.newPtrTemp()
	castPtr := b.emit(&BitcastPtrInst{Dst: castTmp, Src: payloadArrPtr, Type: types.PtrType{}}, castTmp, types.PtrType{})

	return b.loadField(castPtr, variantStructTy, fmt.Sprintf("payload_%d", e.FieldIndex), e.FieldIndex, e.Type)
}

func (b *Builder) LowerStructLiteral(e *hir.StructLiteral) Value {
	if structType, isStruct := e.Type.(*types.StructType); isStruct {
		tmp := b.newTemp()
		b.locals[tmp] = structType
		obj := &Register{Name: tmp, Type: structType}

		for _, field := range e.Fields {
			flatVal := hir.LowerNode(field.Value, b)

			fieldName := field.Key.Value
			fieldIndex := structType.GetFieldIndex(fieldName)
			if fieldIndex == -1 {
				fieldIndex = 0
			}
			fieldType := getValueType(flatVal)

			b.storeField(obj, structType, flatVal, fieldName, fieldIndex, fieldType)
		}
		return obj
	}

	tmp := b.newTemp()
	t := e.Type
	if t == nil {
		t = types.UnknownType{}
	}

	b.locals[tmp] = t
	return &Register{Name: tmp, Type: t}
}

func (b *Builder) LowerIfExpr(expr *hir.IfExpr) Value {
	flatCond := hir.LowerNode(expr.Condition, b)

	thenBlock := b.newBlock()
	mergeBlock := b.newBlock()
	elseBlock := mergeBlock

	if expr.Alternative != nil {
		elseBlock = b.newBlock()
	}

	isUnit := false
	if _, ok := expr.Type.(types.UnitType); ok {
		isUnit = true
	}

	var resultTemp string
	var resultReg *Register

	if !isUnit {
		resultTemp = b.newTemp()
		b.locals[resultTemp] = expr.Type
		resultReg = &Register{Name: resultTemp, Type: expr.Type}
	} else {
		resultReg = &Register{Name: "_unit", Type: types.UnitType{}}
	}

	b.current.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  thenBlock.ID,
		FalseTarget: elseBlock.ID,
	}

	// --- 1. Process 'Then' Branch ---
	b.current = thenBlock
	thenVal := b.LowerBlockStmt(expr.Consequence)
	thenEnd := b.current

	isMergeReachable := false

	if thenEnd != nil {
		if !isUnit {
			thenEnd.Statements = append(thenEnd.Statements, &AssignInst{Dst: resultTemp, RValue: thenVal})
		}
		if thenEnd.Terminator == nil {
			thenEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
		}
		isMergeReachable = true
	}

	// --- 2. Process 'Else' Branch (If Present) ---
	if expr.Alternative != nil {
		b.current = elseBlock
		elseVal := b.LowerBlockStmt(expr.Alternative)
		elseEnd := b.current

		if elseEnd != nil {
			if !isUnit {
				elseEnd.Statements = append(elseEnd.Statements, &AssignInst{Dst: resultTemp, RValue: elseVal})
			}
			if elseEnd.Terminator == nil {
				elseEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
			isMergeReachable = true
		}
	} else {
		// If there is no 'else' branch, a false condition jumps straight
		// to the elseBlock (which is the mergeBlock), making it reachable.
		isMergeReachable = true
	}

	// --- 3. Resolve Merge Block Reachability ---
	if isMergeReachable {
		b.current = mergeBlock
	} else {
		// Both branches terminated early (e.g., via a return statement).
		// The code following this if-expression is officially unreachable.
		b.current = nil
		delete(b.graph.Blocks, mergeBlock.ID)
	}

	return resultReg
}

func (b *Builder) LowerCallExpr(e *hir.CallExpr) Value {
	if ident, ok := e.Function.(*hir.Identifier); ok {
		if handler, exists := intrinsicRegistry[ident.Value]; exists {
			return handler(b, e)
		}
	}

	flatFunc := hir.LowerNode(e.Function, b)
	var flatArgs []Value
	var argConsumed []bool

	for _, arg := range e.Arguments {
		// No capability annotation → pass the evaluated expression directly
		if arg.Cap == types.CapNone || arg.Cap == "" {
			flatArgs = append(flatArgs, hir.LowerNode(arg.Argument, b))
			argConsumed = append(argConsumed, false)
			continue
		}

		argType := hir.TypeOf(arg.Argument)
		resultType := lowerParamType(argType, arg.Cap)

		// Allocate a temporary of the ABI-correct type (PtrType for mut/heap-ro,
		// or the concrete type for own/small-ro).
		argTmp := b.newTemp()
		b.locals[argTmp] = resultType

		if arg.Cap == types.CapMut {
			// Mutability always requires indirection: take the address and store it.
			ptrVal := b.addressOf(arg.Argument)
			b.push(&AssignInst{Dst: argTmp, RValue: ptrVal})
		} else {
			// CapRo or CapOwn: lower the expression and transfer into the temporary.
			flatArg := hir.LowerNode(arg.Argument, b)
			if srcReg, ok := flatArg.(*Register); ok {
				b.emitCapTransfer(argTmp, srcReg.Name, arg.Cap)
			} else {
				// Literals / constants
				b.push(&AssignInst{Dst: argTmp, RValue: flatArg})
			}
		}

		flatArgs = append(flatArgs, &Register{Name: argTmp, Type: resultType})
		argConsumed = append(argConsumed, arg.Cap == types.CapOwn)
	}

	tmp := b.emitTemp(e.Type)
	return b.emit(&CallInst{
		Dst:         tmp,
		Function:    flatFunc,
		Arguments:   flatArgs,
		ArgConsumed: argConsumed,
		Type:        e.Type,
	}, tmp, e.Type)
}

func (b *Builder) LowerArrayLiteral(e *hir.ArrayLiteral) Value {
	arrayType, ok := e.Type.(*types.ArrayType)
	if !ok {
		tmp := b.emitTemp(e.Type)
		return &Register{Name: tmp, Type: e.Type}
	}

	tmp := b.newTemp()
	b.locals[tmp] = arrayType
	obj := &Register{Name: tmp, Type: arrayType}

	for i, elem := range e.Elements {
		flatVal := hir.LowerNode(elem, b)
		elemType := getValueType(flatVal)

		ptrTmp := b.newPtrTemp()
		ptr := b.emit(&IndexAddrInst{
			Dst:        ptrTmp,
			Source:     obj,
			SourceType: arrayType,
			Index:      &IntConstant{Value: int64(i), Type: types.I64Type{}},
			Type:       elemType,
		}, ptrTmp, types.PtrType{})

		b.push(&StoreInst{DstPtr: ptr, Value: flatVal, Type: elemType})
	}

	return obj
}

func (b *Builder) LowerVariantLiteral(e *hir.VariantLiteral) Value {
	tmp := b.newTemp()
	b.locals[tmp] = e.Type

	var flatPayloads []Value
	if len(e.Arguments) > 0 {
		for _, arg := range e.Arguments {
			flatPayloads = append(flatPayloads, hir.LowerNode(arg, b))
		}
	} else if len(e.Fields) > 0 {
		givenFields := make(map[string]hir.Expr, len(e.Fields))
		for _, f := range e.Fields {
			givenFields[f.Name] = f.Value
		}
		for _, field := range e.Variant.Fields {
			if val, exists := givenFields[field.Name]; exists {
				flatPayloads = append(flatPayloads, hir.LowerNode(val, b))
			} else {
				panic(fmt.Sprintf("Missing field '%s' for variant '%s'", field.Name, e.Variant.Name))
			}
		}
	}

	b.emitVariantInit(b.current, tmp, e.Type, e.Variant.Name, e.Variant.Discriminant, flatPayloads)

	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) flattenStringEq(e *hir.InfixExpr, flatLeft, flatRight Value) Value {
	leftTmp := b.newTemp()
	b.emit(&AssignInst{Dst: leftTmp, RValue: flatLeft}, leftTmp, types.StringType{})
	rightTmp := b.newTemp()
	b.emit(&AssignInst{Dst: rightTmp, RValue: flatRight}, rightTmp, types.StringType{})
	leftPtr := b.emitBorrow(leftTmp, false)
	rightPtr := b.emitBorrow(rightTmp, false)
	callVal := b.EmitMamlStrEq(leftPtr, rightPtr)

	boolTmp := b.newTemp()
	result := b.emit(&BinaryOpInst{
		Dst:      boolTmp,
		Operator: "!=",
		Left:     callVal,
		Right:    &IntConstant{Value: 0, Type: types.I64Type{}},
		Type:     types.BoolType{},
	}, boolTmp, types.BoolType{})

	if e.Operator == "!=" {
		notTmp := b.newTemp()
		result = b.emit(&UnaryOpInst{
			Dst:      notTmp,
			Operator: "!",
			Operand:  result,
			Type:     types.BoolType{},
		}, notTmp, types.BoolType{})
	}

	return result
}

// Given an expression that can appear on the left-hand side of an assignment,
// return a pointer to where that object lives.
func (b *Builder) addressOf(expr hir.Expr) Value {
	switch e := expr.(type) {
	case *hir.Identifier:
		regName := e.Value
		if e.Symbol != nil {
			regName = b.getSymbolName(e.Symbol)
		}
		if _, isPtr := b.locals[regName].(types.PtrType); isPtr {
			return &Register{Name: regName, Type: types.PtrType{}}
		}
		return b.emitBorrow(regName, true)

	case *hir.FieldAccess:
		basePtr := b.addressOf(e.Object)
		objType := hir.TypeOf(e.Object)
		fieldIndex := -1
		if st, ok := objType.(*types.StructType); ok {
			fieldIndex = st.GetFieldIndex(e.Field.Value)
		}
		return b.emitFieldAddr(basePtr, objType, e.Field.Value, fieldIndex, e.Type)

	case *hir.IndexExpr:
		basePtr := b.addressOf(e.Left)
		sourceType := hir.TypeOf(e.Left)
		switch sourceType.(type) {
		case types.StringType, *types.ViewType:
			rawDataPtr := b.loadField(basePtr, sourceType, "ptr", 0, types.PtrType{})

			var elemType types.Type = types.U8Type{}
			if view, isView := sourceType.(*types.ViewType); isView {
				elemType = view.Base
			}
			idxVal := hir.LowerNode(e.Index, b)

			elemPtrTmp := b.newPtrTemp()
			return b.emit(&IndexAddrInst{
				Dst:        elemPtrTmp,
				Source:     rawDataPtr,
				SourceType: types.PtrType{},
				Index:      idxVal,
				Type:       elemType,
			}, elemPtrTmp, types.PtrType{})
		}

		elemType := e.Type
		idxVal := hir.LowerNode(e.Index, b)
		ptrTmp := b.newPtrTemp()
		return b.emit(&IndexAddrInst{
			Dst:        ptrTmp,
			Source:     basePtr,
			SourceType: sourceType,
			Index:      idxVal,
			Type:       elemType,
		}, ptrTmp, types.PtrType{})

	case *hir.VecReadExpr:
		vecPtrVal := b.addressOf(e.Vec)
		flatIdx := hir.LowerNode(e.Index, b)
		return b.EmitMamlVecGet(vecPtrVal, flatIdx)

	default:
		panic(fmt.Sprintf("MIR Builder Error: Expression cannot be used as an L-Value: %T", expr))
	}
}

// emitExtractString reads the ptr/len fields out of a String value, returning
// each as a loaded Value.
func (b *Builder) emitExtractString(strReg Value) (ptrVal, lenVal Value) {
	strType := getValueType(strReg)
	ptrVal = b.loadField(strReg, strType, "ptr", 0, types.PtrType{})
	lenVal = b.loadField(strReg, strType, "len", 1, types.U32Type{})
	return ptrVal, lenVal
}

func (b *Builder) getVariantPayloadStructType(t types.Type, variantName string) *types.StructType {
	sumTy, ok := t.(*types.SumType)
	if !ok {
		panic("Expected SumType for getVariantPayloadStructType")
	}

	var targetVariant *types.SumVariant
	for _, v := range sumTy.Variants {
		if v.Name == variantName {
			targetVariant = &v
			break
		}
	}
	if targetVariant == nil {
		panic(fmt.Sprintf("Variant %s not found in sum type", variantName))
	}

	var fields []types.StructField
	idx := 0

	for _, tt := range targetVariant.TupleTypes {
		fields = append(fields, types.StructField{
			Name: fmt.Sprintf("payload_%d", idx),
			Type: tt,
		})
		idx++
	}

	for _, f := range targetVariant.Fields {
		fields = append(fields, types.StructField{
			Name: fmt.Sprintf("payload_%d", idx),
			Type: f.Type,
		})
		idx++
	}

	return &types.StructType{
		Name:   fmt.Sprintf("%s_%s_Payload", sumTy.MangledName(), variantName),
		Fields: fields,
	}
}

// emitVariantInit writes a variant's discriminant and payload fields into the
// local named dst, on the given block. This is the single implementation of
// sum-type construction; both MapVariantLiteral (building on b.current) and
// callers that need to build a variant on an arbitrary block (e.g. the
// Some/None construction in MapMapReadExpr) route through here so the two
// don't drift out of sync with each other.
func (b *Builder) emitVariantInit(block *BasicBlock, dst string, sumType types.Type, variantName string, discriminant int, payloads []Value) {
	prevCurrent := b.current
	b.current = block
	defer func() { b.current = prevCurrent }()

	basePtr := b.emitBorrow(dst, true)

	b.storeField(basePtr, sumType, &IntConstant{Value: int64(discriminant), Type: types.I32Type{}}, "discriminant", 0, types.I32Type{})

	if len(payloads) == 0 {
		return
	}

	payloadArrPtr := b.emitFieldAddr(basePtr, sumType, "payload", 1, types.UnknownType{})
	variantStructTy := b.getVariantPayloadStructType(sumType, variantName)

	castTmp := b.newPtrTemp()
	castPtr := b.emit(&BitcastPtrInst{Dst: castTmp, Src: payloadArrPtr, Type: types.PtrType{}}, castTmp, types.PtrType{})

	for i, pVal := range payloads {
		pType := getValueType(pVal)
		b.storeField(castPtr, variantStructTy, pVal, fmt.Sprintf("payload_%d", i), i, pType)
	}
}

// =============================================================================
// Intrinsic Compiler Functions (Built-ins)
// =============================================================================

type intrinsicHandler func(b *Builder, e *hir.CallExpr) Value

var intrinsicRegistry map[string]intrinsicHandler

func init() {
	intrinsicRegistry = map[string]intrinsicHandler{
		"yield_now":    (*Builder).lowerYieldNow,
		"run_executor": (*Builder).lowerRunExecutor,
		"len":          (*Builder).lowerLen,
		"delete":       (*Builder).lowerDelete,
	}
}

func (b *Builder) lowerLen(e *hir.CallExpr) Value {
	arg := e.Arguments[0].Argument
	argType := hir.TypeOf(arg)
	ptrReg := b.addressOf(arg)
	switch argType.(type) {
	case *types.VectorType, *types.ViewType:
		return b.EmitMamlVecLen(ptrReg)
	case *types.MapType:
		return b.EmitMamlMapLen(ptrReg)
	case types.StringType, *types.StringType:
		return b.EmitMamlStrLen(ptrReg)
	default:
		panic("compiler error: unhandled type passed to intrinsic len()")
	}
}
