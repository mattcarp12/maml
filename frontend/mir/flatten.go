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

func (b *Builder) MapIdentifier(e *hir.Identifier) Value {
	regName := e.Value
	if e.Symbol != nil && e.Symbol.Kind == types.VarSymbol {
		regName = b.getSymbolName(e.Symbol)
	}
	return &Register{Name: regName, Type: e.Type}
}

func (b *Builder) MapIntLiteral(e *hir.IntLiteral) Value {
	return &IntConstant{Value: e.Value, Type: e.Type}
}

func (b *Builder) MapBoolLiteral(e *hir.BoolLiteral) Value {
	return &BoolConstant{Value: e.Value, Type: e.Type}
}

func (b *Builder) MapStringLiteral(e *hir.StringLiteral) Value {
	tmp := b.newTemp()
	b.locals[tmp] = e.Type

	rawStrPtr := &StringConstant{Value: e.Value, Type: types.PtrType{}}

	injectField := func(val Value, fieldName string, fieldIdx int, fieldType types.Type) {
		addrTmp := b.newTemp()
		b.locals[addrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &FieldAddrInst{
			Dst:        addrTmp,
			Object:     &Register{Name: tmp, Type: e.Type},
			ObjectType: e.Type,
			FieldName:  fieldName,
			FieldIndex: fieldIdx,
			FieldType:  fieldType,
		})

		b.current.Statements = append(b.current.Statements, &StoreInst{
			DstPtr: addrTmp,
			Value:  val,
			Type:   fieldType,
		})
	}

	injectField(rawStrPtr, "ptr", 0, types.PtrType{})
	injectField(&IntConstant{Value: int64(len(e.Value)), Type: types.I32Type{}}, "len", 1, types.I32Type{})
	injectField(&BoolConstant{Value: false, Type: types.BoolType{}}, "is_owned", 2, types.BoolType{})

	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapInfixExpr(e *hir.InfixExpr) Value {
	flatLeft := hir.MapNode(e.Left, b)
	flatRight := hir.MapNode(e.Right, b)

	if hir.TypeOf(e.Left).Equals(types.StringType{}) && (e.Operator == "==" || e.Operator == "!=") {
		return b.flattenStringEq(e, flatLeft, flatRight)
	}
	tmp := b.emitTemp(e.Type)
	b.current.Statements = append(b.current.Statements, &BinaryOpInst{
		Dst:      tmp,
		Operator: e.Operator,
		Left:     flatLeft,
		Right:    flatRight,
		Type:     e.Type,
	})
	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapPrefixExpr(e *hir.PrefixExpr) Value {
	flatRight := hir.MapNode(e.Right, b)
	tmp := b.emitTemp(e.Type)
	b.current.Statements = append(b.current.Statements, &UnaryOpInst{
		Dst:      tmp,
		Operator: e.Operator,
		Operand:  flatRight,
		Type:     e.Type,
	})
	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapIndexExpr(e *hir.IndexExpr) Value {
	ptrVal := b.flattenPlace(e)

	isString := false
	loadType := e.Type
	if _, ok := hir.TypeOf(e.Left).(types.StringType); ok {
		isString = true
		loadType = types.U8Type{}
	}

	rawTmp := b.newTemp()
	b.locals[rawTmp] = loadType
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{
		Dst:  rawTmp,
		Ptr:  ptrVal,
		Type: loadType,
	})

	if isString {
		castTmp := b.newTemp()
		b.locals[castTmp] = e.Type
		b.current.Statements = append(b.current.Statements, &CastInst{
			Dst:  castTmp,
			Src:  &Register{Name: rawTmp, Type: loadType},
			Type: e.Type,
		})
		return &Register{Name: castTmp, Type: e.Type}
	}

	return &Register{Name: rawTmp, Type: e.Type}
}

func (b *Builder) MapAwaitExpr(e *hir.AwaitExpr) Value {
	flatTask := hir.MapNode(e.Value, b)
	b.EmitMamlTaskAwait(flatTask, b.currentFuture)
	resumeBlock := b.emitCoroSuspend()
	tmp := b.newTemp()
	b.locals[tmp] = e.Type
	resultVal := b.EmitMamlTaskGetResult(flatTask)
	b.emitTransfer(tmp, resultVal)
	result := b.EmitMamlTaskGetResult(flatTask)
	resumeBlock.Statements = append(resumeBlock.Statements, &AssignInst{Dst: tmp, RValue: result})
	b.current = resumeBlock
	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapSpawnExpr(e *hir.SpawnExpr) Value {
	flatFuture := hir.MapNode(e.Value, b)
	b.EmitMamlSpawnTask(flatFuture)
	return flatFuture
}

func (b *Builder) MapFieldAccess(e *hir.FieldAccess) Value {
	ptrVal := b.flattenPlace(e)

	valTmp := b.newTemp()
	b.locals[valTmp] = e.Type
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{
		Dst:  valTmp,
		Ptr:  ptrVal,
		Type: e.Type,
	})
	return &Register{Name: valTmp, Type: e.Type}
}

func (b *Builder) MapSliceExpr(e *hir.SliceExpr) Value {
	basePtr := b.flattenPlace(e.Left)
	sourceType := hir.TypeOf(e.Left)
	basePtr = b.resolveBasePtr(basePtr, sourceType)

	var origPtr Value
	var origLen Value
	var elemType types.Type

	extractField := func(fieldName string, fieldIdx int, fieldType types.Type) Value {
		addrTmp := b.newTemp()
		b.locals[addrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &FieldAddrInst{
			Dst:        addrTmp,
			Object:     basePtr,
			ObjectType: sourceType,
			FieldName:  fieldName,
			FieldIndex: fieldIdx,
			FieldType:  fieldType,
		})

		valTmp := b.newTemp()
		b.locals[valTmp] = fieldType
		b.current.Statements = append(b.current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  &Register{Name: addrTmp, Type: types.PtrType{}},
			Type: fieldType,
		})
		return &Register{Name: valTmp, Type: fieldType}
	}

	switch t := sourceType.(type) {
	case types.StringType:
		origPtr = extractField("ptr", 0, types.PtrType{})
		origLen = extractField("len", 1, types.I64Type{})
		elemType = types.U8Type{}
	case *types.ViewType:
		origPtr = extractField("ptr", 0, types.PtrType{})
		origLen = extractField("len", 1, types.I64Type{})
		elemType = t.Base
	case *types.VectorType:
		origPtr = extractField("buffer", 0, types.PtrType{})
		origLen = extractField("len", 2, types.I64Type{})
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
		lowVal = hir.MapNode(e.Low, b)
	} else {
		lowVal = &IntConstant{Value: 0, Type: types.I64Type{}}
	}

	var highVal Value
	if e.High != nil {
		highVal = hir.MapNode(e.High, b)
	} else {
		highVal = origLen
	}

	newLenTmp := b.newTemp()
	b.locals[newLenTmp] = types.I64Type{}
	b.current.Statements = append(b.current.Statements, &BinaryOpInst{
		Dst:      newLenTmp,
		Left:     highVal,
		Operator: "-",
		Right:    lowVal,
		Type:     types.I64Type{},
	})
	newLen := &Register{Name: newLenTmp, Type: types.I64Type{}}

	newDataPtrTmp := b.newTemp()
	b.locals[newDataPtrTmp] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &IndexAddrInst{
		Dst:        newDataPtrTmp,
		Source:     origPtr,
		SourceType: types.PtrType{},
		Index:      lowVal,
		Type:       elemType,
	})
	newDataPtr := &Register{Name: newDataPtrTmp, Type: types.PtrType{}}

	resultTmp := b.newTemp()
	b.locals[resultTmp] = e.Type

	injectField := func(val Value, fieldName string, fieldIdx int, fieldType types.Type) {
		addrTmp := b.newTemp()
		b.locals[addrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &FieldAddrInst{
			Dst:        addrTmp,
			Object:     &Register{Name: resultTmp, Type: e.Type},
			ObjectType: e.Type,
			FieldName:  fieldName,
			FieldIndex: fieldIdx,
			FieldType:  fieldType,
		})

		b.current.Statements = append(b.current.Statements, &StoreInst{
			DstPtr: addrTmp,
			Value:  val,
			Type:   fieldType,
		})
	}

	injectField(newDataPtr, "ptr", 0, types.PtrType{})
	injectField(newLen, "len", 1, types.I64Type{})

	if _, isStr := e.Type.(types.StringType); isStr {
		injectField(&BoolConstant{Value: false, Type: types.BoolType{}}, "is_owned", 2, types.BoolType{})
	}

	return &Register{Name: resultTmp, Type: e.Type}
}

