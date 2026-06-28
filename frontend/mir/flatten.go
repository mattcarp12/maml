package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/layout"
	"github.com/mattcarp12/maml/frontend/types"
)

// newTemp generates a unique temporary register name for the flattened MIR.
func (b *Builder) newTemp() string {
	b.tempCount++
	return fmt.Sprintf("_t%d", b.tempCount)
}

// flattenExpr eliminates nested expressions by materializing intermediate
// values into explicit, linear temporary variables inside the BasicBlock.
func (b *Builder) flattenExpr(expr hir.Expr, current *BasicBlock) (Value, *BasicBlock) {
	if expr == nil {
		return nil, current
	}

	switch e := expr.(type) {
	case *hir.Identifier:
		// Convert HIR Identifier to MIR Register
		regName := e.Value
		if e.Symbol != nil && e.Symbol.Kind == types.VarSymbol {
			regName = b.getSymbolName(e.Symbol)
		}
		return &Register{Name: regName, Type: e.Type}, current

	case *hir.IntLiteral:
		return &IntConstant{Value: e.Value, Type: e.Type}, current

	case *hir.BoolLiteral:
		return &BoolConstant{Value: e.Value, Type: e.Type}, current

	case *hir.StringLiteral:
		return &StringConstant{Value: e.Value, Type: e.Type}, current

	case *hir.InfixExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatRight, current := b.flattenExpr(e.Right, current)

		// Intercept String Equality Comparisons to enforce deep-equality runtime semantics
		if hir.TypeOf(e.Left).Equals(types.StringType{}) && (e.Operator == "==" || e.Operator == "!=") {
			return b.flattenStringEq(e, flatLeft, flatRight, current)
		}
		tmp := b.emitTemp(current, e.Type)
		current.Statements = append(current.Statements, &BinaryOpInst{
			Dst:      tmp,
			Operator: e.Operator,
			Left:     flatLeft,
			Right:    flatRight,
			Type:     e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.PrefixExpr:
		flatRight, current := b.flattenExpr(e.Right, current)
		tmp := b.emitTemp(current, e.Type)
		current.Statements = append(current.Statements, &UnaryOpInst{
			Dst:      tmp,
			Operator: e.Operator,
			Operand:  flatRight,
			Type:     e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.IndexExpr:
		// 1. Calculate the exact memory address of the index
		ptrVal, nextBlock := b.flattenPlace(e, current)
		current = nextBlock

		// 2. Load the actual value from that memory address
		valTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: e.Type})
		current.Statements = append(current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  ptrVal,
			Type: e.Type,
		})

		return &Register{Name: valTmp, Type: e.Type}, current

	case *hir.CallExpr:
		return b.flattenCall(e, current)

	case *hir.AwaitExpr:
		flatTask, current := b.flattenExpr(e.Value, current)
		resumeBlock := b.newBlock()
		cleanupBlock := b.newBlock()
		suspendBlock := b.newBlock()
		regTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: regTmp, Type: types.UnitType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:       regTmp,
			Function:  &Register{Name: "maml_task_await", Type: types.UnknownType{}},
			Arguments: []Value{flatTask},
			Type:      types.UnitType{},
		})

		current.Terminator = &CoroSuspendTerminator{
			ResumeBlock:  resumeBlock.ID,
			CleanupBlock: cleanupBlock.ID,
			SuspendBlock: suspendBlock.ID,
		}
		cleanupBlock.Terminator = &JumpTerminator{Target: suspendBlock.ID}
		suspendBlock.Terminator = &CoroYieldTerminator{}

		tmp := b.newTemp()
		resumeBlock.Statements = append(resumeBlock.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		resumeBlock.Statements = append(resumeBlock.Statements, &CallInst{
			Dst:       tmp,
			Function:  &Register{Name: "maml_task_get_result", Type: types.UnknownType{}},
			Arguments: []Value{flatTask},
			Type:      e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, resumeBlock

	case *hir.SpawnExpr:
		flatFuture, current := b.flattenExpr(e.Value, current)
		_, current = b.EmitMamlSpawnTask(current, flatFuture)
		return flatFuture, current

	case *hir.StructLiteral:
		return b.flattenStructLiteral(e, current)

	case *hir.FieldAccess:
		// 1. Calculate the memory address of the field
		ptrVal, current := b.flattenPlace(e, current)
		// 2. Load the actual value from that address
		valTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: e.Type})
		current.Statements = append(current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  ptrVal,
			Type: e.Type,
		})
		return &Register{Name: valTmp, Type: e.Type}, current

	case *hir.ArrayLiteral:
		return b.flattenArrayLiteral(e, current)

	case *hir.MapLiteral:
		return b.flattenMapLiteral(e, current)

	case *hir.VecLiteral:
		return b.flattenVecLiteral(e, current)

	case *hir.VariantLiteral:
		return b.flattenVariantLiteral(e, current)

	case *hir.SliceExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)

		var flatLow, flatHigh Value
		if e.Low != nil {
			flatLow, current = b.flattenExpr(e.Low, current)
		}
		if e.High != nil {
			flatHigh, current = b.flattenExpr(e.High, current)
		}
		tmp := b.emitTemp(current, e.Type)
		current.Statements = append(current.Statements, &SliceInst{
			Dst:           tmp,
			Left:          flatLeft,
			ContainerType: hir.TypeOf(e.Left),
			Low:           flatLow,
			High:          flatHigh,
			ResultType:    e.Type,
		})

		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.IfExpr:
		return b.flattenIfExpr(e, current)

	case *hir.BlockStmt:
		return b.flattenBlockExpr(e, current)

	case *hir.VariantDiscriminantExpr:
		flatObj, current := b.flattenExpr(e.Object, current)
		tmp := b.emitTemp(current, e.Type)
		current.Statements = append(current.Statements, &VariantDiscriminantInst{
			Dst:    tmp,
			Object: flatObj,
			Type:   e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.VariantReadExpr:
		flatObj, current := b.flattenExpr(e.Object, current)
		tmp := b.emitTemp(current, e.Type)
		current.Statements = append(current.Statements, &VariantReadInst{
			Dst:          tmp,
			Object:       flatObj,
			VariantName:  e.VariantName,
			PayloadIndex: e.FieldIndex,
			Type:         e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.MapReadExpr:
		flatMap, current := b.flattenExpr(e.Map, current)
		hashVal, ptrVal, lenVal, _, current := b.lowerMapKey(e.Key, current)

		opaquePtr, current := b.EmitMamlMapGet(current, flatMap, hashVal, ptrVal, lenVal)

		// Option<V> Branching Logic
		resTmp := b.emitTemp(current, e.Type)
		cmpTmp := b.emitTemp(current, types.BoolType{})
		current.Statements = append(current.Statements, &BinaryOpInst{
			Dst: cmpTmp, Operator: "!=", Left: opaquePtr, Right: &IntConstant{Value: 0, Type: types.AnyType{}}, Type: types.BoolType{},
		})

		thenBlock := b.newBlock()
		elseBlock := b.newBlock()
		mergeBlock := b.newBlock()

		current.Terminator = &BranchTerminator{
			Condition:   &Register{Name: cmpTmp, Type: types.BoolType{}},
			TrueTarget:  thenBlock.ID,
			FalseTarget: elseBlock.ID,
		}

		// --- Then Block (Some) ---
		valTmp := b.newTemp()
		optType := e.Type.(*types.SumType)
		valType := optType.TypeArgs[0]

		thenBlock.Statements = append(thenBlock.Statements, &TempDeclInst{Name: valTmp, Type: valType})
		thenBlock.Statements = append(thenBlock.Statements, &LoadPtrInst{Dst: valTmp, Ptr: opaquePtr, Type: valType})

		someTmp := b.emitTemp(thenBlock, e.Type)
		thenBlock.Statements = append(thenBlock.Statements, &VariantInitInst{
			Dst: someTmp, VariantName: "Some", Discriminant: 0,
			Payloads: []Value{&Register{Name: valTmp, Type: valType}}, Type: e.Type,
		})
		thenBlock.Statements = append(thenBlock.Statements, &AssignInst{Dst: resTmp, RValue: &Register{Name: someTmp, Type: e.Type}})
		thenBlock.Terminator = &JumpTerminator{Target: mergeBlock.ID}

		// --- Else Block (None) ---
		noneTmp := b.newTemp()
		elseBlock.Statements = append(elseBlock.Statements, &TempDeclInst{Name: noneTmp, Type: e.Type})
		elseBlock.Statements = append(elseBlock.Statements, &VariantInitInst{
			Dst: noneTmp, VariantName: "None", Discriminant: 1, Payloads: nil, Type: e.Type,
		})
		elseBlock.Statements = append(elseBlock.Statements, &AssignInst{Dst: resTmp, RValue: &Register{Name: noneTmp, Type: e.Type}})
		elseBlock.Terminator = &JumpTerminator{Target: mergeBlock.ID}

		if reg, ok := flatMap.(*Register); ok {
			mergeBlock.Statements = append(mergeBlock.Statements, &KeepAliveInst{Src: reg.Name})
		}

		return &Register{Name: resTmp, Type: e.Type}, mergeBlock

	case *hir.VecReadExpr:
		flatVec, current := b.flattenExpr(e.Vec, current)
		flatIdx, current := b.flattenExpr(e.Index, current)

		opaquePtr, current := b.EmitMamlVecGet(current, flatVec, flatIdx)

		valTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: e.Type})
		current.Statements = append(current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  opaquePtr,
			Type: e.Type,
		})

		return &Register{Name: valTmp, Type: e.Type}, current

	}

	panic(fmt.Sprintf("MIR Builder Error: Unhandled expression type in flattenExpr: %T", expr))
}

