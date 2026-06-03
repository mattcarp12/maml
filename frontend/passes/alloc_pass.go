package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

// LowerAllocations upgrades TempDeclInst to RefAllocInst based on the EscapeMap.
func LowerAllocations(g *mir.Graph, escapes map[string]EscapeState) {
	if g == nil || escapes == nil {
		return
	}

	for _, block := range g.SortedBlocks() {
		for i, inst := range block.Statements {
			if decl, ok := inst.(*mir.TempDeclInst); ok && decl != nil {
				// GUARD: If type resolution failed or is missing, skip to avoid nil deref
				if decl.Type == nil {
					continue
				}

				// Upgrade to Heap Allocation if the analyzer flagged it
				if decl.Type.IsReferenceType() && escapes[decl.Name] == StateHeap {
					block.Statements[i] = &mir.RefAllocInst{
						Dst:  decl.Name,
						Type: decl.Type,
					}
				}
			}
		}
	}
}
