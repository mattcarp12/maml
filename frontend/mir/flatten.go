package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// newTemp generates a unique temporary register name for the flattened MIR.
func (b *Builder) newTemp() string {
	b.tempCount++
	return fmt.Sprintf("_t%d", b.tempCount)
}

// flattenExpr eliminates nested expressions by materializing intermediate
// values into explicit, linear temporary variables inside the BasicBlock.
func (b *Builder) flattenExpr(expr tast.Expr, current *BasicBlock) (tast.Expr, *BasicBlock) {
	if expr == nil {
		return nil, current
	}

	switch e := expr.(type) {
	// -------------------------------------------------------------------------
	// 1. Base Cases (Already Flat)
	// -------------------------------------------------------------------------
	case *tast.Identifier, *tast.IntLiteral, *tast.BoolLiteral, *tast.StringLiteral:
		return e, current

	// -------------------------------------------------------------------------
	// 2. Complex Operations
	// -------------------------------------------------------------------------
	case *tast.InfixExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatRight, current := b.flattenExpr(e.Right, current)

		tmp := b.newTemp()

		// GUARD: Ensure type is never raw nil
		allocType := e.Type
		if allocType == nil {
			allocType = types.UnknownType{}
		}

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: allocType})

		flatOp := &tast.InfixExpr{Pos_: e.Pos_, Left: flatLeft, Operator: e.Operator, Right: flatRight, Type: allocType}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatOp})

		return &tast.Identifier{Value: tmp, Type: allocType}, current

	case *tast.PrefixExpr:
		flatRight, current := b.flattenExpr(e.Right, current)

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		flatOp := &tast.PrefixExpr{Pos_: e.Pos_, Operator: e.Operator, Right: flatRight, Type: e.Type}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatOp})

		return &tast.Identifier{Value: tmp, Type: e.Type}, current

	case *tast.CallExpr:
		return b.flattenCall(e, current)

	case *tast.AwaitExpr:
		// 1. Evaluate the task/promise being awaited
		flatTask, current := b.flattenExpr(e.Value, current)

		// 2. Provision the state machine continuation blocks
		resumeBlock := b.newBlock()
		cleanupBlock := b.newBlock()
		suspendBlock := b.newBlock()

		// 3. Emit the strict suspension boundary
		current.Terminator = &CoroSuspendTerminator{
			ResumeBlock:  resumeBlock.ID,
			CleanupBlock: cleanupBlock.ID,
			SuspendBlock: suspendBlock.ID,
		}

		// 4. Wire the secondary state machine paths to satisfy MIR termination rules
		// Cleanup unconditionally falls through to Suspend.
		cleanupBlock.Terminator = &JumpTerminator{Target: suspendBlock.ID}
		// Suspend returns control (the coroutine handle) back to the executor.
		suspendBlock.Terminator = &ReturnTerminator{}

		// 5. The result of the await is logically materialized in the resume block
		tmp := b.newTemp()
		resumeBlock.Statements = append(resumeBlock.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		// Map the resolved value into the temporary register
		resumeBlock.Statements = append(resumeBlock.Statements, &AssignInst{Dst: tmp, RValue: flatTask})

		// 6. Execution continues exclusively in the resume block!
		return &tast.Identifier{Value: tmp, Type: e.Type}, resumeBlock

	// -------------------------------------------------------------------------
	// 3. Struct Literal
	//
	// A named-struct literal is lowered into a sequence of flat instructions:
	//
	//   TempDeclInst   -- declares the struct register (backend: alloca)
	//   StructInitInst -- one per field, field-index resolved from StructType
	//
	// The caller receives an *tast.Identifier pointing at the new temporary.
	//
	// Map / Vec / SumType literals are NOT decomposed field-by-field here;
	// they pass through as a single AssignInst so the backend can route them
	// to the appropriate runtime constructor.
	// -------------------------------------------------------------------------
	case *tast.StructLiteral:
		return b.flattenStructLiteral(e, current)

	// -------------------------------------------------------------------------
	// 4. Field Access
	//
	// Flattened into TempDeclInst + FieldReadInst so the backend always
	// receives a flat (index-resolved) load rather than a nested expression.
	// -------------------------------------------------------------------------
	case *tast.FieldAccess:
		return b.flattenFieldAccess(e, current)

	case *tast.ArrayLiteral:
		return b.flattenArrayLiteral(e, current)

	case *tast.MapLiteral:
		return b.flattenMapLiteral(e, current)

	case *tast.VecLiteral:
		return b.flattenVecLiteral(e, current)

	case *tast.VariantLiteral:
		return b.flattenVariantLiteral(e, current)

	case *tast.SliceExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)

		var flatLow, flatHigh tast.Expr
		if e.Low != nil {
			flatLow, current = b.flattenExpr(e.Low, current)
		}
		if e.High != nil {
			flatHigh, current = b.flattenExpr(e.High, current)
		}

		// ContainerType is on the Left identifier after flattening.
		containerType := flatLeft.(*tast.Identifier).Type

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})
		current.Statements = append(current.Statements, &SliceInst{
			Dst:           tmp,
			Left:          flatLeft,
			ContainerType: containerType,
			Low:           flatLow,
			High:          flatHigh,
			ResultType:    e.Type,
		})

		return &tast.Identifier{Value: tmp, Type: e.Type}, current

	case *tast.MatchExpr:
		return b.flattenMatchExpr(e, current)

	case *tast.MethodCallExpr:
		return b.flattenMethodCall(e, current)

	case *tast.IndexExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatIndex, current := b.flattenExpr(e.Index, current)

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		flatIndexExpr := &tast.IndexExpr{Pos_: e.Pos_, Left: flatLeft, Index: flatIndex, Type: e.Type}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatIndexExpr})

		return &tast.Identifier{Value: tmp, Type: e.Type}, current

	case *tast.IfExpr:
		return b.flattenIfExpr(e, current)

	case *tast.BlockStmt:
		return b.flattenBlockExpr(e, current)
	}

	return expr, current
}