func (b *Builder) flattenStructLiteral(e *hir.StructLiteral, current *BasicBlock) (Value, *BasicBlock) {
	if structType, isStruct := e.Type.(*types.StructType); isStruct {
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: structType})

		for _, field := range e.Fields {
			flatVal, nextBlock := b.flattenExpr(field.Value, current)
			current = nextBlock

			fieldName := field.Key.Value
			fieldIndex := structType.GetFieldIndex(fieldName)
			if fieldIndex == -1 {
				fieldIndex = 0 // Failsafe
			}

			current.Statements = append(current.Statements, &StructInitInst{
				Dst:        tmp,
				FieldName:  fieldName,
				FieldIndex: fieldIndex,
				Value:      flatVal,
			})
		}
		return &Register{Name: tmp, Type: structType}, current
	}

	tmp := b.newTemp()
	t := e.Type
	if t == nil {
		t = types.UnknownType{}
	}

	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})
	return &Register{Name: tmp, Type: t}, current
}

func (b *Builder) flattenIfExpr(expr *hir.IfExpr, current *BasicBlock) (Value, *BasicBlock) {
	flatCond, current := b.flattenExpr(expr.Condition, current)

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
		current.Statements = append(current.Statements, &TempDeclInst{
			Name: resultTemp,
			Type: expr.Type,
		})
		resultReg = &Register{Name: resultTemp, Type: expr.Type}
	} else {
		resultReg = &Register{Name: "_unit", Type: types.UnitType{}}
	}

	current.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  thenBlock.ID,
		FalseTarget: elseBlock.ID,
	}

	thenVal, thenEnd := b.flattenBlockExpr(expr.Consequence, thenBlock)
	if thenEnd != nil {
		if !isUnit {
			thenEnd.Statements = append(thenEnd.Statements, &AssignInst{Dst: resultTemp, RValue: thenVal})
		}
		if thenEnd.Terminator == nil {
			thenEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
		}
	}

	if expr.Alternative != nil {
		elseVal, elseEnd := b.flattenBlockExpr(expr.Alternative, elseBlock)
		if elseEnd != nil {
			if !isUnit {
				elseEnd.Statements = append(elseEnd.Statements, &AssignInst{Dst: resultTemp, RValue: elseVal})
			}
			if elseEnd.Terminator == nil {
				elseEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
		}
	}

	return resultReg, mergeBlock
}

