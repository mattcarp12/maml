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

// flattenExpr eliminates nested expressions by materializing intermediate
// values into explicit, linear temporary variables inside the BasicBlock.
func (b *Builder) flattenExpr(expr hir.Expr, current *BasicBlock) (hir.Operand, *BasicBlock) {
	if expr == nil {
		return nil, current
	}

	switch e := expr.(type) {
	case *hir.Identifier:
		// If this is a local variable, safely clone and rename it to its unshadowed MIR name
		if e.Symbol != nil && e.Symbol.Kind == types.VarSymbol {
			return &hir.Identifier{
				Pos_:   e.Pos_,
				End_:   e.End_,
				Value:  b.getSymbolName(e.Symbol),
				Type:   e.Type,
				Symbol: e.Symbol,
			}, current
		}
		return e, current

	case *hir.IntLiteral, *hir.BoolLiteral, *hir.StringLiteral:
		return e.(hir.Operand), current

	case *hir.InfixExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatRight, current := b.flattenExpr(e.Right, current)
		tmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &BinaryOpInst{
			Dst:      tmp,
			Operator: e.Operator,
			Left:     flatLeft,
			Right:    flatRight,
			Type:     e.Type,
		})
		return &hir.Identifier{Value: tmp, Type: e.Type}, current

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
		return &hir.Identifier{Value: tmp, Type: e.Type}, current

	case *hir.IndexExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatIndex, current := b.flattenExpr(e.Index, current)
		tmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &IndexReadInst{
			Dst:        tmp,
			Source:     flatLeft,
			SourceType: hir.TypeOf(flatLeft),
			Index:      flatIndex,
			Type:       e.Type,
		})
		return &hir.Identifier{Value: tmp, Type: e.Type}, current

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
			Function: &hir.Identifier{Value: "maml_task_get_result", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatTask, Mut: false},
			},
			Type: e.Type,
		})
		return &hir.Identifier{Value: tmp, Type: e.Type}, resumeBlock

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

		var flatLow, flatHigh hir.Operand
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
			ContainerType: hir.TypeOf(flatLeft),
			Low:           flatLow,
			High:          flatHigh,
			ResultType:    e.Type,
		})

		return &hir.Identifier{Value: tmp, Type: e.Type}, current

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
		return &hir.Identifier{Value: tmp, Type: e.Type}, current

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
		return &hir.Identifier{Value: tmp, Type: e.Type}, current

	case *hir.MapReadExpr:
		flatMap, current := b.flattenExpr(e.Map, current)
		hashVal, ptrVal, lenVal, current := b.lowerMapKey(e.Key, current)

		opaqueTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: opaqueTmp, Type: types.AnyType{}})

		// EMIT EXACTLY 4 ARGUMENTS FOR ZIG'S maml_map_get
		current.Statements = append(current.Statements, &CallInst{
			Dst:      opaqueTmp,
			Function: &hir.Identifier{Value: "maml_map_get", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatMap},
				{Argument: hashVal},
				{Argument: ptrVal},
				{Argument: lenVal},
			},
			Type: types.AnyType{},
		})

		opaquePtr := &hir.Identifier{Value: opaqueTmp, Type: types.AnyType{}}

		valTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: e.Type})
		current.Statements = append(current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  opaquePtr,
			Type: e.Type,
		})

		return &hir.Identifier{Value: valTmp, Type: e.Type}, current

	case *hir.VecReadExpr:
		// 1. Flatten the Vector and Index operands
		flatVec, current := b.flattenExpr(e.Vec, current)
		flatIdx, current := b.flattenExpr(e.Index, current)

		// 2. Runtime Lookup: Call maml_vec_get
		opaqueTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: opaqueTmp, Type: types.AnyType{}})

		// EMIT CALLINST DIRECTLY
		current.Statements = append(current.Statements, &CallInst{
			Dst:      opaqueTmp,
			Function: &hir.Identifier{Value: "maml_vec_get", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: flatVec},
				{Argument: flatIdx},
			},
			Type: types.AnyType{},
		})
		opaquePtr := &hir.Identifier{Value: opaqueTmp, Type: types.AnyType{}}

		// 3. Dereference: Load the actual typed value from the opaque pointer
		valTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: valTmp, Type: e.Type})
		current.Statements = append(current.Statements, &LoadPtrInst{
			Dst:  valTmp,
			Ptr:  opaquePtr,
			Type: e.Type, // The expected value type of the vector element
		})

		return &hir.Identifier{Value: valTmp, Type: e.Type}, current

	}

	// The Failsafe
	if op, ok := expr.(hir.Operand); ok {
		return op, current
	}

	// If we reach here, we leaked an unflattened AST node!
	panic(fmt.Sprintf("MIR Builder Error: Unhandled expression type in flattenExpr: %T", expr))
}

