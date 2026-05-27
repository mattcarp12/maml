package alloc

import (
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/mir"
)

// LowerAllocations upgrades TempDeclInst to RefAllocInst based on the EscapeMap (Phase 6).
func LowerAllocations(g *mir.Graph, escapes map[string]escape.EscapeState) {
	if g == nil || escapes == nil {
		return
	}
	for _, block := range g.Blocks {
		for i, inst := range block.Statements {
			if decl, ok := inst.(*mir.TempDeclInst); ok && decl != nil {
				// GUARD: If type resolution failed or is missing, skip to avoid nil deref
				if decl.Type == nil {
					continue
				}
				if decl.Type.IsReferenceType() && escapes[decl.Name] == escape.StateHeap {
					// Upgrade to Heap Allocation
					block.Statements[i] = &mir.RefAllocInst{
						Dst:  decl.Name,
						Type: decl.Type,
					}
				}
			}
		}
	}
}
