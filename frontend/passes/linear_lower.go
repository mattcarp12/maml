package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

// LowerLinearTypes scrubs borrow-checker-specific instructions and lowers
// them into standard memory operations so the C++/LLVM backend doesn't
// have to reason about NLL lifetimes or capabilities.
func LowerLinearTypes(g *mir.Graph) {
	for _, block := range g.Blocks {
		var newStmts []mir.Instruction
		for _, inst := range block.Statements {
			switch i := inst.(type) {

			case *mir.BorrowInst:
				// Lower the borrow (mut or ro) into a standard bitwise copy.
				// For heap types, this just copies the fat pointer.
				// The borrow checker has already guaranteed this alias is safe.
				newStmts = append(newStmts, &mir.CopyInst{
					Dst: i.Dst,
					Src: i.Src,
				})

			case *mir.KeepAliveInst:
				// SCRUB: No longer needed after Liveness/Borrow checking is complete.
				continue

			default:
				newStmts = append(newStmts, inst)
			}
		}
		block.Statements = newStmts
	}
}