func (b *Builder) flattenBlockExpr(block *hir.BlockStmt, current *BasicBlock) (Value, *BasicBlock) {
	if block == nil || len(block.Statements) == 0 {
		return &Register{Name: "_unit", Type: types.UnitType{}}, current
	}

	stmts := block.Statements
	for i, stmt := range stmts {
		if current == nil {
			break
		}
		if i == len(stmts)-1 {
			if yieldStmt, ok := stmt.(*hir.YieldStmt); ok {
				flatVal, nextBlock := b.flattenExpr(yieldStmt.Value, current)
				return flatVal, nextBlock
			}
			if exprStmt, ok := stmt.(*hir.ExprStmt); ok {
				flatVal, nextBlock := b.flattenExpr(exprStmt.Value, current)
				return flatVal, nextBlock
			}
		}
		current = b.buildStmt(stmt, current)
	}

	return &Register{Name: "_unit", Type: types.UnitType{}}, current
}

func (b *Builder) flattenCall(e *hir.CallExpr, current *BasicBlock) (Value, *BasicBlock) {
	if ident, ok := e.Function.(*hir.Identifier); ok {
		switch ident.Value {
		case "yield_now":
			resumeBlock := b.newBlock()
			cleanupBlock := b.newBlock()
			suspendBlock := b.newBlock()

			tmp := b.newTemp()

			// KEEPING MANUAL: Relies on missing arguments in MIR to signal implicit C++ injection
			current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: types.UnitType{}})
			current.Statements = append(current.Statements, &CallInst{
				Dst:       tmp,
				Function:  &Register{Name: "maml_yield_now", Type: types.UnknownType{}},
				Arguments: []Value{},
				Type:      types.UnitType{},
			})

			current.Terminator = &CoroSuspendTerminator{
				ResumeBlock:  resumeBlock.ID,
				CleanupBlock: cleanupBlock.ID,
				SuspendBlock: suspendBlock.ID,
			}
			cleanupBlock.Terminator = &JumpTerminator{Target: suspendBlock.ID}
			suspendBlock.Terminator = &CoroYieldTerminator{}

			return &Register{Name: "_unit", Type: types.UnitType{}}, resumeBlock

		case "run_executor":
			flatArg, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			current = nextBlock

			_, current = b.EmitMamlRunExecutor(current, flatArg)

			// KEEPING MANUAL: Polymorphic Return Type requires explicit type tagging
			resTmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: resTmp, Type: e.Type})
			current.Statements = append(current.Statements, &CallInst{
				Dst:       resTmp,
				Function:  &Register{Name: "maml_task_get_result", Type: types.UnknownType{}},
				Arguments: []Value{flatArg},
				Type:      e.Type,
			})

			return &Register{Name: resTmp, Type: e.Type}, current

		case "len":
			flatArg, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			current = nextBlock

			switch hir.TypeOf(e.Arguments[0].Argument).(type) {
			case *types.VectorType, *types.ViewType:
				return b.EmitMamlVecLen(current, flatArg)
			case *types.MapType:
				return b.EmitMamlMapLen(current, flatArg)
			}

		case "delete":
			flatMap, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			hashVal, ptrVal, lenVal, intKey, nextBlock := b.lowerMapKey(e.Arguments[1].Argument, nextBlock)
			current = nextBlock

			_, current = b.EmitMamlMapDelete(current, flatMap, hashVal, ptrVal, lenVal, intKey)
			return &Register{Name: "_unit", Type: types.UnitType{}}, current

		case "print":
			flatArg, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			current = nextBlock
			strPtrReg, current := b.emitStructFieldLoad(current, flatArg, "ptr", 0, types.PtrType{})
			strLenReg, current := b.emitStructFieldLoad(current, flatArg, "len", 1, types.I64Type{})
			_, current = b.EmitMamlPrint(current, strPtrReg, strLenReg)
			return &Register{Name: "_unit", Type: types.UnitType{}}, current
		}
	}

	flatFunc, current := b.flattenExpr(e.Function, current)
	var flatArgs []Value
	for _, arg := range e.Arguments {
		flatArg, nextBlock := b.flattenExpr(arg.Argument, current)
		current = nextBlock

		// 1. If it's a primitive implicit pass (CapNone), pass it directly.
		if arg.Cap == types.CapNone || arg.Cap == "" {
			flatArgs = append(flatArgs, flatArg)
			continue
		}

		// 2. Otherwise, we must emit a MIR instruction so the Borrow Checker sees the boundary crossing!
		argTmp := b.newTemp()
		argType := hir.TypeOf(arg.Argument)
		current.Statements = append(current.Statements, &TempDeclInst{Name: argTmp, Type: argType})

		// Safely extract the source register name (Sema's isValidMemoryPath guarantees this is a Register)
		srcReg, ok := flatArg.(*Register)
		if !ok {
			panic("compiler error: capability applied to non-register value at call site")
		}

		// 3. Emit the explicit memory transfer instruction
		switch arg.Cap {
		case types.CapMut:
			current.Statements = append(current.Statements, &BorrowInst{Dst: argTmp, Src: srcReg.Name, IsMut: true})
		case types.CapRo:
			current.Statements = append(current.Statements, &BorrowInst{Dst: argTmp, Src: srcReg.Name, IsMut: false})
		case types.CapOwn:
			current.Statements = append(current.Statements, &MoveInst{Dst: argTmp, Src: srcReg.Name})
		case types.CapCopy:
			current.Statements = append(current.Statements, &CopyInst{Dst: argTmp, Src: srcReg.Name})
		}

		// 4. Pass the new, cleanly borrowed/moved temporary register to the function
		flatArgs = append(flatArgs, &Register{Name: argTmp, Type: argType})
	}

	tmp := b.emitTemp(current, e.Type)
	current.Statements = append(current.Statements, &CallInst{
		Dst:       tmp,
		Function:  flatFunc,
		Arguments: flatArgs,
		Type:      e.Type,
	})
	return &Register{Name: tmp, Type: e.Type}, current
}

