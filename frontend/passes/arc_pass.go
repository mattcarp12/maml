package passes

import (
	"sort"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// InjectARC enforces the Phase 7 Ownership model by inserting RefIncInst and RefDecInst.
func InjectARC(g *mir.Graph, liveness *LivenessResult) {
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
		return ok && t != nil && t.IsNeedsARC() // Fixed: Only inject ARC for owning types
	}

	// 2. Single-Pass Block Injection
	for _, block := range g.SortedBlocks() {
		var newStmts []mir.Instruction
		activeRefs := make(map[string]bool)

		// Seed active references from the dataflow LiveIn set
		for v := range liveness.LiveIn[block.ID] {
			if isRef(v) {
				activeRefs[v] = true
			}
		}

		for _, inst := range block.Statements {
			// A. Reassignment Release
			// If we are overwriting an existing reference, we must release the old data first.
			dst := getDef(inst)
			if dst != "" && isRef(dst) && activeRefs[dst] {
				newStmts = append(newStmts, &mir.RefDecInst{Src: dst})
			}

			// Keep the original instruction
			newStmts = append(newStmts, inst)

			if dst != "" && isRef(dst) {
				activeRefs[dst] = true
			}

			// B. Retain Injection (Shared Aliases)
			// Assigning a reference type via a plain identifier creates an immutable shared alias.
			if assign, ok := inst.(*mir.AssignInst); ok && isRef(assign.Dst) {
				if reg, isReg := assign.RValue.(*mir.Register); isReg {
					newStmts = append(newStmts, &mir.RefIncInst{Src: reg.Name})
				}
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
			newStmts = append(newStmts, &mir.RefDecInst{Src: v})
		}

		block.Statements = newStmts
	}
}

// getDef extracts the destination variable written to by an instruction.
// It explicitly omits container mutators (StructInitInst, ArrayInitInst, IndexAssignInst)
// because they modify heap memory, not the pointer tracking variable itself.
func getDef(inst mir.Instruction) string {
	switch i := inst.(type) {
	case *mir.TempDeclInst:
		return i.Name
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
	}
	return ""
}