func (b *Builder) flattenStructLiteral(e *tast.StructLiteral, current *BasicBlock) (tast.Expr, *BasicBlock) {
	// =========================================================================
	// CASE 1: Named Structs (Field-by-Field Initialization)
	// =========================================================================
	if structType, isStruct := e.Type.(*types.StructType); isStruct {
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: structType})

		for _, field := range e.Fields {
			flatVal, nextBlock := b.flattenExpr(field.Value, current)
			current = nextBlock

			fieldName := ""
			if ident, ok := field.Key.(*tast.Identifier); ok && ident != nil {
				fieldName = ident.Value
			}

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
		return &tast.Identifier{Value: tmp, Type: structType}, current
	}

	// =========================================================================
	// CASE 2: Sum Types / Variants (Discriminant + Shifted Payload Initialization)
	// =========================================================================
	if sumTy, isSum := e.Type.(*types.SumType); isSum {
		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: sumTy})

		var targetVariant *types.SumVariant
		discriminant := -1

		// Step A: Check if Sema injected an explicit __discriminant (e.g., Unit Variants like `Quit`)
		for _, field := range e.Fields {
			if ident, ok := field.Key.(*tast.Identifier); ok && ident.Value == "__discriminant" {
				if intLit, ok := field.Value.(*tast.IntLiteral); ok {
					discriminant = int(intLit.Value)
					targetVariant = &sumTy.Variants[discriminant]
				}
				break
			}
		}

		// Step B: If not explicitly injected, infer the variant from the provided field names
		if targetVariant == nil {
			providedFields := make(map[string]bool)
			for _, field := range e.Fields {
				if ident, ok := field.Key.(*tast.Identifier); ok && ident != nil {
					providedFields[ident.Value] = true
				}
			}

			for i, v := range sumTy.Variants {
				match := true
				for _, vf := range v.Fields {
					if !providedFields[vf.Name] {
						match = false
						break
					}
				}
				// Ensure it's an exact match to avoid collisions
				if match && len(v.Fields) > 0 && len(v.Fields) == len(providedFields) {
					targetVariant = &sumTy.Variants[i]
					discriminant = targetVariant.Discriminant
					break
				}
			}
		}

		// Step C: Construct the ordered variant layout context array: [DiscriminantTy, Field1Ty, Field2Ty, ...]
		var variantLayout []types.Type
		if targetVariant != nil {
			variantLayout = append(variantLayout, types.IntType{}) // Index 0: Discriminant (__discriminant) is always i32
			for _, vf := range targetVariant.Fields {
				variantLayout = append(variantLayout, vf.Type) // Followed by payload types in strict order
			}
		}

		// Step D: Explicitly write the Discriminant to Index 0
		if discriminant != -1 {
			current.Statements = append(current.Statements, &StructInitInst{
				Dst:           tmp,
				FieldName:     "__discriminant",
				FieldIndex:    0,
				Value:         &tast.IntLiteral{Value: int64(discriminant), Type: types.IntType{}},
				VariantLayout: variantLayout, // <--- Attach here
			})
		}
		// Step D: Flatten and write the payload fields, shifted by +1
		for _, field := range e.Fields {
			fieldName := ""
			if ident, ok := field.Key.(*tast.Identifier); ok && ident != nil {
				fieldName = ident.Value
			}

			// Skip the synthetic discriminant if sema injected it
			if fieldName == "__discriminant" {
				continue
			}

			flatVal, nextBlock := b.flattenExpr(field.Value, current)
			current = nextBlock

			fieldIndex := 1
			if targetVariant != nil {
				for k, vf := range targetVariant.Fields {
					if vf.Name == fieldName {
						fieldIndex = k + 1
						break
					}
				}
			}

			current.Statements = append(current.Statements, &StructInitInst{
				Dst:           tmp,
				FieldName:     fieldName,
				FieldIndex:    fieldIndex,
				Value:         flatVal,
				VariantLayout: variantLayout,
			})
		}

		return &tast.Identifier{Value: tmp, Type: sumTy}, current
	}

	// =========================================================================
	// CASE 3: Maps, Vecs, and pass-through literals
	// =========================================================================
	tmp := b.newTemp()
	t := e.Type
	if t == nil {
		t = types.UnknownType{}
	}
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})
	current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: e})

	return &tast.Identifier{Value: tmp, Type: t}, current
}

