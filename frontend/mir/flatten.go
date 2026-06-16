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

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
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
		tmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &UnaryOpInst{
			Dst:      tmp,
			Operator: e.Operator,
			Operand:  flatRight,
			Type:     e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.IndexExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatIndex, current := b.flattenExpr(e.Index, current)
		tmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &IndexReadInst{
			Dst:        tmp,
			Source:     flatLeft,
			SourceType: hir.TypeOf(e.Left),
			Index:      flatIndex,
			Type:       e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.CallExpr:
		return b.flattenCall(e, current)

	case *hir.AwaitExpr:
		flatTask, current := b.flattenExpr(e.Value, current)
		resumeBlock := b.newBlock()
		cleanupBlock := b.newBlock()
		suspendBlock := b.newBlock()
		current.Terminator = &CoroSuspendTerminator{
			ResumeBlock:  resumeBlock.ID,
			CleanupBlock: cleanupBlock.ID,
			SuspendBlock: suspendBlock.ID,
		}
		cleanupBlock.Terminator = &JumpTerminator{Target: suspendBlock.ID}
		suspendBlock.Terminator = &ReturnTerminator{}
		tmp := b.newTemp()
		resumeBlock.Statements = append(resumeBlock.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		resumeBlock.Statements = append(resumeBlock.Statements, &CallInst{
			Dst:      tmp,
			Function: &Register{Name: "maml_task_get_result", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatTask, Mut: false},
			},
			Type: e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, resumeBlock

	case *hir.StructLiteral:
		return b.flattenStructLiteral(e, current)

	case *hir.FieldAccess:
		return b.flattenFieldAccess(e, current)

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
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
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
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &VariantDiscriminantInst{
			Dst:    tmp,
			Object: flatObj,
			Type:   e.Type,
		})
		return &Register{Name: tmp, Type: e.Type}, current

	case *hir.VariantReadExpr:
		flatObj, current := b.flattenExpr(e.Object, current)
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
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

		opaqueTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: opaqueTmp, Type: types.AnyType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:      opaqueTmp,
			Function: &Register{Name: "maml_map_get", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatMap},
				{Argument: hashVal},
				{Argument: ptrVal},
				{Argument: lenVal},
				// {Argument: intKey},
			},
			Type: types.AnyType{},
		})
		opaquePtr := &Register{Name: opaqueTmp, Type: types.AnyType{}}

		// Option<V> Branching Logic
		resTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: resTmp, Type: e.Type})

		cmpTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: cmpTmp, Type: types.BoolType{}})
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

		someTmp := b.newTemp()
		thenBlock.Statements = append(thenBlock.Statements, &TempDeclInst{Name: someTmp, Type: e.Type})
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

		opaqueTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: opaqueTmp, Type: types.AnyType{}})

		current.Statements = append(current.Statements, &CallInst{
			Dst:      opaqueTmp,
			Function: &Register{Name: "maml_vec_get", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatVec},
				{Argument: flatIdx},
			},
			Type: types.AnyType{},
		})
		opaquePtr := &Register{Name: opaqueTmp, Type: types.AnyType{}}

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

