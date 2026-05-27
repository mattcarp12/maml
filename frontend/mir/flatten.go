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
func (b *Builder) flattenExpr(expr hir.Expr, current *BasicBlock) (hir.Expr, *BasicBlock) {
	if expr == nil {
		return nil, current
	}

	switch e := expr.(type) {
	// -------------------------------------------------------------------------
	// 1. Base Cases (Already Flat)
	// -------------------------------------------------------------------------
	case *hir.Identifier, *hir.IntLiteral, *hir.BoolLiteral, *hir.StringLiteral:
		return e, current

	// -------------------------------------------------------------------------
	// 2. Complex Operations
	// -------------------------------------------------------------------------
	case *hir.InfixExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatRight, current := b.flattenExpr(e.Right, current)

		tmp := b.newTemp()

		// GUARD: Ensure type is never raw nil
		allocType := e.Type
		if allocType == nil {
			allocType = types.UnknownType{}
		}

		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: allocType})

		flatOp := &hir.InfixExpr{Pos_: e.Pos_, Left: flatLeft, Operator: e.Operator, Right: flatRight, Type: allocType}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatOp})

		return &hir.Identifier{Value: tmp, Type: allocType}, current
	case *hir.PrefixExpr:
		flatRight, current := b.flattenExpr(e.Right, current)

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		flatOp := &hir.PrefixExpr{Pos_: e.Pos_, Operator: e.Operator, Right: flatRight, Type: e.Type}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatOp})

		return &hir.Identifier{Value: tmp, Type: e.Type}, current

	case *hir.CallExpr:
		return b.flattenCall(e, current)

	case *hir.AwaitExpr:
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
		return &hir.Identifier{Value: tmp, Type: e.Type}, resumeBlock

	case *hir.FieldAccess:
		flatObj, current := b.flattenExpr(e.Object, current)

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		flatAccess := &hir.FieldAccess{Pos_: e.Pos_, Object: flatObj, Field: e.Field, Type: e.Type}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatAccess})

		return &hir.Identifier{Value: tmp, Type: e.Type}, current

	case *hir.IndexExpr:
		flatLeft, current := b.flattenExpr(e.Left, current)
		flatIndex, current := b.flattenExpr(e.Index, current)

		tmp := b.newTemp()
		current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

		flatIndexExpr := &hir.IndexExpr{Pos_: e.Pos_, Left: flatLeft, Index: flatIndex, Type: e.Type}
		current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatIndexExpr})

		return &hir.Identifier{Value: tmp, Type: e.Type}, current

	case *hir.IfExpr:
		return b.flattenIfExpr(e, current)
	}

	return expr, current
}

func (b *Builder) flattenIfExpr(expr *hir.IfExpr, current *BasicBlock) (hir.Expr, *BasicBlock) {
	// 1. Evaluate the condition explicitly in the current block
	flatCond, current := b.flattenExpr(expr.Condition, current)

	// 2. Provision the mandatory branch paths
	thenBlock := b.newBlock()
	mergeBlock := b.newBlock()
	elseBlock := mergeBlock

	if expr.Alternative != nil {
		elseBlock = b.newBlock()
	}

	// 3. Pre-allocate a temporary register to hold the result of the if/else
	resultTemp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{
		Name: resultTemp,
		Type: expr.Type,
	})
	resultIdent := &hir.Identifier{Value: resultTemp, Type: expr.Type}

	// 4. Seal the current block with a conditional switch
	current.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  thenBlock.ID,
		FalseTarget: elseBlock.ID,
	}

	// Helper inline function to lower a block and bind its final yielded expression to the destination register
	buildBlockWithYield := func(body *hir.BlockStmt, startBlock *BasicBlock) *BasicBlock {
		if body == nil {
			return startBlock
		}
		curr := startBlock
		for i, stmt := range body.Statements {
			if curr == nil {
				break
			}
			// If this is the final statement in the block, check if it yields an expression value
			if i == len(body.Statements)-1 {
				// 1. Handle standard expression statement yields
				if exprStmt, ok := stmt.(*hir.ExprStmt); ok {
					var flatVal hir.Expr
					flatVal, curr = b.flattenExpr(exprStmt.Value, curr)
					if curr != nil {
						curr.Statements = append(curr.Statements, &AssignInst{Dst: resultTemp, RValue: flatVal})
					}
					continue
				}

				// 2. FIXED: Handle explicit block values produced by desugared aggregates and match expressions
				if yieldStmt, ok := stmt.(*hir.YieldStmt); ok {
					var flatVal hir.Expr
					flatVal, curr = b.flattenExpr(yieldStmt.Value, curr)
					if curr != nil {
						curr.Statements = append(curr.Statements, &AssignInst{Dst: resultTemp, RValue: flatVal})
					}
					continue
				}
			}
			curr = b.buildStmt(stmt, curr)
		}
		return curr
	}

	// 5. Build the 'Then' branch with expression evaluation tracking
	thenEnd := buildBlockWithYield(expr.Consequence, thenBlock)
	if thenEnd != nil {
		if thenEnd.Terminator == nil {
			thenEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
		}
	}

	// 6. Build the 'Else' branch with expression evaluation tracking
	if expr.Alternative != nil {
		elseEnd := buildBlockWithYield(expr.Alternative, elseBlock)
		if elseEnd != nil {
			if elseEnd.Terminator == nil {
				elseEnd.Terminator = &JumpTerminator{Target: mergeBlock.ID}
			}
		}
	}

	// 7. Return the temporary register holding the value, and the new active block
	return resultIdent, mergeBlock
}