func (b *Builder) flattenStructLiteral(e *hir.StructLiteral, current *BasicBlock) (hir.Operand, *BasicBlock) {
	// =========================================================================
	// CASE 1: Named Structs (Field-by-Field Initialization)
	// =========================================================================
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
		return &hir.Identifier{Value: tmp, Type: structType}, current
	}

	// =========================================================================
	// CASE 2: Semantic Error Fallback (e.g. UnknownType)
	// =========================================================================
	tmp := b.newTemp()
	t := e.Type
	if t == nil {
		t = types.UnknownType{}
	}

	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})
	return &hir.Identifier{Value: tmp, Type: t}, current
}

// flattenFieldAccess lowers a *hir.FieldAccess into a TempDeclInst +
// FieldReadInst pair.  The resolved field index is carried on the instruction
// so the backend can emit a getelementptr without re-walking the type tree.
func (b *Builder) flattenFieldAccess(e *hir.FieldAccess, current *BasicBlock) (hir.Operand, *BasicBlock) {
	flatObj, current := b.flattenExpr(e.Object, current)

	// Resolve the declaration-order field index from the object's struct type.
	// After flattening, flatObj is always an *hir.Identifier whose Type is the
	// struct type (preserved by every flattenExpr path that emits a TempDeclInst).
	fieldIndex := -1
	if ident, ok := flatObj.(*hir.Identifier); ok {
		if st, ok := ident.Type.(*types.StructType); ok {
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

	return &hir.Identifier{Value: tmp, Type: e.Type}, current
}

func (b *Builder) flattenIfExpr(expr *hir.IfExpr, current *BasicBlock) (hir.Operand, *BasicBlock) {
	// 1. Evaluate the condition explicitly in the current block
	flatCond, current := b.flattenExpr(expr.Condition, current)

	// 2. Provision the mandatory branch paths
	thenBlock := b.newBlock()
	mergeBlock := b.newBlock()
	elseBlock := mergeBlock

	if expr.Alternative != nil {
		elseBlock = b.newBlock()
	}

	// 3. Pre-allocate a temporary register ONLY if the type is not unit
	isUnit := false
	if _, ok := expr.Type.(types.UnitType); ok {
		isUnit = true
	}

	var resultTemp string
	var resultIdent *hir.Identifier

	if !isUnit {
		resultTemp = b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{
			Name: resultTemp,
			Type: expr.Type,
		})
		resultIdent = &hir.Identifier{Value: resultTemp, Type: expr.Type}
	} else {
		// Pass the sentinel identifier forward without declaring it
		resultIdent = &hir.Identifier{Value: "_unit", Type: types.UnitType{}}
	}

	// 4. Seal the current block with a conditional switch
	current.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  thenBlock.ID,
		FalseTarget: elseBlock.ID,
	}

	// 5. Build the 'Then' branch
	thenVal, thenEnd := b.flattenBlockExpr(expr.Consequence, thenBlock)
	if thenEnd != nil {
		if !isUnit {
			thenEnd.Statements = append(thenEnd.Statements, &AssignInst{Dst: resultTemp, RValue: thenVal})
		}
		if thenEnd.Terminator == nil {
			thenEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
		}
	}

	// 6. Build the 'Else' branch
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

	// 7. Return the temporary register holding the value, and the new active block
	return resultIdent, mergeBlock
}