func (b *Builder) flattenArrayLiteral(e *hir.ArrayLiteral, current *BasicBlock) (Value, *BasicBlock) {
	arrayType, ok := e.Type.(*types.ArrayType)
	if !ok {
		tmp := b.emitTemp(current, e.Type)
		return &Register{Name: tmp, Type: e.Type}, current
	}

	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: arrayType})

	for i, elem := range e.Elements {
		flatVal, nextBlock := b.flattenExpr(elem, current)
		current = nextBlock

		current.Statements = append(current.Statements, &ArrayInitInst{
			Dst:   tmp,
			Index: i,
			Value: flatVal,
		})
	}

	return &Register{Name: tmp, Type: arrayType}, current
}

func (b *Builder) flattenVariantLiteral(e *hir.VariantLiteral, current *BasicBlock) (Value, *BasicBlock) {
	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
	variant := e.Variant

	var flatPayloads []Value

	for _, arg := range e.Arguments {
		flatArg, next := b.flattenExpr(arg, current)
		current = next
		flatPayloads = append(flatPayloads, flatArg)
	}

	if len(e.Fields) > 0 {
		evaluatedFields := make(map[string]Value)
		for _, field := range e.Fields {
			flatVal, next := b.flattenExpr(field.Value, current)
			current = next
			evaluatedFields[field.Name] = flatVal
		}

		for _, declField := range variant.Fields {
			flatPayloads = append(flatPayloads, evaluatedFields[declField.Name])
		}
	}

	current.Statements = append(current.Statements, &VariantInitInst{
		Dst:          tmp,
		VariantName:  variant.Name,
		Discriminant: variant.Discriminant,
		Payloads:     flatPayloads,
		Type:         e.Type,
	})

	return &Register{Name: tmp, Type: e.Type}, current
}