func (b *Builder) flattenFieldAccess(e *hir.FieldAccess, current *BasicBlock) (Value, *BasicBlock) {
	flatObj, current := b.flattenExpr(e.Object, current)

	fieldIndex := -1
	if reg, ok := flatObj.(*Register); ok {
		if st, ok := reg.Type.(*types.StructType); ok {
			fieldIndex = st.GetFieldIndex(e.Field.Value)
		}
	}

	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
	current.Statements = append(current.Statements, &FieldReadInst{
		Dst:        tmp,
		Object:     flatObj,
		FieldName:  e.Field.Value,
		FieldIndex: fieldIndex,
		Type:       e.Type,
	})

	return &Register{Name: tmp, Type: e.Type}, current
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
		case "len":
			flatArg, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			current = nextBlock

			var rtSym string
			switch hir.TypeOf(e.Arguments[0].Argument).(type) {
			case *types.VectorType, *types.ViewType:
				rtSym = "maml_vec_len"
			case *types.MapType:
				rtSym = "maml_map_len"
			}

			tmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: types.IntType{}})
			current.Statements = append(current.Statements, &CallInst{
				Dst: tmp, Function: &Register{Name: rtSym, Type: types.UnknownType{}},
				Arguments: []MIRCallArg{{Argument: flatArg, Mut: false}}, Type: types.IntType{},
			})
			return &Register{Name: tmp, Type: types.IntType{}}, current

		case "delete":
			flatMap, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			hashVal, ptrVal, lenVal, intKey, nextBlock := b.lowerMapKey(e.Arguments[1].Argument, nextBlock)
			current = nextBlock

			tmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: types.UnitType{}})
			current.Statements = append(current.Statements, &CallInst{
				Dst: tmp, Function: &Register{Name: "maml_map_delete", Type: types.UnknownType{}},
				Arguments: []MIRCallArg{
					{Argument: flatMap, Mut: true}, {Argument: hashVal}, {Argument: ptrVal}, {Argument: lenVal}, {Argument: intKey},
				}, Type: types.UnitType{},
			})
			return &Register{Name: tmp, Type: types.UnitType{}}, current

		case "puts":
			flatArg, nextBlock := b.flattenExpr(e.Arguments[0].Argument, current)
			current = nextBlock

			// 1. Extract the raw pointer from the String fat-pointer
			strPtrTmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: strPtrTmp, Type: types.AnyType{}})
			current.Statements = append(current.Statements, &FieldReadInst{
				Dst:        strPtrTmp,
				Object:     flatArg,
				FieldName:  "ptr",
				FieldIndex: 0,
				Type:       types.AnyType{},
			})

			// 2. Extract the length from the String fat-pointer
			strLenTmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: strLenTmp, Type: types.IntType{}})
			current.Statements = append(current.Statements, &FieldReadInst{
				Dst:        strLenTmp,
				Object:     flatArg,
				FieldName:  "len",
				FieldIndex: 1,
				Type:       types.IntType{},
			})

			// 3. Route both arguments to the safe Zig maml_print runtime function
			tmp := b.newTemp()
			current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: types.UnitType{}})

			printFnType := types.RuntimeABI[types.SYM_PUTS]
			current.Statements = append(current.Statements, &CallInst{
				Dst:      tmp,
				Function: &Register{Name: types.SYM_PUTS, Type: printFnType},
				Arguments: []MIRCallArg{
					{Argument: &Register{Name: strPtrTmp, Type: types.AnyType{}}, Mut: false},
					{Argument: &Register{Name: strLenTmp, Type: types.IntType{}}, Mut: false},
				},
				Type: types.UnitType{},
			})
			return &Register{Name: tmp, Type: types.UnitType{}}, current

		}
	}

	flatFunc, current := b.flattenExpr(e.Function, current)

	var flatArgs []MIRCallArg
	for _, arg := range e.Arguments {
		flatArgExpr, currentBlock := b.flattenExpr(arg.Argument, current)
		current = currentBlock
		flatArgs = append(flatArgs, MIRCallArg{
			Argument: flatArgExpr,
			Mut:      arg.Mut,
		})
	}

	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
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
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
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

	getHooks := func(t types.Type) (string, string) {
		if t != nil && t.IsNeedsARC() {
			return "maml_retain", "maml_release"
		}
		return "null", "null"
	}
	elemRetain, elemRelease := getHooks(t.Base)

	createFn := &Register{Name: "maml_vec_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []MIRCallArg{
			{Argument: &IntConstant{Value: int64(layout.SizeOf(t.Base, b.Target)), Type: types.IntType{}}},
			{Argument: &Register{Name: elemRetain, Type: types.UnknownType{}}},
			{Argument: &Register{Name: elemRelease, Type: types.UnknownType{}}},
		},
		Type: t,
	})

	for _, elem := range e.Elements {
		var flatElem Value
		flatElem, current = b.flattenExpr(elem, current)

		pushFn := &Register{Name: "maml_vec_push", Type: types.UnknownType{}}
		pushTmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: pushTmp, Type: types.UnitType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:      pushTmp,
			Function: pushFn,
			Arguments: []MIRCallArg{
				{Argument: &Register{Name: tmp, Type: t}, Mut: true},
				{Argument: flatElem, Mut: false},
			},
			Type: types.UnitType{},
		})
	}

	return &Register{Name: tmp, Type: t}, current
}

func (b *Builder) flattenMapLiteral(e *hir.MapLiteral, current *BasicBlock) (Value, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})

	getHooks := func(t types.Type) (string, string) {
		if t != nil && t.IsNeedsARC() {
			return "maml_retain", "maml_release"
		}
		return "null", "null"
	}

	keyRetain, keyRelease := getHooks(t.Key)
	valRetain, valRelease := getHooks(t.Value)

	isStrKey := int64(0)
	if _, isStr := t.Key.(*types.StringType); isStr {
		isStrKey = 1
	}

	createFn := &Register{Name: "maml_map_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []MIRCallArg{
			{Argument: &IntConstant{Value: int64(layout.SizeOf(t.Value, b.Target)), Type: types.IntType{}}},
			{Argument: &IntConstant{Value: isStrKey, Type: types.IntType{}}},
			{Argument: &Register{Name: keyRetain, Type: types.UnknownType{}}},
			{Argument: &Register{Name: keyRelease, Type: types.UnknownType{}}},
			{Argument: &Register{Name: valRetain, Type: types.UnknownType{}}},
			{Argument: &Register{Name: valRelease, Type: types.UnknownType{}}},
		},
		Type: t,
	})

	for _, kv := range e.Elements {
		var flatVal Value
		flatVal, current = b.flattenExpr(kv.Value, current)

		var hashVal, ptrVal, lenVal, intKey Value
		hashVal, ptrVal, lenVal, intKey, current = b.lowerMapKey(kv.Key, current)

		putFn := &Register{Name: "maml_map_put", Type: types.UnknownType{}}
		putTmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: putTmp, Type: types.UnitType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:      putTmp,
			Function: putFn,
			Arguments: []MIRCallArg{
				{Argument: &Register{Name: tmp, Type: t}, Mut: true},
				{Argument: hashVal},
				{Argument: ptrVal},
				{Argument: lenVal},
				{Argument: intKey},
				{Argument: flatVal},
			},
			Type: types.UnitType{},
		})
	}
	return &Register{Name: tmp, Type: t}, current
}

