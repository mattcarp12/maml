package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

func LowerAllocations(g *mir.Graph, escapes map[string]EscapeState) {
	if g == nil || escapes == nil {
		return
	}

	for _, block := range g.SortedBlocks() {
		var newStmts []mir.Instruction
		for _, inst := range block.Statements {
			// 1. Always retain the local declaration so the backend has a stack pointer
			newStmts = append(newStmts, inst)

			if decl, ok := inst.(*mir.TempDeclInst); ok && decl != nil && decl.Type != nil {
				// 2. ONLY allocate a backing buffer if a value type (like a stack struct)
				// is forced to escape to the heap. ARC types (like Vec) already manage
				// their own heap memory via their constructors!
				if !decl.Type.IsNeedsARC() && escapes[decl.Name] == StateHeap {
					newStmts = append(newStmts, &mir.RefAllocInst{
						Dst:  decl.Name,
						Type: decl.Type,
					})
				}
			}
		}
		block.Statements = newStmts
	}
}