// flattenFieldAccess lowers a *tast.FieldAccess into a TempDeclInst +
// FieldReadInst pair.  The resolved field index is carried on the instruction
// so the backend can emit a getelementptr without re-walking the type tree.
func (b *Builder) flattenFieldAccess(e *tast.FieldAccess, current *BasicBlock) (tast.Expr, *BasicBlock) {
	flatObj, current := b.flattenExpr(e.Object, current)

	// Resolve the declaration-order field index from the object's struct type.
	// After flattening, flatObj is always an *tast.Identifier whose Type is the
	// struct type (preserved by every flattenExpr path that emits a TempDeclInst).
	fieldIndex := -1
	if ident, ok := flatObj.(*tast.Identifier); ok {
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

	return &tast.Identifier{Value: tmp, Type: e.Type}, current
}

func (b *Builder) flattenIfExpr(expr *tast.IfExpr, current *BasicBlock) (tast.Expr, *BasicBlock) {
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
	var resultIdent *tast.Identifier

	if !isUnit {
		resultTemp = b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{
			Name: resultTemp,
			Type: expr.Type,
		})
		resultIdent = &tast.Identifier{Value: resultTemp, Type: expr.Type}
	} else {
		// Pass the sentinel identifier forward without declaring it
		resultIdent = &tast.Identifier{Value: "_unit", Type: types.UnitType{}}
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
// capturing the final yield value. This is the canonical path for any *tast.BlockStmt
// that appears as an expression — including match arm bodies and standalone block exprs.
//
// It replaces the duplicated buildBlockWithYield closure in flattenIfExpr.
func (b *Builder) flattenBlockExpr(block *tast.BlockStmt, current *BasicBlock) (tast.Expr, *BasicBlock) {
	if block == nil || len(block.Statements) == 0 {
		return &tast.Identifier{Value: "_unit", Type: types.UnitType{}}, current
	}

	stmts := block.Statements
	for i, stmt := range stmts {
		if current == nil {
			break
		}
		// If this is the last statement, check if it produces a value.
		if i == len(stmts)-1 {
			if yieldStmt, ok := stmt.(*tast.YieldStmt); ok {
				flatVal, nextBlock := b.flattenExpr(yieldStmt.Value, current)
				return flatVal, nextBlock
			}
			if exprStmt, ok := stmt.(*tast.ExprStmt); ok {
				flatVal, nextBlock := b.flattenExpr(exprStmt.Value, current)
				return flatVal, nextBlock
			}
		}
		current = b.buildStmt(stmt, current)
	}

	return &tast.Identifier{Value: "_unit", Type: types.UnitType{}}, current
}

// flattenCall explicitly materializes function arguments into temporary slots
// and enforces affine/borrowing capabilities via linear MIR instructions.
func (b *Builder) flattenCall(e *tast.CallExpr, current *BasicBlock) (tast.Expr, *BasicBlock) {
	var flatArgs []tast.CallArg

	for _, arg := range e.Arguments {
		// 1. Evaluate the argument deeply (Left-to-Right execution)
		flatArg, nextBlock := b.flattenExpr(arg.Argument, current)
		current = nextBlock

		// 2. Extract type safely
		var argType types.Type
		if flatArg != nil {
			switch a := flatArg.(type) {
			case *tast.Identifier:
				argType = a.Type
			case *tast.IntLiteral:
				argType = a.Type
			case *tast.BoolLiteral:
				argType = a.Type
			case *tast.StringLiteral:
				argType = a.Type
			}
		}

		// 3. Materialize the explicit argument slot
		argTmp := b.newTemp()
		if argType != nil {
			current.Statements = append(current.Statements, &TempDeclInst{Name: argTmp, Type: argType})
		}

		// 4. ABI Lowering & Capability Enforcement
		if ident, isIdent := flatArg.(*tast.Identifier); isIdent {
			// Argument is a registered variable - we can enforce ownership/borrowing
			if arg.Own {
				current.Statements = append(current.Statements, &MoveInst{Dst: argTmp, Src: ident.Value})
			} else if arg.Mut {
				current.Statements = append(current.Statements, &MutBorrowInst{Src: ident.Value})
				current.Statements = append(current.Statements, &AssignInst{Dst: argTmp, RValue: flatArg})
			} else {
				// Primitive Copy vs Immutable Reference
				isPrimitive := false
				if argType != nil {
					switch argType.(type) {
					case types.IntType, types.BoolType, types.UnitType:
						isPrimitive = true
					}
				}
				if isPrimitive {
					current.Statements = append(current.Statements, &CopyInst{Dst: argTmp, Src: ident.Value})
				} else {
					current.Statements = append(current.Statements, &AssignInst{Dst: argTmp, RValue: flatArg})
				}
			}
		} else {
			// Argument is a literal value (e.g., 10, true, "hello")
			// Emit a direct assignment instead of a copy instruction
			current.Statements = append(current.Statements, &AssignInst{Dst: argTmp, RValue: flatArg})
		}

		flatArgs = append(flatArgs, tast.CallArg{
			Pos_:     arg.Pos_,
			Argument: &tast.Identifier{Value: argTmp, Type: argType},
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}

	// 5. Evaluate the target function
	flatFn, current := b.flattenExpr(e.Function, current)

	// 6. Emit the Call
	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

	flatCall := &tast.CallExpr{Pos_: e.Pos_, Function: flatFn, Arguments: flatArgs, Type: e.Type}
	current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatCall})

	return &tast.Identifier{Value: tmp, Type: e.Type}, current
}

func (b *Builder) flattenMethodCall(expr *tast.MethodCallExpr, current *BasicBlock) (tast.Expr, *BasicBlock) {
	// 1. Flatten the receiver object (the 'self' pointer)
	flatObj, current := b.flattenExpr(expr.Object, current)

	// 2. Prepare the arguments array, injecting the receiver as Arg 0
	// (Note: Adjust 'tast.CallArg' to match your actual TAST argument struct if it differs)
	var flatArgs []tast.CallArg
	flatArgs = append(flatArgs, tast.CallArg{
		Argument: flatObj,
		// If your language uses 'mut self', you can check expr.Method.Symbol.ParamMode here
	})

	// 3. Flatten the remaining explicit arguments
	for _, arg := range expr.Arguments {
		var flatArg tast.Expr
		flatArg, current = b.flattenExpr(arg.Argument, current)

		flatArgs = append(flatArgs, tast.CallArg{
			Argument: flatArg,
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}

	// 4. Construct a standard CallExpr using the resolved method symbol
	desugaredCall := &tast.CallExpr{
		Pos_: expr.Pos_,
		Function: &tast.Identifier{
			Pos_:   expr.Method.Pos_,
			Value:  expr.Method.Symbol.Name, // e.g., "Player_move" instead of "move"
			Type:   expr.Method.Type,
			Symbol: expr.Method.Symbol,
		},
		Arguments: flatArgs,
		Type:      expr.Type,
	}

	return desugaredCall, current
}

// flattenArrayLiteral lowers a *tast.ArrayLiteral into the flat MIR
// instruction sequence:
//
//	TempDeclInst    -- declares the array register (backend: alloca)
//	ArrayInitInst   -- one per element, carrying the element index and value
//
// This mirrors the struct literal approach exactly: decompose field-by-field
// so the backend never needs to inspect tast nodes directly.
func (b *Builder) flattenArrayLiteral(e *tast.ArrayLiteral, current *BasicBlock) (tast.Expr, *BasicBlock) {
	arrayType := e.Type.(types.ArrayType)

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

	return &tast.Identifier{Value: tmp, Type: arrayType}, current
}

func (b *Builder) flattenMatchExpr(expr *tast.MatchExpr, current *BasicBlock) (tast.Expr, *BasicBlock) {
	// 1. Flatten the subject into a temp.
	flatSubject, current := b.flattenExpr(expr.Subject, current)

	// 2. Pre-allocate the result temp ONLY if the type is not unit
	isUnit := false
	if _, ok := expr.Type.(types.UnitType); ok {
		isUnit = true
	}

	var resultTemp string
	var resultIdent *tast.Identifier

	if !isUnit {
		resultTemp = b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{
			Name: resultTemp,
			Type: expr.Type,
		})
		resultIdent = &tast.Identifier{Value: resultTemp, Type: expr.Type}
	} else {
		resultIdent = &tast.Identifier{Value: "_unit", Type: types.UnitType{}}
	}

	mergeBlock := b.newBlock()

	// For variant patterns we need the discriminant once; load it here.
	// We re-use it across all arms so it is only loaded once in the dispatch block.
	var flatDiscriminant tast.Expr
	if len(expr.Arms) > 0 {
		if _, isVariant := expr.Arms[0].Pattern.(*tast.VariantPattern); isVariant {
			discrimTmp := b.newTemp()
			subjectIdent := flatSubject.(*tast.Identifier)
			current.Statements = append(current.Statements, &TempDeclInst{
				Name: discrimTmp,
				Type: types.IntType{},
			})
			current.Statements = append(current.Statements, &FieldReadInst{
				Dst:        discrimTmp,
				Object:     subjectIdent,
				FieldName:  "__discriminant",
				FieldIndex: 0,
				Type:       types.IntType{},
			})
			flatDiscriminant = &tast.Identifier{Value: discrimTmp, Type: types.IntType{}}
		}
	}

	// 3. Build each arm. dispatchBlock is where each new comparison is emitted;
	//    it advances as we chain arms together.
	dispatchBlock := current

	for i, arm := range expr.Arms {
		armBlock := b.newBlock()
		var nextDispatchBlock *BasicBlock

		switch pat := arm.Pattern.(type) {
		case *tast.WildcardPattern:
			// Unconditional: terminate the dispatch block with a direct jump.
			dispatchBlock.Terminator = &JumpTerminator{Target: armBlock.ID}
			// Wildcard is always last (exhaustiveness checked by sema), no next dispatch needed.

		case *tast.LiteralPattern:
			nextDispatchBlock = b.newBlock()
			flatLiteral, _ := b.flattenExpr(pat.Value, dispatchBlock)
			cmp := b.newTemp()
			dispatchBlock.Statements = append(dispatchBlock.Statements, &TempDeclInst{
				Name: cmp,
				Type: types.BoolType{},
			})
			dispatchBlock.Statements = append(dispatchBlock.Statements, &AssignInst{
				Dst: cmp,
				RValue: &tast.InfixExpr{
					Left:     flatSubject,
					Operator: "==",
					Right:    flatLiteral,
					Type:     types.BoolType{},
				},
			})
			dispatchBlock.Terminator = &BranchTerminator{
				Condition:   &tast.Identifier{Value: cmp, Type: types.BoolType{}},
				TrueTarget:  armBlock.ID,
				FalseTarget: nextDispatchBlock.ID,
			}

		case *tast.VariantPattern:
			isLast := i == len(expr.Arms)-1
			if !isLast {
				nextDispatchBlock = b.newBlock()
			} else {
				// Last variant arm: the "false" branch is unreachable
				// (sema guarantees exhaustiveness), but we still need a valid target.
				nextDispatchBlock = b.newBlock()
				nextDispatchBlock.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
			discriminant := pat.Symbol.Variant.Discriminant
			discrimConst := &tast.IntLiteral{Value: int64(discriminant), Type: types.IntType{}}
			cmp := b.newTemp()
			dispatchBlock.Statements = append(dispatchBlock.Statements, &TempDeclInst{
				Name: cmp,
				Type: types.BoolType{},
			})
			dispatchBlock.Statements = append(dispatchBlock.Statements, &AssignInst{
				Dst: cmp,
				RValue: &tast.InfixExpr{
					Left:     flatDiscriminant,
					Operator: "==",
					Right:    discrimConst,
					Type:     types.BoolType{},
				},
			})
			dispatchBlock.Terminator = &BranchTerminator{
				Condition:   &tast.Identifier{Value: cmp, Type: types.BoolType{}},
				TrueTarget:  armBlock.ID,
				FalseTarget: nextDispatchBlock.ID,
			}

			// 1. Build the specific memory layout for this variant arm
			var variantLayout []types.Type
			variantLayout = append(variantLayout, types.IntType{}) // Index 0: Discriminant
			// Append tuple payloads if they exist
			for _, t := range pat.Symbol.Variant.TupleTypes {
				variantLayout = append(variantLayout, t)
			}
			// Append struct payloads if they exist
			for _, f := range pat.Symbol.Variant.Fields {
				variantLayout = append(variantLayout, f.Type)
			}

			// Emit binding extractions at the top of the arm block.
			// Tuple bindings: payload field 0, 1, 2...
			for j, binding := range pat.TupleBindings {
				armBlock.Statements = append(armBlock.Statements, &TempDeclInst{
					Name: binding.Value,
					Type: binding.Type,
				})
				armBlock.Statements = append(armBlock.Statements, &FieldReadInst{
					Dst:           binding.Value,
					Object:        flatSubject,
					FieldName:     fmt.Sprintf("__payload_%d", j),
					FieldIndex:    j + 1, // field 0 is discriminant, payload starts at 1
					Type:          binding.Type,
					VariantLayout: variantLayout,
				})
			}
			// Struct field bindings.
			for _, field := range pat.Fields {
				if field.Binding == nil {
					continue
				}
				variant := pat.Symbol.Variant
				fieldIdx := -1
				for k, vf := range variant.Fields {
					if vf.Name == field.Field {
						fieldIdx = k
						break
					}
				}
				armBlock.Statements = append(armBlock.Statements, &TempDeclInst{
					Name: field.Binding.Value,
					Type: field.Binding.Type,
				})
				armBlock.Statements = append(armBlock.Statements, &FieldReadInst{
					Dst:           field.Binding.Value,
					Object:        flatSubject,
					FieldName:     field.Field,
					FieldIndex:    fieldIdx + 1, // +1 past discriminant
					Type:          field.Binding.Type,
					VariantLayout: variantLayout,
				})
			}
		}

		// 4. Flatten the arm body and capture its result into resultTemp.
		flatBody, armEnd := b.flattenExpr(arm.Body, armBlock)
		if armEnd != nil {
			// Guard the assignment
			if !isUnit {
				armEnd.Statements = append(armEnd.Statements, &AssignInst{
					Dst:    resultTemp,
					RValue: flatBody,
				})
			}
			if armEnd.Terminator == nil {
				armEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
		}

		if nextDispatchBlock != nil {
			dispatchBlock = nextDispatchBlock
		}
	}

	return resultIdent, mergeBlock
}

// flattenVariantLiteral handles Some<T>, None, Ok<T>, Err<E>, etc.
func (b *Builder) flattenVariantLiteral(e *tast.VariantLiteral, current *BasicBlock) (tast.Expr, *BasicBlock) {
	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

	// sumTy := e.Type.(*types.SumType)
	variant := e.Variant

	// Emit discriminant
	current.Statements = append(current.Statements, &StructInitInst{
		Dst:        tmp,
		FieldName:  "__discriminant",
		FieldIndex: 0,
		Value:      &tast.IntLiteral{Value: int64(variant.Discriminant), Type: types.IntType{}},
	})

	// Emit payload (if any)
	for i, arg := range e.Arguments {
		flatArg, next := b.flattenExpr(arg, current)
		current = next

		current.Statements = append(current.Statements, &StructInitInst{
			Dst:        tmp,
			FieldName:  fmt.Sprintf("__payload_%d", i),
			FieldIndex: i + 1,
			Value:      flatArg,
		})
	}

	return &tast.Identifier{Value: tmp, Type: e.Type}, current
}

// flattenVecLiteral lowers Vec literals using the runtime constructor path
// by zero-allocating the vector and explicitly pushing each element.
func (b *Builder) flattenVecLiteral(e *tast.VecLiteral, current *BasicBlock) (tast.Expr, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})

	// 1. Emit the zero-allocation (alloc_composite with no elements)
	emptyVec := &tast.VecLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: nil, // Strip elements so the backend just allocates the base struct
		Type:     t,
	}
	current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: emptyVec})

	// 2. Synthesize method calls to push each element sequentially
	vecIdent := &tast.Identifier{Value: tmp, Type: t}
	pushSym := &types.Symbol{
		Kind: types.FuncSymbol,
		Name: "maml_vec_push", // Maps to the mangled runtime function
	}

	for _, elem := range e.Elements {
		pushCall := &tast.MethodCallExpr{
			Pos_:   e.Pos_,
			Object: vecIdent,
			Method: &tast.Identifier{
				Value:  "maml_vec_push",
				Symbol: pushSym,
			},
			// The compiler enforces mutability on vec.push() via the Mut flag
			Arguments: []tast.CallArg{{Argument: elem, Mut: true}},
			Type:      types.UnitType{},
		}

		// Recursively flatten the synthetic method call
		_, current = b.flattenMethodCall(pushCall, current)
	}

	return vecIdent, current
}