func (b *Builder) lowerMapKey(keyExpr hir.Expr, current *BasicBlock) (hash, ptr, len, intKey Value, nextBlock *BasicBlock) {
	flatKey, current := b.flattenExpr(keyExpr, current)
	keyType := hir.TypeOf(keyExpr)

	switch keyType.(type) {
	case types.IntType, *types.IntType:
		hashTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: hashTmp, Type: types.IntType{}})
		current.Statements = append(current.Statements, &CastInst{Dst: hashTmp, Src: flatKey, Type: types.IntType{}})

		hashVal := &Register{Name: hashTmp, Type: types.IntType{}}
		ptrVal := &IntConstant{Value: 0, Type: types.AnyType{}}
		lenVal := &IntConstant{Value: 0, Type: types.IntType{}}

		return hashVal, ptrVal, lenVal, flatKey, current

	case types.StringType, *types.StringType:
		keyTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: keyTmp, Type: keyType})
		current.Statements = append(current.Statements, &AssignInst{Dst: keyTmp, RValue: flatKey})
		safeKey := &Register{Name: keyTmp, Type: keyType}

		ptrTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: ptrTmp, Type: types.AnyType{}})
		current.Statements = append(current.Statements, &FieldReadInst{Dst: ptrTmp, Object: safeKey, FieldName: "ptr", FieldIndex: 0, Type: types.AnyType{}})
		ptrVal := &Register{Name: ptrTmp, Type: types.AnyType{}}

		lenTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: lenTmp, Type: types.IntType{}})
		current.Statements = append(current.Statements, &FieldReadInst{Dst: lenTmp, Object: safeKey, FieldName: "len", FieldIndex: 1, Type: types.IntType{}})
		lenVal := &Register{Name: lenTmp, Type: types.IntType{}}

		hashTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: hashTmp, Type: types.IntType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst: hashTmp, Function: &Register{Name: "maml_str_hash", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{{Argument: ptrVal}, {Argument: lenVal}}, Type: types.IntType{},
		})
		hashVal := &Register{Name: hashTmp, Type: types.IntType{}}

		intKeyVal := &IntConstant{Value: 0, Type: types.IntType{}}
		return hashVal, ptrVal, lenVal, intKeyVal, current

	default:
		return &IntConstant{Value: 0, Type: types.IntType{}}, &IntConstant{Value: 0, Type: types.AnyType{}}, &IntConstant{Value: 0, Type: types.IntType{}}, &IntConstant{Value: 0, Type: types.IntType{}}, current
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

	leftPtrTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: leftPtrTmp, Type: types.AnyType{}})
	current.Statements = append(current.Statements, &FieldReadInst{
		Dst: leftPtrTmp, Object: safeLeft, FieldName: "ptr", FieldIndex: 0, Type: types.AnyType{},
	})

	leftLenTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: leftLenTmp, Type: types.IntType{}})
	current.Statements = append(current.Statements, &FieldReadInst{
		Dst: leftLenTmp, Object: safeLeft, FieldName: "len", FieldIndex: 1, Type: types.IntType{},
	})

	rightPtrTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: rightPtrTmp, Type: types.AnyType{}})
	current.Statements = append(current.Statements, &FieldReadInst{
		Dst: rightPtrTmp, Object: safeRight, FieldName: "ptr", FieldIndex: 0, Type: types.AnyType{},
	})

	rightLenTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: rightLenTmp, Type: types.IntType{}})
	current.Statements = append(current.Statements, &FieldReadInst{
		Dst: rightLenTmp, Object: safeRight, FieldName: "len", FieldIndex: 1, Type: types.IntType{},
	})

	callTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: callTmp, Type: types.IntType{}})
	current.Statements = append(current.Statements, &CallInst{
		Dst:      callTmp,
		Function: &Register{Name: "maml_str_eq", Type: types.UnknownType{}},
		Arguments: []MIRCallArg{
			{Argument: &Register{Name: leftPtrTmp, Type: types.AnyType{}}},
			{Argument: &Register{Name: leftLenTmp, Type: types.IntType{}}},
			{Argument: &Register{Name: rightPtrTmp, Type: types.AnyType{}}},
			{Argument: &Register{Name: rightLenTmp, Type: types.IntType{}}},
		},
		Type: types.IntType{},
	})

	boolTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: boolTmp, Type: types.BoolType{}})
	current.Statements = append(current.Statements, &BinaryOpInst{
		Dst:      boolTmp,
		Operator: "!=",
		Left:     &Register{Name: callTmp, Type: types.IntType{}},
		Right:    &IntConstant{Value: 0, Type: types.IntType{}},
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