// flattenBlockExpr lowers a block-as-expression by walking its statements and
// capturing the final yield value. This is the canonical path for any *hir.BlockStmt
// that appears as an expression — including match arm bodies and standalone block exprs.
//
// It replaces the duplicated buildBlockWithYield closure in flattenIfExpr.
func (b *Builder) flattenBlockExpr(block *hir.BlockStmt, current *BasicBlock) (hir.Operand, *BasicBlock) {
	if block == nil || len(block.Statements) == 0 {
		return &hir.Identifier{Value: "_unit", Type: types.UnitType{}}, current
	}

	stmts := block.Statements
	for i, stmt := range stmts {
		if current == nil {
			break
		}
		// If this is the last statement, check if it produces a value.
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

	return &hir.Identifier{Value: "_unit", Type: types.UnitType{}}, current
}

func (b *Builder) flattenCall(e *hir.CallExpr, current *BasicBlock) (hir.Operand, *BasicBlock) {
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

	return &hir.Identifier{Value: tmp, Type: e.Type}, current
}

func (b *Builder) flattenArrayLiteral(e *hir.ArrayLiteral, current *BasicBlock) (hir.Operand, *BasicBlock) {
	arrayType, ok := e.Type.(types.ArrayType)
	if !ok {
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		return &hir.Identifier{Value: tmp, Type: e.Type}, current
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

	return &hir.Identifier{Value: tmp, Type: arrayType}, current
}

// flattenVariantLiteral handles Some<T>, None, Ok<T>, Err<E>, etc.
func (b *Builder) flattenVariantLiteral(e *hir.VariantLiteral, current *BasicBlock) (hir.Operand, *BasicBlock) {
	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
	variant := e.Variant

	var flatPayloads []hir.Operand

	// 1. Flatten tuple arguments
	for _, arg := range e.Arguments {
		flatArg, next := b.flattenExpr(arg, current)
		current = next
		flatPayloads = append(flatPayloads, flatArg)
	}

	// 2. Flatten struct fields (using the new VariantField slice from Step 1)
	if len(e.Fields) > 0 {
		// Evaluate fields in the order the user wrote them (preserves side-effect order)
		evaluatedFields := make(map[string]hir.Operand)
		for _, field := range e.Fields {
			flatVal, next := b.flattenExpr(field.Value, current)
			current = next
			evaluatedFields[field.Name] = flatVal
		}

		// Pack them into the MIR payloads array in STRICT declaration order
		for _, declField := range variant.Fields {
			flatPayloads = append(flatPayloads, evaluatedFields[declField.Name])
		}
	}

	// 3. Emit the dedicated Variant instruction
	current.Statements = append(current.Statements, &VariantInitInst{
		Dst:          tmp,
		VariantName:  variant.Name,
		Discriminant: variant.Discriminant,
		Payloads:     flatPayloads,
		Type:         e.Type,
	})

	return &hir.Identifier{Value: tmp, Type: e.Type}, current
}

// flattenVecLiteral lowers Vec literals using the runtime constructor path
// by zero-allocating the vector and explicitly pushing each element.
func (b *Builder) flattenVecLiteral(e *hir.VecLiteral, current *BasicBlock) (hir.Operand, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})

	// Add creation initializer (pass the number of elements as initial capacity!)
	createFn := &hir.Identifier{Value: "maml_vec_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []MIRCallArg{
			{Argument: &hir.IntLiteral{Value: int64(len(e.Elements)), Type: types.IntType{}}},
		},
		Type: t,
	})

	// Explicitly emit CallInst for each push instead of synthesizing a CallExpr
	for _, elem := range e.Elements {
		var flatElem hir.Operand
		flatElem, current = b.flattenExpr(elem, current)

		pushFn := &hir.Identifier{Value: "maml_vec_push", Type: types.UnknownType{}}
		pushTmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: pushTmp, Type: types.UnitType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:      pushTmp,
			Function: pushFn,
			Arguments: []MIRCallArg{
				{Argument: &hir.Identifier{Value: tmp, Type: t}, Mut: true},
				{Argument: flatElem, Mut: false},
			},
			Type: types.UnitType{},
		})
	}

	return &hir.Identifier{Value: tmp, Type: t}, current
}

