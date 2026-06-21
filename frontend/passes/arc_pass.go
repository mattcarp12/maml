package passes

import (
	"fmt"
	"sort"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// InjectARC enforces the Phase 7 Ownership model by inserting RefIncInst and RefDecInst.
func InjectARC(g *mir.Graph, liveness *LivenessResult, escapes map[string]EscapeState) {
	// 1. Pre-scan to map variable names to their Types
	varTypes := make(map[string]types.Type)
	for _, block := range g.SortedBlocks() {
		for _, inst := range block.Statements {
			switch i := inst.(type) {
			case *mir.TempDeclInst:
				varTypes[i.Name] = i.Type
			case *mir.RefAllocInst:
				varTypes[i.Dst] = i.Type
			}
		}
	}

	isRef := func(name string) bool {
		t, ok := varTypes[name]
		// Only inject ARC if the type is explicitly marked as needing reference counting.
		return ok && t != nil && t.IsNeedsARC()
	}

	isEscaped := func(name string) bool {
		// If it's not explicitly marked OnHeap, we assume it's OnStack
		return escapes[name] == OnHeap
	}

	// Helper to generate a fresh temporary register name
	tempCount := 0
	newTemp := func() string {
		tempCount++
		return fmt.Sprintf("_drop_t%d", tempCount)
	}

	// Recursive drop unroller
	var emitDrop func(name string, t types.Type) []mir.Instruction
	emitDrop = func(name string, t types.Type) []mir.Instruction {
		var drops []mir.Instruction

		// Base Case: It's a raw reference type (like Vec or Map)
		// Assuming you have a way to distinguish raw references from composites.
		if st, isStruct := t.(*types.StructType); isStruct {
			// Recursive Case: Unroll the struct
			for i, field := range st.Fields {
				if field.Type.IsNeedsARC() {
					tempName := newTemp()

					// 1. Extract the nested field
					drops = append(drops, &mir.TempDeclInst{
						Name: tempName,
						Type: field.Type,
					})
					drops = append(drops, &mir.FieldReadInst{
						Dst:        tempName,
						Object:     &mir.Register{Name: name, Type: st},
						FieldName:  field.Name,
						FieldIndex: i,
						Type:       field.Type,
					})

					// 2. Recursively drop the extracted field
					drops = append(drops, emitDrop(tempName, field.Type)...)
				}
			}
		} else if t.IsNeedsARC() {
			// It's a base ARC type, just decrement it
			drops = append(drops, &mir.RefDecInst{Src: name})
		}

		return drops
	}

	// 2. Single-Pass Block Injection
	for _, block := range g.SortedBlocks() {
		var newStmts []mir.Instruction
		activeRefs := make(map[string]bool)

		// Find all predecessors for this block
		var preds []mir.BlockID
		for _, p := range g.SortedBlocks() {
			if term := p.Terminator; term != nil {
				switch t := term.(type) {
				case *mir.JumpTerminator:
					if t.Target == block.ID {
						preds = append(preds, p.ID)
					}
				case *mir.BranchTerminator:
					if t.TrueTarget == block.ID || t.FalseTarget == block.ID {
						preds = append(preds, p.ID)
					}
				}
			}
		}

		// Catch variables that died on the edge between blocks
		var edgeDying []string
		for _, predID := range preds {
			for v := range liveness.LiveOut[predID] {
				// If it was alive in the previous block, but isn't alive here, it died on the jump!
				if isRef(v) && !liveness.LiveIn[block.ID][v] {
					edgeDying = append(edgeDying, v)
				}
			}
		}

		// Sort and emit edge-death releases at the very top of the block
		sort.Strings(edgeDying)
		lastEdgeDrop := "" // deduplicate if multiple paths drop the same variable
		for _, v := range edgeDying {
			if v != lastEdgeDrop {
				newStmts = append(newStmts, &mir.RefDecInst{Src: v})
				lastEdgeDrop = v
			}
		}

		// Seed active references from the dataflow LiveIn set
		for v := range liveness.LiveIn[block.ID] {
			if isRef(v) {
				activeRefs[v] = true
			}
		}

		// --- Local Last-Use Scanner ---
		lastUseIdx := make(map[string]int)
		terminatorUses := make(map[string]bool)

		// 1a. Check if variables are kept alive by the terminator
		if ret, ok := block.Terminator.(*mir.ReturnTerminator); ok && ret.Value != nil {
			if reg, isReg := ret.Value.(*mir.Register); isReg {
				terminatorUses[reg.Name] = true
			}
		}
		if br, ok := block.Terminator.(*mir.BranchTerminator); ok && br.Condition != nil {
			if reg, isReg := br.Condition.(*mir.Register); isReg {
				terminatorUses[reg.Name] = true
			}
		}

		// 1b. Find the final statement index for every variable read
		for idx, inst := range block.Statements {
			markUse := func(v mir.Value) {
				if reg, ok := v.(*mir.Register); ok {
					lastUseIdx[reg.Name] = idx
				}
			}
			switch i := inst.(type) {
			case *mir.AssignInst:
				markUse(i.RValue)
			case *mir.CopyInst:
				lastUseIdx[i.Src] = idx
			case *mir.FieldReadInst:
				markUse(i.Object)
			case *mir.CallInst:
				markUse(i.Function)
				for _, arg := range i.Arguments {
					markUse(arg.Argument)
				}
			case *mir.StructInitInst:
				markUse(i.Value)
			}
		}

		// Helper to detect implicit moves
		isImplicitMove := func(varName string, currentIdx int) bool {
			// If it escapes the block or is used in the terminator, it's not a move.
			if liveness.LiveOut[block.ID][varName] || terminatorUses[varName] {
				return false
			}
			// If it is dying, and this is its final use, it's a move!
			return lastUseIdx[varName] == currentIdx
		}

		for idx, inst := range block.Statements {
			// A. Reassignment Release
			// If we are overwriting an existing reference, we must release the old data first.
			dst := getDef(inst)
			if dst != "" && isRef(dst) && activeRefs[dst] {
				varType := varTypes[dst]
				newStmts = append(newStmts, emitDrop(dst, varType)...)
			}

			// Keep the original instruction
			newStmts = append(newStmts, inst)

			if dst != "" && isRef(dst) {
				activeRefs[dst] = true
			}

			// B. Retain Injection (Shared Aliases)
			// Assigning a reference type via a plain identifier creates an immutable shared alias.
			switch i := inst.(type) {
			case *mir.AssignInst:
				if isRef(i.Dst) {
					if reg, isReg := i.RValue.(*mir.Register); isReg {
						if isImplicitMove(reg.Name, idx) {
							delete(activeRefs, reg.Name)
						} else if !isEscaped(reg.Name) {
							// RC ELISION: Variable is stack-bound. Skip the atomic increment!
						} else {
							newStmts = append(newStmts, &mir.RefIncInst{Src: reg.Name})
						}
					}
				}
			case *mir.CopyInst:
				if isRef(i.Dst) {
					if isImplicitMove(i.Src, idx) {
						delete(activeRefs, i.Src)
					} else if !isEscaped(i.Src) {
						// RC ELISION
					} else {
						newStmts = append(newStmts, &mir.RefIncInst{Src: i.Src})
					}
				}
			case *mir.FieldReadInst:
				if isRef(i.Dst) {
					if !isEscaped(i.Dst) {
						// RC ELISION
					} else {
						newStmts = append(newStmts, &mir.RefIncInst{Src: i.Dst})
					}
				}
			case *mir.CallInst:
				// If we pass an ARC argument by value for the last time, it's a move into the function!
				// for _, arg := range i.Arguments {
				// 	if reg, isReg := arg.Argument.(*mir.Register); isReg && isRef(reg.Name) {
				// 		if isImplicitMove(reg.Name, idx) && !arg.Mut {
				// 			delete(activeRefs, reg.Name)
				// 		}
				// 	}
				// }
			case *mir.StructInitInst:
				// Populating a struct field with an ARC type
				if reg, isReg := i.Value.(*mir.Register); isReg && isRef(reg.Name) {
					if isImplicitMove(reg.Name, idx) {
						delete(activeRefs, reg.Name) // Implicit move into the struct!
					} else if !isEscaped(reg.Name) {
						// RC ELISION
					} else {
						newStmts = append(newStmts, &mir.RefIncInst{Src: reg.Name})
					}
				}
			case *mir.FreezeInst:
				if reg, isReg := i.Root.(*mir.Register); isReg {
					delete(activeRefs, reg.Name) // Explicit move, kill the root!
				}
			case *mir.OwnInst:
				if reg, isReg := i.Root.(*mir.Register); isReg {
					delete(activeRefs, reg.Name) // Explicit move, kill the root!
				}
			case *mir.MoveInst:
				delete(activeRefs, i.Src)
			}
		}

		// C. End of Life / Scope Exit Releases
		// If a reference variable is active but mathematically absent from the LiveOut set,
		// it dies within this block. We safely drop it before the terminator.

		// Extract the dying references into a slice first
		var dyingRefs []string
		for v := range activeRefs {
			// A reference dies if it is not live out of this block
			if !liveness.LiveOut[block.ID][v] {
				isReturned := false
				if ret, ok := block.Terminator.(*mir.ReturnTerminator); ok && ret.Value != nil {
					if reg, isReg := ret.Value.(*mir.Register); isReg && reg.Name == v {
						isReturned = true
					}
				}

				// NEW: Check if the variable is a caller-owned parameter
				isParam := false
				for _, param := range g.Params {
					if param.Name == v {
						isParam = true
						break
					}
				}

				// Only drop if it's not returned AND not a caller-owned parameter!
				if !isReturned && !isParam {
					dyingRefs = append(dyingRefs, v)
				}
			}
		}

		// Sort them alphabetically to guarantee a deterministic instruction sequence
		sort.Strings(dyingRefs)

		// Iterate over the sorted slice to emit the actual MIR instructions
		for _, v := range dyingRefs {
			varType := varTypes[v] // Look up the type from your pre-scan map
			newStmts = append(newStmts, emitDrop(v, varType)...)
		}

		block.Statements = newStmts
	}
}

// getDef extracts the destination variable written to by an instruction.
// It explicitly omits container mutators (StructInitInst, ArrayInitInst, IndexAssignInst)
// because they modify heap memory, not the pointer tracking variable itself.
func getDef(inst mir.Instruction) string {
	switch i := inst.(type) {
	// case *mir.TempDeclInst:
	// 	return i.Name
	case *mir.AssignInst:
		return i.Dst
	case *mir.CopyInst:
		return i.Dst
	case *mir.MoveInst:
		return i.Dst
	case *mir.RefAllocInst:
		return i.Dst
	case *mir.CallInst:
		return i.Dst
	case *mir.FieldReadInst:
		return i.Dst
	case *mir.VariantReadInst:
		return i.Dst
	case *mir.VariantDiscriminantInst:
		return i.Dst
	case *mir.SliceInst:
		return i.Dst
	case *mir.IndexReadInst:
		return i.Dst
	case *mir.BinaryOpInst:
		return i.Dst
	case *mir.UnaryOpInst:
		return i.Dst
	case *mir.CastInst:
		return i.Dst
	case *mir.LoadPtrInst:
		return i.Dst
	// case *mir.StructInitInst:
	// 	return i.Dst
	case *mir.VariantInitInst:
		return i.Dst
	case *mir.MutBorrowInst:
		return i.Dst
	case *mir.OwnInst:
		return i.Dst
	case *mir.FreezeInst:
		return i.Dst
	}
	return ""
}