// flattenCall explicitly materializes function arguments into temporary slots
// and enforces affine/borrowing capabilities via linear MIR instructions.
func (b *Builder) flattenCall(e *hir.CallExpr, current *BasicBlock) (hir.Expr, *BasicBlock) {
	var flatArgs []hir.CallArg

	for _, arg := range e.Arguments {
		// 1. Evaluate the argument deeply (Left-to-Right execution)
		flatArg, nextBlock := b.flattenExpr(arg.Argument, current)
		current = nextBlock

		// 2. Extract source identity and type
		var srcName string
		var argType types.Type

		switch a := flatArg.(type) {
		case *hir.Identifier:
			srcName = a.Value
			argType = a.Type
		case *hir.IntLiteral:
			srcName = fmt.Sprintf("%d", a.Value)
			argType = a.Type
		case *hir.BoolLiteral:
			srcName = fmt.Sprintf("%t", a.Value)
			argType = a.Type
		case *hir.StringLiteral:
			srcName = fmt.Sprintf("\"%s\"", a.Value)
			argType = a.Type
		default:
			srcName = flatArg.String()
		}

		// 3. Materialize the explicit argument slot
		argTmp := b.newTemp()
		if argType != nil {
			current.Statements = append(current.Statements, &TempDeclInst{Name: argTmp, Type: argType})
		}

		// 4. ABI Lowering & Capability Enforcement
		if arg.Own {
			// Permanent Ownership Transfer
			current.Statements = append(current.Statements, &MoveInst{
				Dst: argTmp,
				Src: srcName,
			})
		} else if arg.Mut {
			// 1. Emit the zero-cost exclusivity marker for the Frontend
			current.Statements = append(current.Statements, &MutBorrowInst{
				Src: srcName,
			})
			// 2. Emit the actual structural routing for the Backend
			current.Statements = append(current.Statements, &AssignInst{
				Dst:    argTmp,
				RValue: &hir.Identifier{Value: srcName, Type: argType},
			})
		} else {
			// Standard Borrow or Primitive Copy
			isPrimitive := false
			if argType != nil {
				switch argType.(type) {
				case types.IntType, types.BoolType, types.UnitType:
					isPrimitive = true
				}
			}

			if isPrimitive {
				current.Statements = append(current.Statements, &CopyInst{Dst: argTmp, Src: srcName})
			} else {
				// Immutable borrow passes a reference pointer.
				// No RefInc is needed because the call scope is strictly bound to the caller.
				current.Statements = append(current.Statements, &AssignInst{Dst: argTmp, RValue: flatArg})
			}
		}

		flatArgs = append(flatArgs, hir.CallArg{
			Pos_:     arg.Pos_,
			Argument: &hir.Identifier{Value: argTmp, Type: argType},
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}

	// 5. Evaluate the target function
	flatFn, current := b.flattenExpr(e.Function, current)

	// 6. Emit the Call
	tmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: tmp, Type: e.Type})

	flatCall := &hir.CallExpr{Pos_: e.Pos_, Function: flatFn, Arguments: flatArgs, Type: e.Type}
	current.Statements = append(current.Statements, &AssignInst{Dst: tmp, RValue: flatCall})

	return &hir.Identifier{Value: tmp, Type: e.Type}, current
}
