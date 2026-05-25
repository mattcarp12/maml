package arc

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/liveness"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// InjectARC enforces the Phase 7 Ownership model by inserting RefIncInst and RefDecInst.
func InjectARC(g *mir.Graph, liveness *liveness.LivenessResult) {
	// 1. Pre-scan to map variable names to their Types
	varTypes := make(map[string]types.Type)
	for _, block := range g.Blocks {
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
		return ok && t != nil && t.IsReferenceType()
	}

	// 2. Single-Pass Block Injection
	for _, block := range g.Blocks {
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
				if ident, isIdent := assign.Expr.(*hir.Identifier); isIdent {
					newStmts = append(newStmts, &mir.RefIncInst{Src: ident.Value})
				}
			}
		}

		// C. End of Life / Scope Exit Releases
		// If a reference variable is active but mathematically absent from the LiveOut set,
		// it dies within this block. We safely drop it before the terminator.
		for v := range activeRefs {
			if !liveness.LiveOut[block.ID][v] && v != "_ret" {
				newStmts = append(newStmts, &mir.RefDecInst{Src: v})
			}
		}

		block.Statements = newStmts
	}
}

// getDef extracts the destination variable written to by an instruction.
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
	}
	return ""
}