func (b *Builder) flattenVecLiteral(e *hir.VecLiteral, current *BasicBlock) (Value, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})
	createFn := &Register{Name: "maml_vec_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []Value{
			&IntConstant{Value: int64(layout.SizeOf(t.Base, b.Target)), Type: types.I64Type{}},
		},
		Type: t,
	})

	for _, elem := range e.Elements {
		var flatElem Value
		flatElem, current = b.flattenExpr(elem, current)
		_, current = b.EmitMamlVecPush(current, &Register{Name: tmp, Type: t}, flatElem)
	}

	return &Register{Name: tmp, Type: t}, current
}

func (b *Builder) flattenMapLiteral(e *hir.MapLiteral, current *BasicBlock) (Value, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})
	isStrKey := int64(0)
	if _, isStr := t.Key.(types.StringType); isStr {
		isStrKey = 1
	}

	createFn := &Register{Name: "maml_map_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []Value{
			&IntConstant{Value: int64(layout.SizeOf(t.Value, b.Target)), Type: types.I64Type{}},
			&IntConstant{Value: isStrKey, Type: types.I64Type{}},
		},
		Type: t,
	})

	for _, kv := range e.Elements {
		var flatVal Value
		flatVal, current = b.flattenExpr(kv.Value, current)

		var hashVal, ptrVal, lenVal, intKey Value
		hashVal, ptrVal, lenVal, intKey, current = b.lowerMapKey(kv.Key, current)

		_, current = b.EmitMamlMapPut(current, &Register{Name: tmp, Type: t}, hashVal, ptrVal, lenVal, intKey, flatVal)
	}
	return &Register{Name: tmp, Type: t}, current
}