func (b *Builder) MapVariantDiscriminantExpr(e *hir.VariantDiscriminantExpr) Value {
	basePtr := b.flattenPlace(e.Object)
	basePtr = b.resolveBasePtr(basePtr, hir.TypeOf(e.Object))

	discrimPtr := b.newTemp()
	b.locals[discrimPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        discrimPtr,
		Object:     basePtr,
		ObjectType: hir.TypeOf(e.Object),
		FieldName:  "discriminant",
		FieldIndex: 0,
		FieldType:  types.I32Type{},
	})

	discrimVal := b.newTemp()
	b.locals[discrimVal] = types.I32Type{}
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{
		Dst:  discrimVal,
		Ptr:  &Register{Name: discrimPtr, Type: types.PtrType{}},
		Type: types.I32Type{},
	})

	return &Register{Name: discrimVal, Type: types.I32Type{}}
}

func (b *Builder) MapVariantReadExpr(e *hir.VariantReadExpr) Value {
	basePtr := b.flattenPlace(e.Object)
	basePtr = b.resolveBasePtr(basePtr, hir.TypeOf(e.Object))

	payloadArrPtr := b.newTemp()
	b.locals[payloadArrPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        payloadArrPtr,
		Object:     basePtr,
		ObjectType: hir.TypeOf(e.Object),
		FieldName:  "payload",
		FieldIndex: 1,
		FieldType:  types.UnknownType{},
	})

	variantStructTy := b.getVariantPayloadStructType(hir.TypeOf(e.Object), e.VariantName)

	castPtr := b.newTemp()
	b.locals[castPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &BitcastPtrInst{
		Dst:  castPtr,
		Src:  &Register{Name: payloadArrPtr, Type: types.PtrType{}},
		Type: types.PtrType{},
	})

	fieldPtr := b.newTemp()
	b.locals[fieldPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        fieldPtr,
		Object:     &Register{Name: castPtr, Type: types.PtrType{}},
		ObjectType: variantStructTy,
		FieldName:  fmt.Sprintf("payload_%d", e.FieldIndex),
		FieldIndex: e.FieldIndex,
		FieldType:  e.Type,
	})

	valTmp := b.newTemp()
	b.locals[valTmp] = e.Type
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{
		Dst:  valTmp,
		Ptr:  &Register{Name: fieldPtr, Type: types.PtrType{}},
		Type: e.Type,
	})

	return &Register{Name: valTmp, Type: e.Type}
}