// flattenMapLiteral lowers Map literals using the runtime constructor path
// by zero-allocating the map and explicitly putting each key-value pair.
func (b *Builder) flattenMapLiteral(e *tast.MapLiteral, current *BasicBlock) (tast.Expr, *BasicBlock) {
	tmp := b.newTemp()
	t := e.Type
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: t})

	// 1. Emit the zero-allocation (alloc_composite with no elements)
	emptyMap := &tast.MapLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: nil, // Strip elements so the backend just allocates the base struct
		Type:     t,
	}
	current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: emptyMap})

	// 2. Synthesize method calls to put each key-value pair sequentially
	mapIdent := &tast.Identifier{Value: tmp, Type: t}
	putSym := &types.Symbol{
		Kind: types.FuncSymbol,
		Name: "maml_map_put", // Maps to the mangled runtime function
	}

	for _, el := range e.Elements {
		putCall := &tast.MethodCallExpr{
			Pos_:   e.Pos_,
			Object: mapIdent,
			Method: &tast.Identifier{
				Value:  "maml_map_put",
				Symbol: putSym,
			},
			Arguments: []tast.CallArg{
				{Argument: el.Key},
				{Argument: el.Value},
			},
			Type: types.UnitType{},
		}

		// Recursively flatten the synthetic method call
		_, current = b.flattenMethodCall(putCall, current)
	}

	return mapIdent, current
}