func (b *Builder) lowerMapKey(keyExpr hir.Expr, current *BasicBlock) (hash, ptr, len, intKey Value, nextBlock *BasicBlock) {
	flatKey, current := b.flattenExpr(keyExpr, current)
	keyType := hir.TypeOf(keyExpr)

	switch keyType.(type) {
	case types.I64Type, *types.I64Type:
		hashTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: hashTmp, Type: types.I64Type{}})
		current.Statements = append(current.Statements, &CastInst{Dst: hashTmp, Src: flatKey, Type: types.I64Type{}})

		hashVal := &Register{Name: hashTmp, Type: types.I64Type{}}
		ptrVal := &IntConstant{Value: 0, Type: types.AnyType{}}
		lenVal := &IntConstant{Value: 0, Type: types.I64Type{}}

		return hashVal, ptrVal, lenVal, flatKey, current

	case types.StringType, *types.StringType:
		keyTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: keyTmp, Type: keyType})
		current.Statements = append(current.Statements, &AssignInst{Dst: keyTmp, RValue: flatKey})
		safeKey := &Register{Name: keyTmp, Type: keyType}
		ptrVal, current := b.emitStructFieldLoad(current, safeKey, "ptr", 0, types.PtrType{})
		lenVal, current := b.emitStructFieldLoad(current, safeKey, "len", 1, types.I64Type{})
		hashVal, current := b.EmitMamlStrHash(current, ptrVal, lenVal)

		intKeyVal := &IntConstant{Value: 0, Type: types.I64Type{}}
		return hashVal, ptrVal, lenVal, intKeyVal, current

	default:
		return &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.AnyType{}}, &IntConstant{Value: 0, Type: types.I64Type{}}, &IntConstant{Value: 0, Type: types.I64Type{}}, current
	}
}

func (b *Builder) flattenStringEq(e *hir.InfixExpr, flatLeft, flatRight Value, current *BasicBlock) (Value, *BasicBlock) {
	leftTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: leftTmp, Type: types.StringType{}})
	current.Statements = append(current.Statements, &AssignInst{Dst: leftTmp, RValue: flatLeft})
	safeLeft := &Register{Name: leftTmp, Type: types.StringType{}}

	rightTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: rightTmp, Type: types.StringType{}})
	current.Statements = append(current.Statements, &AssignInst{Dst: rightTmp, RValue: flatRight})
	safeRight := &Register{Name: rightTmp, Type: types.StringType{}}

	leftPtr, current := b.emitStructFieldLoad(current, safeLeft, "ptr", 0, types.PtrType{})
	leftLen, current := b.emitStructFieldLoad(current, safeLeft, "len", 1, types.I64Type{})
	rightPtr, current := b.emitStructFieldLoad(current, safeRight, "ptr", 0, types.PtrType{})
	rightLen, current := b.emitStructFieldLoad(current, safeRight, "len", 1, types.I64Type{})

	callVal, current := b.EmitMamlStrEq(current, leftPtr, leftLen, rightPtr, rightLen)

	boolTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: boolTmp, Type: types.BoolType{}})
	current.Statements = append(current.Statements, &BinaryOpInst{
		Dst:      boolTmp,
		Operator: "!=",
		Left:     callVal,
		Right:    &IntConstant{Value: 0, Type: types.I64Type{}},
		Type:     types.BoolType{},
	})

	if e.Operator == "!=" {
		notTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: notTmp, Type: types.BoolType{}})
		current.Statements = append(current.Statements, &UnaryOpInst{
			Dst:      notTmp,
			Operator: "!",
			Operand:  &Register{Name: boolTmp, Type: types.BoolType{}},
			Type:     types.BoolType{},
		})
		boolTmp = notTmp
	}

	return &Register{Name: boolTmp, Type: types.BoolType{}}, current
}