func (b *Builder) MapMapReadExpr(e *hir.MapReadExpr) Value {
	flatMap := hir.MapNode(e.Map, b)
	hashVal, ptrVal, lenVal := b.lowerMapKey(e.Key)

	opaquePtr := b.EmitMamlMapGet(flatMap, hashVal, ptrVal, lenVal)

	resTmp := b.emitTemp(e.Type)
	cmpTmp := b.emitTemp(types.BoolType{})
	b.current.Statements = append(b.current.Statements, &BinaryOpInst{
		Dst: cmpTmp, Operator: "!=", Left: opaquePtr, Right: &IntConstant{Value: 0, Type: types.I64Type{}}, Type: types.BoolType{},
	})

	thenBlock := b.newBlock()
	elseBlock := b.newBlock()
	mergeBlock := b.newBlock()

	b.current.Terminator = &BranchTerminator{
		Condition:   &Register{Name: cmpTmp, Type: types.BoolType{}},
		TrueTarget:  thenBlock.ID,
		FalseTarget: elseBlock.ID,
	}

	valTmp := b.newTemp()
	optType := e.Type.(*types.SumType)
	valType := optType.TypeArgs[0]

	b.locals[valTmp] = valType
	thenBlock.Statements = append(thenBlock.Statements, &LoadPtrInst{Dst: valTmp, Ptr: opaquePtr, Type: valType})

	someTmp := b.newTemp()
	b.locals[someTmp] = e.Type
	b.emitVariantInit(thenBlock, someTmp, e.Type, "Some", 0, []Value{&Register{Name: valTmp, Type: valType}})
	thenBlock.Statements = append(thenBlock.Statements, &AssignInst{Dst: resTmp, RValue: &Register{Name: someTmp, Type: e.Type}})
	thenBlock.Terminator = &JumpTerminator{Target: mergeBlock.ID}

	noneTmp := b.newTemp()
	b.locals[noneTmp] = e.Type

	b.emitVariantInit(elseBlock, noneTmp, e.Type, "None", 1, nil)
	elseBlock.Statements = append(elseBlock.Statements, &AssignInst{Dst: resTmp, RValue: &Register{Name: noneTmp, Type: e.Type}})
	elseBlock.Terminator = &JumpTerminator{Target: mergeBlock.ID}

	if reg, ok := flatMap.(*Register); ok {
		mergeBlock.Statements = append(mergeBlock.Statements, &KeepAliveInst{Src: reg.Name})
	}

	b.current = mergeBlock
	return &Register{Name: resTmp, Type: e.Type}
}

func (b *Builder) MapVecReadExpr(e *hir.VecReadExpr) Value {
	flatVecPtr := hir.MapNode(e.Vec, b)
	flatIdx := hir.MapNode(e.Index, b)

	opaquePtr := b.EmitMamlVecGet(flatVecPtr, flatIdx)

	valTmp := b.newTemp()
	b.locals[valTmp] = e.Type
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{
		Dst:  valTmp,
		Ptr:  opaquePtr,
		Type: e.Type,
	})

	return &Register{Name: valTmp, Type: e.Type}
}