// flattenMapLiteral lowers Map literals using the runtime constructor path
// by zero-allocating the map and explicitly putting each key-value pair.
func (b *Builder) flattenMapLiteral(e *hir.MapLiteral, current *BasicBlock) (hir.Operand, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})

	// Add creation initializer
	createFn := &hir.Identifier{Value: "maml_map_create", Type: types.UnknownType{}}
	current.Statements = append(current.Statements, &CallInst{
		Dst:      tmp,
		Function: createFn,
		Arguments: []MIRCallArg{
			{Argument: &hir.IntLiteral{Value: 0, Type: types.IntType{}}},
			{Argument: &hir.IntLiteral{Value: 0, Type: types.IntType{}}},
		},
		Type: t,
	})

	for _, kv := range e.Elements {
		var flatVal hir.Operand
		// Only pre-flatten the value
		flatVal, current = b.flattenExpr(kv.Value, current)

		// Let lowerMapKey handle the flattening and unpacking of the key
		hashVal, ptrVal, lenVal, current := b.lowerMapKey(kv.Key, current)

		putFn := &hir.Identifier{Value: "maml_map_put", Type: types.UnknownType{}}
		putTmp := b.newTemp()

		current.Statements = append(current.Statements, &TempDeclInst{Name: putTmp, Type: types.UnitType{}})
		current.Statements = append(current.Statements, &CallInst{
			Dst:      putTmp,
			Function: putFn,
			Arguments: []MIRCallArg{
				{Argument: &hir.Identifier{Value: tmp, Type: t}, Mut: true},
				{Argument: hashVal},
				{Argument: flatVal},
				{Argument: ptrVal},
				{Argument: lenVal},
			},
			Type: types.UnitType{},
		})
	}
	return &hir.Identifier{Value: tmp, Type: t}, current
}

// lowerMapKey prepares the hash, str_ptr, and str_len arguments required
// by the runtime map ABI. It implements Step 1 of the map lowering phase.
func (b *Builder) lowerMapKey(keyExpr hir.Expr, current *BasicBlock) (hash, ptr, len hir.Operand, nextBlock *BasicBlock) {
	flatKey, current := b.flattenExpr(keyExpr, current)
	keyType := hir.TypeOf(flatKey)

	switch keyType.(type) {
	case types.IntType:
		// For integer keys: hash = zext(key), ptr = null, len = 0
		hashTmp := b.newTemp()

		// Declare the temporary for the hash and emit the cast
		current.Statements = append(current.Statements, &TempDeclInst{Name: hashTmp, Type: types.IntType{}})
		current.Statements = append(current.Statements, &CastInst{
			Dst:  hashTmp,
			Src:  flatKey,
			Type: types.IntType{}, // The backend will interpret this as the target bit-width (e.g., i64)
		})

		hashVal := &hir.Identifier{Value: hashTmp, Type: types.IntType{}}

		// ptr = null (0 typed as AnyType/ptr)
		ptrVal := &hir.IntLiteral{Value: 0, Type: types.AnyType{}}

		// len = 0
		lenVal := &hir.IntLiteral{Value: 0, Type: types.IntType{}}

		return hashVal, ptrVal, lenVal, current

	case types.StringType:
		// For string keys: extract ptr and len fields, then call maml_str_hash

		// 1. Extract ptr
		ptrTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: ptrTmp, Type: types.AnyType{}})
		current.Statements = append(current.Statements, &FieldReadInst{
			Dst:        ptrTmp,
			Object:     flatKey,
			FieldName:  "ptr",
			FieldIndex: 0,
			Type:       types.AnyType{},
		})
		ptrVal := &hir.Identifier{Value: ptrTmp, Type: types.AnyType{}}

		// 2. Extract len
		lenTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: lenTmp, Type: types.IntType{}})
		current.Statements = append(current.Statements, &FieldReadInst{
			Dst:        lenTmp,
			Object:     flatKey,
			FieldName:  "len",
			FieldIndex: 1,
			Type:       types.IntType{},
		})
		lenVal := &hir.Identifier{Value: lenTmp, Type: types.IntType{}}

		// 3. hash = call maml_str_hash(ptr, len)
		hashTmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: hashTmp, Type: types.IntType{}})

		// EMIT CALLINST DIRECTLY
		current.Statements = append(current.Statements, &CallInst{
			Dst:      hashTmp,
			Function: &hir.Identifier{Value: "maml_str_hash", Type: types.UnknownType{}},
			Arguments: []MIRCallArg{
				{Argument: ptrVal},
				{Argument: lenVal},
			},
			Type: types.IntType{},
		})
		hashVal := &hir.Identifier{Value: hashTmp, Type: types.IntType{}}

		return hashVal, ptrVal, lenVal, current

	default:
		// Failsafe for unhandled types falling through Sema
		return &hir.IntLiteral{Value: 0, Type: types.IntType{}},
			&hir.IntLiteral{Value: 0, Type: types.AnyType{}},
			&hir.IntLiteral{Value: 0, Type: types.IntType{}},
			current
	}
}
