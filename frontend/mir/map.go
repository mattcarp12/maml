package mir

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

func (b *Builder) LowerMapLiteral(e *hir.MapLiteral) Value {
	tmp := b.newTemp()
	t := e.Type
	b.locals[tmp] = t // 'tmp' is now the Map struct value on the stack
	obj := &Register{Name: tmp, Type: t}

	isStrKey := false
	if _, isStr := t.Key.(types.StringType); isStr {
		isStrKey = true
	}

	// Inline Initialization
	b.storeField(obj, t, &Register{Name: "null", Type: types.PtrType{}}, "entries", 0, types.PtrType{})
	b.storeField(obj, t, &IntConstant{Value: 0, Type: types.U32Type{}}, "count", 1, types.U32Type{})
	b.storeField(obj, t, &IntConstant{Value: 0, Type: types.U32Type{}}, "tombstone_count", 2, types.U32Type{})
	b.storeField(obj, t, &IntConstant{Value: 0, Type: types.U32Type{}}, "cap", 3, types.U32Type{})
	b.storeField(obj, t, &IntConstant{Value: int64(SizeOf(t.Value, b.Target)), Type: types.U32Type{}}, "val_size", 4, types.U32Type{})
	b.storeField(obj, t, &BoolConstant{Value: isStrKey, Type: types.BoolType{}}, "is_string_key", 5, types.BoolType{})

	if len(e.Elements) > 0 {
		// Borrow the map value to pass a pointer to the ABI
		mapPtrReg := b.emitBorrow(tmp, true)

		for _, kv := range e.Elements {
			flatVal := hir.LowerNode(kv.Value, b)
			hashVal, ptrVal, lenVal := b.lowerMapKey(kv.Key)
			b.EmitMamlMapPut(mapPtrReg, hashVal, ptrVal, lenVal, flatVal)
		}
	}

	return obj
}

func (b *Builder) LowerMapReadExpr(e *hir.MapReadExpr) Value {
	mapPtrVal := b.addressOf(e.Map)
	hashVal, ptrVal, lenVal := b.lowerMapKey(e.Key)

	opaquePtr := b.EmitMamlMapGet(mapPtrVal, hashVal, ptrVal, lenVal)

	resTmp := b.emitTemp(e.Type)
	cmpTmp := b.emitTemp(types.BoolType{})
	b.push(&BinaryOpInst{
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

	optType := e.Type.(*types.SumType)
	valType := optType.TypeArgs[0]

	valTmp := b.newTemp()
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

	if reg, ok := mapPtrVal.(*Register); ok {
		mergeBlock.Statements = append(mergeBlock.Statements, &KeepAliveInst{Src: reg.Name})
	}

	b.current = mergeBlock
	return &Register{Name: resTmp, Type: e.Type}
}

func (b *Builder) LowerMapInsertStmt(n *hir.MapInsertStmt) Value {
	if n == nil {
		return unitValue
	}

	mapPtrVal := b.addressOf(n.Map)
	hashVal, ptrVal, lenVal := b.lowerMapKey(n.Key)

	var writeVal Value
	var elemType types.Type
	if n.Operator != "" {
		opaquePtr := b.EmitMamlMapGet(mapPtrVal, hashVal, ptrVal, lenVal)
		elemType = hir.TypeOf(n.Value)
		writeVal = b.emitCompoundMath(opaquePtr, n.Operator, n.Value, elemType)
	} else {
		writeVal = hir.LowerNode(n.Value, b)
		elemType = getValueType(writeVal)
	}

	boxedVal := b.boxScalar(writeVal, elemType)
	b.EmitMamlMapPut(mapPtrVal, hashVal, ptrVal, lenVal, boxedVal)
	return unitValue
}

func (b *Builder) lowerDelete(e *hir.CallExpr) Value {
	mapPtrReg := b.addressOf(e.Arguments[0].Argument)
	hashVal, ptrVal, lenVal := b.lowerMapKey(e.Arguments[1].Argument)

	b.EmitMamlMapDelete(mapPtrReg, hashVal, ptrVal, lenVal)
	return &Register{Name: "_unit", Type: types.UnitType{}}
}

func (b *Builder) lowerMapKey(keyExpr hir.Expr) (hash, ptr, length Value) {
	flatKey := hir.LowerNode(keyExpr, b)
	keyType := hir.TypeOf(keyExpr)

	switch keyType.(type) {
	case types.I64Type, *types.I64Type:
		hashTmp := b.newTemp()
		hashVal := b.emit(&CastInst{Dst: hashTmp, Src: flatKey, Type: types.I64Type{}}, hashTmp, types.I64Type{})
		ptrVal := &IntConstant{Value: 0, Type: types.I64Type{}}
		lenVal := &IntConstant{Value: 0, Type: types.I64Type{}}
		return hashVal, ptrVal, lenVal

	case types.StringType, *types.StringType:
		keyTmp := b.newTemp()
		safeKey := b.emit(&AssignInst{Dst: keyTmp, RValue: flatKey}, keyTmp, keyType)
		ptrVal, lenVal := b.emitExtractString(safeKey)
		strPtr := b.emitBorrow(keyTmp, false)
		hashVal := b.EmitMamlStrHash(strPtr)
		return hashVal, ptrVal, lenVal

	default:
		return &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.I64Type{}}
	}
}