func (b *Builder) MapStructLiteral(e *hir.StructLiteral) Value {
	if structType, isStruct := e.Type.(*types.StructType); isStruct {
		tmp := b.newTemp()
		b.locals[tmp] = structType

		for _, field := range e.Fields {
			flatVal := hir.MapNode(field.Value, b)

			fieldName := field.Key.Value
			fieldIndex := structType.GetFieldIndex(fieldName)
			if fieldIndex == -1 {
				fieldIndex = 0
			}
			fieldType := getValueType(flatVal)

			ptrTmp := b.newTemp()
			b.locals[ptrTmp] = types.PtrType{}
			b.current.Statements = append(b.current.Statements, &FieldAddrInst{
				Dst:        ptrTmp,
				Object:     &Register{Name: tmp, Type: structType},
				ObjectType: structType,
				FieldName:  fieldName,
				FieldIndex: fieldIndex,
				FieldType:  fieldType,
			})

			b.current.Statements = append(b.current.Statements, &StoreInst{
				DstPtr: ptrTmp,
				Value:  flatVal,
				Type:   fieldType,
			})
		}
		return &Register{Name: tmp, Type: structType}
	}

	tmp := b.newTemp()
	t := e.Type
	if t == nil {
		t = types.UnknownType{}
	}

	b.locals[tmp] = t
	return &Register{Name: tmp, Type: t}
}