func (b *Builder) flattenPlace(expr hir.Expr, current *BasicBlock) (Value, *BasicBlock) {
	switch e := expr.(type) {
	case *hir.Identifier:
		// The base of the address chain.
		regName := e.Value
		if e.Symbol != nil {
			regName = b.getSymbolName(e.Symbol)
		}
		// This represents the pointer to the local variable.
		return &Register{Name: regName, Type: e.Type}, current

	case *hir.FieldAccess:
		// 1. Get the pointer to the parent object recursively
		basePtr, current := b.flattenPlace(e.Object, current)

		// 2. Determine the field index
		fieldIndex := -1
		objType := hir.TypeOf(e.Object)
		if st, ok := objType.(*types.StructType); ok {
			fieldIndex = st.GetFieldIndex(e.Field.Value)
		}

		// 3. Emit the FieldAddrInst to calculate the new offset pointer
		ptrTmp := b.newTemp()
		// Note: You might want to wrap e.Type in a types.PointerType{} depending on your type system.
		current.Statements = append(current.Statements, &TempDeclInst{Name: ptrTmp, Type: e.Type})
		current.Statements = append(current.Statements, &FieldAddrInst{
			Dst:        ptrTmp,
			Object:     basePtr,
			FieldName:  e.Field.Value,
			FieldIndex: fieldIndex,
			Type:       e.Type,
		})
		return &Register{Name: ptrTmp, Type: e.Type}, current

	case *hir.IndexExpr:
		// 1. Get the pointer to the base array/slice
		basePtr, current := b.flattenPlace(e.Left, current)

		// 2. Evaluate the index value (this is an R-Value, so use flattenExpr)
		idxVal, current := b.flattenExpr(e.Index, current)

		// 3. Emit the IndexAddrInst to calculate the offset pointer
		ptrTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: ptrTmp, Type: e.Type})
		current.Statements = append(current.Statements, &IndexAddrInst{
			Dst:        ptrTmp,
			Source:     basePtr,
			SourceType: hir.TypeOf(e.Left),
			Index:      idxVal,
			Type:       e.Type,
		})

		return &Register{Name: ptrTmp, Type: e.Type}, current

	// Eventually, you can add *hir.IndexExpr here so arr[0] = 5 uses the exact same logic!
	default:
		panic(fmt.Sprintf("MIR Builder Error: Expression cannot be used as an L-Value: %T", expr))
	}
}

// emitStructFieldLoad manually generates the Address + Load chain for builtin compiler ops.
func (b *Builder) emitStructFieldLoad(current *BasicBlock, obj Value, fieldName string, fieldIndex int, fieldType types.Type) (Value, *BasicBlock) {
	// 1. Get the exact memory address of the field
	addrTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: addrTmp, Type: fieldType})
	current.Statements = append(current.Statements, &FieldAddrInst{
		Dst:        addrTmp,
		Object:     obj,
		FieldName:  fieldName,
		FieldIndex: fieldIndex,
		Type:       fieldType,
	})

	// 2. Load the value from that address
	valTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: fieldType})
	current.Statements = append(current.Statements, &LoadPtrInst{
		Dst:  valTmp,
		Ptr:  &Register{Name: addrTmp, Type: fieldType},
		Type: fieldType,
	})

	return &Register{Name: valTmp, Type: fieldType}, current
}