func (b *Builder) MapIfExpr(expr *hir.IfExpr) Value {
	flatCond := hir.MapNode(expr.Condition, b)

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

	b.current = thenBlock
	thenVal := b.MapBlockStmt(expr.Consequence)
	thenEnd := b.current
	if thenEnd != nil {
		if !isUnit {
			thenEnd.Statements = append(thenEnd.Statements, &AssignInst{Dst: resultTemp, RValue: thenVal})
		}
		if thenEnd.Terminator == nil {
			thenEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
		}
	}

	if expr.Alternative != nil {
		b.current = elseBlock
		elseVal := b.MapBlockStmt(expr.Alternative)
		elseEnd := b.current
		if elseEnd != nil {
			if !isUnit {
				elseEnd.Statements = append(elseEnd.Statements, &AssignInst{Dst: resultTemp, RValue: elseVal})
			}
			if elseEnd.Terminator == nil {
				elseEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
		}
	}

	b.current = mergeBlock
	return resultReg
}

func (b *Builder) MapCallExpr(e *hir.CallExpr) Value {
	if ident, ok := e.Function.(*hir.Identifier); ok {
		if handler, exists := intrinsicRegistry[ident.Value]; exists {
			return handler(b, e)
		}
	}

	flatFunc := hir.MapNode(e.Function, b)
	var flatArgs []Value
	for _, arg := range e.Arguments {
		flatArg := hir.MapNode(arg.Argument, b)

		if arg.Cap == types.CapNone || arg.Cap == "" {
			flatArgs = append(flatArgs, flatArg)
			continue
		}

		argTmp := b.newTemp()
		argType := hir.TypeOf(arg.Argument)

		resultType := lowerParamType(argType, arg.Cap)
		b.locals[argTmp] = resultType

		srcReg, ok := flatArg.(*Register)
		if !ok {
			panic("compiler error: capability applied to non-register value at call site")
		}

		switch arg.Cap {
		case types.CapMut:
			if isByRefType(argType) {
				// Heap types are already pointers — reuse the value directly.
				// No new owned temp is created, so no extra free will be inserted.
				flatArgs = append(flatArgs, srcReg)
				continue
			}
			b.current.Statements = append(b.current.Statements, &BorrowInst{Dst: argTmp, Src: srcReg.Name, IsMut: true})
		case types.CapRo:
			if isByRefType(argType) {
				flatArgs = append(flatArgs, srcReg)
				continue
			}
			b.current.Statements = append(b.current.Statements, &BorrowInst{Dst: argTmp, Src: srcReg.Name, IsMut: false})
		case types.CapOwn:
			b.current.Statements = append(b.current.Statements, &MoveInst{Dst: argTmp, Src: srcReg.Name})
		case types.CapCopy:
			b.current.Statements = append(b.current.Statements, &CopyInst{Dst: argTmp, Src: srcReg.Name})
		}

		flatArgs = append(flatArgs, &Register{Name: argTmp, Type: resultType})
	}

	tmp := b.emitTemp(e.Type)
	b.current.Statements = append(b.current.Statements, &CallInst{
		Dst:       tmp,
		Function:  flatFunc,
		Arguments: flatArgs,
		Type:      e.Type,
	})
	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapArrayLiteral(e *hir.ArrayLiteral) Value {
	arrayType, ok := e.Type.(*types.ArrayType)
	if !ok {
		tmp := b.emitTemp(e.Type)
		return &Register{Name: tmp, Type: e.Type}
	}

	tmp := b.newTemp()
	b.locals[tmp] = arrayType

	for i, elem := range e.Elements {
		flatVal := hir.MapNode(elem, b)
		elemType := getValueType(flatVal)

		ptrTmp := b.newTemp()
		b.locals[ptrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &IndexAddrInst{
			Dst:        ptrTmp,
			Source:     &Register{Name: tmp, Type: arrayType},
			SourceType: arrayType,
			Index:      &IntConstant{Value: int64(i), Type: types.I64Type{}},
			Type:       elemType,
		})

		b.current.Statements = append(b.current.Statements, &StoreInst{
			DstPtr: ptrTmp,
			Value:  flatVal,
			Type:   elemType,
		})
	}

	return &Register{Name: tmp, Type: arrayType}
}

func (b *Builder) MapVariantLiteral(e *hir.VariantLiteral) Value {
	tmp := b.newTemp()
	b.locals[tmp] = e.Type

	basePtr := b.newTemp()
	b.locals[basePtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &BorrowInst{
		Dst:   basePtr,
		Src:   tmp,
		IsMut: true,
	})

	discrimPtr := b.newTemp()
	b.locals[discrimPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        discrimPtr,
		Object:     &Register{Name: basePtr, Type: types.PtrType{}},
		ObjectType: e.Type,
		FieldName:  "discriminant",
		FieldIndex: 0,
		FieldType:  types.I32Type{},
	})
	b.current.Statements = append(b.current.Statements, &StoreInst{
		DstPtr: discrimPtr,
		Value:  &IntConstant{Value: int64(e.Variant.Discriminant), Type: types.I32Type{}},
		Type:   types.I32Type{},
	})

	var flatPayloads []Value
	if len(e.Arguments) > 0 {
		for _, arg := range e.Arguments {
			flatPayloads = append(flatPayloads, hir.MapNode(arg, b))
		}
	} else if len(e.Fields) > 0 {
		givenFields := make(map[string]hir.Expr, len(e.Fields))
		for _, f := range e.Fields {
			givenFields[f.Name] = f.Value
		}
		for _, field := range e.Variant.Fields {
			if val, exists := givenFields[field.Name]; exists {
				flatPayloads = append(flatPayloads, hir.MapNode(val, b))
			} else {
				panic(fmt.Sprintf("Missing field '%s' for variant '%s'", field.Name, e.Variant.Name))
			}
		}
	}

	if len(flatPayloads) > 0 {
		payloadArrPtr := b.newTemp()
		b.locals[payloadArrPtr] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &FieldAddrInst{
			Dst:        payloadArrPtr,
			Object:     &Register{Name: basePtr, Type: types.PtrType{}},
			ObjectType: e.Type,
			FieldName:  "payload",
			FieldIndex: 1,
			FieldType:  types.UnknownType{},
		})

		variantStructTy := b.getVariantPayloadStructType(e.Type, e.Variant.Name)

		castPtr := b.newTemp()
		b.locals[castPtr] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &BitcastPtrInst{
			Dst:  castPtr,
			Src:  &Register{Name: payloadArrPtr, Type: types.PtrType{}},
			Type: types.PtrType{},
		})

		for i, pVal := range flatPayloads {
			pType := getValueType(pVal)

			fieldPtr := b.newTemp()
			b.locals[fieldPtr] = types.PtrType{}
			b.current.Statements = append(b.current.Statements, &FieldAddrInst{
				Dst:        fieldPtr,
				Object:     &Register{Name: castPtr, Type: types.PtrType{}},
				ObjectType: variantStructTy,
				FieldName:  fmt.Sprintf("payload_%d", i),
				FieldIndex: i,
				FieldType:  pType,
			})
			b.current.Statements = append(b.current.Statements, &StoreInst{
				DstPtr: fieldPtr,
				Value:  pVal,
				Type:   pType,
			})
		}
	}

	return &Register{Name: tmp, Type: e.Type}
}

func (b *Builder) MapVecLiteral(e *hir.VecLiteral) Value {
	tmp := b.newTemp()
	t := e.Type
	b.locals[tmp] = t
	vecPtr := b.EmitMamlVecCreate(&IntConstant{Value: int64(SizeOf(t.Base, b.Target)), Type: types.I64Type{}})
	b.emitTransfer(tmp, vecPtr)

	for _, elem := range e.Elements {
		flatElem := hir.MapNode(elem, b)
		boxedElem := b.boxScalar(flatElem, t.Base)
		b.EmitMamlVecPush(&Register{Name: tmp, Type: types.PtrType{}}, boxedElem)
	}
	return &Register{Name: tmp, Type: t}
}

func (b *Builder) MapMapLiteral(e *hir.MapLiteral) Value {
	tmp := b.newTemp()
	t := e.Type
	b.locals[tmp] = t
	isStrKey := false
	if _, isStr := t.Key.(types.StringType); isStr {
		isStrKey = true
	}
	mapPtr := b.EmitMamlMapCreate(
		&IntConstant{Value: int64(SizeOf(t.Value, b.Target)), Type: types.I64Type{}},
		&BoolConstant{Value: isStrKey, Type: types.BoolType{}},
	)
	b.emitTransfer(tmp, mapPtr)

	for _, kv := range e.Elements {
		flatVal := hir.MapNode(kv.Value, b)
		hashVal, ptrVal, lenVal := b.lowerMapKey(kv.Key)
		b.EmitMamlMapPut(&Register{Name: tmp, Type: types.PtrType{}}, hashVal, ptrVal, lenVal, flatVal)
	}
	return &Register{Name: tmp, Type: t}
}

func (b *Builder) lowerMapKey(keyExpr hir.Expr) (hash, ptr, length Value) {
	flatKey := hir.MapNode(keyExpr, b)
	keyType := hir.TypeOf(keyExpr)

	switch keyType.(type) {
	case types.I64Type, *types.I64Type:
		hashTmp := b.newTemp()
		b.locals[hashTmp] = types.I64Type{}
		b.current.Statements = append(b.current.Statements, &CastInst{Dst: hashTmp, Src: flatKey, Type: types.I64Type{}})
		hashVal := &Register{Name: hashTmp, Type: types.I64Type{}}
		ptrVal := &IntConstant{Value: 0, Type: types.I64Type{}}
		lenVal := &IntConstant{Value: 0, Type: types.I64Type{}}
		return hashVal, ptrVal, lenVal

	case types.StringType, *types.StringType:
		keyTmp := b.newTemp()
		b.locals[keyTmp] = keyType
		b.current.Statements = append(b.current.Statements, &AssignInst{Dst: keyTmp, RValue: flatKey})
		safeKey := &Register{Name: keyTmp, Type: keyType}
		ptrVal, lenVal := b.emitExtractString(safeKey)

		hashVal := b.EmitMamlStrHash(ptrVal, lenVal)
		return hashVal, ptrVal, lenVal

	default:
		return &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.I64Type{}}
	}
}

func (b *Builder) flattenStringEq(e *hir.InfixExpr, flatLeft, flatRight Value) Value {
	leftTmp := b.newTemp()
	b.locals[leftTmp] = types.StringType{}
	b.current.Statements = append(b.current.Statements, &AssignInst{Dst: leftTmp, RValue: flatLeft})
	safeLeft := &Register{Name: leftTmp, Type: types.StringType{}}

	rightTmp := b.newTemp()
	b.locals[rightTmp] = types.StringType{}
	b.current.Statements = append(b.current.Statements, &AssignInst{Dst: rightTmp, RValue: flatRight})
	safeRight := &Register{Name: rightTmp, Type: types.StringType{}}

	leftPtr, leftLen := b.emitExtractString(safeLeft)
	rightPtr, rightLen := b.emitExtractString(safeRight)

	callVal := b.EmitMamlStrEq(leftPtr, leftLen, rightPtr, rightLen)

	boolTmp := b.newTemp()
	b.locals[boolTmp] = types.BoolType{}
	b.current.Statements = append(b.current.Statements, &BinaryOpInst{
		Dst:      boolTmp,
		Operator: "!=",
		Left:     callVal,
		Right:    &IntConstant{Value: 0, Type: types.I64Type{}},
		Type:     types.BoolType{},
	})

	if e.Operator == "!=" {
		notTmp := b.newTemp()
		b.locals[notTmp] = types.BoolType{}
		b.current.Statements = append(b.current.Statements, &UnaryOpInst{
			Dst:      notTmp,
			Operator: "!",
			Operand:  &Register{Name: boolTmp, Type: types.BoolType{}},
			Type:     types.BoolType{},
		})
		boolTmp = notTmp
	}

	return &Register{Name: boolTmp, Type: types.BoolType{}}
}

// Given an expression that can appear on the left-hand side of an assignment,
// return a pointer to where that object lives.
func (b *Builder) flattenPlace(expr hir.Expr) Value {
	switch e := expr.(type) {
	case *hir.Identifier:
		regName := e.Value
		if e.Symbol != nil {
			regName = b.getSymbolName(e.Symbol)
		}
		if _, isPtr := b.locals[regName].(types.PtrType); isPtr {
			return &Register{Name: regName, Type: types.PtrType{}}
		}
		ptrTmp := b.newTemp()
		b.locals[ptrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &BorrowInst{
			Dst:   ptrTmp,
			Src:   regName,
			IsMut: true,
		})
		return &Register{Name: ptrTmp, Type: types.PtrType{}}

	case *hir.FieldAccess:
		basePtr := b.flattenPlace(e.Object)
		objType := hir.TypeOf(e.Object)
		basePtr = b.resolveBasePtr(basePtr, objType)

		fieldIndex := -1
		if st, ok := objType.(*types.StructType); ok {
			fieldIndex = st.GetFieldIndex(e.Field.Value)
		}

		ptrTmp := b.newTemp()
		b.locals[ptrTmp] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &FieldAddrInst{
			Dst:        ptrTmp,
			Object:     basePtr,
			ObjectType: objType,
			FieldName:  e.Field.Value,
			FieldIndex: fieldIndex,
			FieldType:  e.Type,
		})
		return &Register{Name: ptrTmp, Type: types.PtrType{}}

	case *hir.IndexExpr:
		basePtr := b.flattenPlace(e.Left)
		sourceType := hir.TypeOf(e.Left)
		basePtr = b.resolveBasePtr(basePtr, sourceType)

		switch sourceType.(type) {
		case types.StringType, *types.ViewType:
			ptrFieldAddr := b.newTemp()
			b.locals[ptrFieldAddr] = types.PtrType{}
			b.current.Statements = append(b.current.Statements, &FieldAddrInst{
				Dst:        ptrFieldAddr,
				Object:     basePtr,
				ObjectType: sourceType,
				FieldName:  "ptr",
				FieldIndex: 0,
				FieldType:  types.PtrType{},
			})

			rawDataPtr := b.newTemp()
			b.locals[rawDataPtr] = types.PtrType{}
			b.current.Statements = append(b.current.Statements, &LoadPtrInst{
				Dst:  rawDataPtr,
				Ptr:  &Register{Name: ptrFieldAddr, Type: types.PtrType{}},
				Type: types.PtrType{},
			})

			elemPtr := b.newTemp()
			b.locals[elemPtr] = types.PtrType{}

			var elemType types.Type = types.U8Type{}
			if view, isView := sourceType.(*types.ViewType); isView {
				elemType = view.Base
			}
			idxVal := hir.MapNode(e.Index, b)
			b.current.Statements = append(b.current.Statements, &IndexAddrInst{
				Dst:        elemPtr,
				Source:     &Register{Name: rawDataPtr, Type: types.PtrType{}},
				SourceType: types.PtrType{},
				Index:      idxVal,
				Type:       elemType,
			})
			return &Register{Name: elemPtr, Type: types.PtrType{}}
		}

		elemType := e.Type
		ptrTmp := b.newTemp()
		b.locals[ptrTmp] = types.PtrType{}
		idxVal := hir.MapNode(e.Index, b)
		b.current.Statements = append(b.current.Statements, &IndexAddrInst{
			Dst:        ptrTmp,
			Source:     basePtr,
			SourceType: sourceType,
			Index:      idxVal,
			Type:       elemType,
		})
		return &Register{Name: ptrTmp, Type: types.PtrType{}}

	case *hir.VecReadExpr:
		flatVecPtr := hir.MapNode(e.Vec, b)
		flatIdx := hir.MapNode(e.Index, b)
		return b.EmitMamlVecGet(flatVecPtr, flatIdx)

	default:
		panic(fmt.Sprintf("MIR Builder Error: Expression cannot be used as an L-Value: %T", expr))
	}
}

func (b *Builder) emitLoad(ptr Value, t types.Type) Value {
	tmp := b.newTemp()
	b.locals[tmp] = t
	b.current.Statements = append(b.current.Statements, &LoadPtrInst{Dst: tmp, Ptr: ptr, Type: t})
	return &Register{Name: tmp, Type: t}
}

func (b *Builder) emitExtractString(strReg Value) (ptrVal, lenVal Value) {
	ptrAddrTmp := b.newTemp()
	b.locals[ptrAddrTmp] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        ptrAddrTmp,
		Object:     strReg,
		ObjectType: getValueType(strReg),
		FieldName:  "ptr",
		FieldIndex: 0,
		FieldType:  types.PtrType{},
	})

	lenAddrTmp := b.newTemp()
	b.locals[lenAddrTmp] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &FieldAddrInst{
		Dst:        lenAddrTmp,
		Object:     strReg,
		ObjectType: getValueType(strReg),
		FieldName:  "len",
		FieldIndex: 1,
		FieldType:  types.U32Type{},
	})

	ptrVal = b.emitLoad(&Register{Name: ptrAddrTmp, Type: types.PtrType{}}, types.PtrType{})
	lenVal = b.emitLoad(&Register{Name: lenAddrTmp, Type: types.PtrType{}}, types.U32Type{})
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
		Name:   fmt.Sprintf("%s_%s_Payload", sumTy.BaseName, variantName),
		Fields: fields,
	}
}

func (b *Builder) emitVariantInit(block *BasicBlock, dst string, sumType types.Type, variantName string, discriminant int, payloads []Value) {
	basePtr := b.newTemp()
	b.locals[basePtr] = types.PtrType{}
	block.Statements = append(block.Statements, &BorrowInst{
		Dst:   basePtr,
		Src:   dst,
		IsMut: true,
	})

	discrimPtr := b.newTemp()
	b.locals[discrimPtr] = types.PtrType{}
	block.Statements = append(block.Statements, &FieldAddrInst{
		Dst:        discrimPtr,
		Object:     &Register{Name: basePtr, Type: types.PtrType{}},
		ObjectType: sumType,
		FieldName:  "discriminant",
		FieldIndex: 0,
		FieldType:  types.I32Type{},
	})
	block.Statements = append(block.Statements, &StoreInst{
		DstPtr: discrimPtr,
		Value:  &IntConstant{Value: int64(discriminant), Type: types.I32Type{}},
		Type:   types.I32Type{},
	})

	if len(payloads) > 0 {
		payloadArrPtr := b.newTemp()
		b.locals[payloadArrPtr] = types.PtrType{}
		block.Statements = append(block.Statements, &FieldAddrInst{
			Dst:        payloadArrPtr,
			Object:     &Register{Name: basePtr, Type: types.PtrType{}},
			ObjectType: sumType,
			FieldName:  "payload",
			FieldIndex: 1,
			FieldType:  types.UnknownType{},
		})

		variantStructTy := b.getVariantPayloadStructType(sumType, variantName)

		castPtr := b.newTemp()
		b.locals[castPtr] = types.PtrType{}
		block.Statements = append(block.Statements, &BitcastPtrInst{
			Dst:  castPtr,
			Src:  &Register{Name: payloadArrPtr, Type: types.PtrType{}},
			Type: types.PtrType{},
		})

		for i, pVal := range payloads {
			pType := getValueType(pVal)
			fieldPtr := b.newTemp()
			b.locals[fieldPtr] = types.PtrType{}

			block.Statements = append(block.Statements, &FieldAddrInst{
				Dst:        fieldPtr,
				Object:     &Register{Name: castPtr, Type: types.PtrType{}},
				ObjectType: variantStructTy,
				FieldName:  fmt.Sprintf("payload_%d", i),
				FieldIndex: i,
				FieldType:  pType,
			})
			block.Statements = append(block.Statements, &StoreInst{
				DstPtr: fieldPtr,
				Value:  pVal,
				Type:   pType,
			})
		}
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
		"print":        (*Builder).lowerPrint,
	}
}

func (b *Builder) lowerYieldNow(e *hir.CallExpr) Value {
	b.EmitMamlYieldNow(b.currentFuture)

	resumeBlock := b.emitCoroSuspend()
	b.current = resumeBlock
	return unitValue
}

func (b *Builder) lowerRunExecutor(e *hir.CallExpr) Value {
	flatArg := hir.MapNode(e.Arguments[0].Argument, b)
	b.EmitMamlRunExecutor(flatArg)
	resTmp := b.newTemp()
	b.locals[resTmp] = e.Type
	result := b.EmitMamlTaskGetResult(flatArg)
	b.emitTransfer(resTmp, result)
	return &Register{Name: resTmp, Type: e.Type}
}

func (b *Builder) lowerLen(e *hir.CallExpr) Value {
	flatArg := hir.MapNode(e.Arguments[0].Argument, b)

	switch hir.TypeOf(e.Arguments[0].Argument).(type) {
	case *types.VectorType, *types.ViewType:
		return b.EmitMamlVecLen(flatArg)
	case *types.MapType:
		return b.EmitMamlMapLen(flatArg)
	default:
		panic("compiler error: unhandled type passed to intrinsic len()")
	}
}

func (b *Builder) lowerDelete(e *hir.CallExpr) Value {
	flatMapPtr := hir.MapNode(e.Arguments[0].Argument, b)
	hashVal, ptrVal, lenVal := b.lowerMapKey(e.Arguments[1].Argument)
	b.EmitMamlMapDelete(flatMapPtr, hashVal, ptrVal, lenVal)
	return &Register{Name: "_unit", Type: types.UnitType{}}
}

func (b *Builder) lowerPrint(e *hir.CallExpr) Value {
	strVal := hir.MapNode(e.Arguments[0].Argument, b)
	tmpName := b.newTemp()
	b.locals[tmpName] = types.StringType{}
	b.current.Statements = append(b.current.Statements, &StoreInst{DstPtr: tmpName, Value: strVal, Type: types.StringType{}})
	strLVal := &Register{Name: tmpName, Type: types.StringType{}}
	ptrVal, lenVal := b.emitExtractString(strLVal)
	b.EmitMamlPrint(ptrVal, lenVal)
	return &Register{Name: "_unit", Type: types.UnitType{}}
}
