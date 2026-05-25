package escape

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
)

type EscapeState int

const (
	StateStack EscapeState = iota
	StateHeap
)

type EscapeAnalyzer struct {
	EscapeMap map[string]EscapeState
}

// AnalyzeEscape performs a fixed-point dataflow analysis over the MIR (Phase 5).
func AnalyzeEscape(g *mir.Graph) map[string]EscapeState {
	a := &EscapeAnalyzer{
		EscapeMap: make(map[string]EscapeState),
	}

	// Seed known escaping boundaries
	a.EscapeMap["_ret"] = StateHeap

	changed := true
	for changed {
		changed = false
		for _, block := range g.Blocks {
			for _, inst := range block.Statements {
				if a.analyzeInstruction(inst) {
					changed = true
				}
			}
		}
	}

	return a.EscapeMap
}

func (a *EscapeAnalyzer) analyzeInstruction(inst mir.Instruction) bool {
	changed := false

	switch i := inst.(type) {
	case *mir.AssignInst:
		if a.EscapeMap[i.Dst] == StateHeap {
			if a.propagateFromExpr(i.Expr) {
				changed = true
			}
		}
	case *mir.CopyInst:
		if a.EscapeMap[i.Dst] == StateHeap && a.EscapeMap[i.Src] != StateHeap {
			a.EscapeMap[i.Src] = StateHeap
			changed = true
		}
	case *mir.MoveInst:
		if a.EscapeMap[i.Dst] == StateHeap && a.EscapeMap[i.Src] != StateHeap {
			a.EscapeMap[i.Src] = StateHeap
			changed = true
		}
	}
	return changed
}

func (a *EscapeAnalyzer) propagateFromExpr(expr hir.Expr) bool {
	changed := false
	switch e := expr.(type) {
	case *hir.Identifier:
		if a.EscapeMap[e.Value] != StateHeap {
			a.EscapeMap[e.Value] = StateHeap
			changed = true
		}
	case *hir.CallExpr:
		// Conservatively assume all arguments passed to a function escape
		// if the result of the function escapes.
		for _, arg := range e.Arguments {
			if a.propagateFromExpr(arg.Argument) {
				changed = true
			}
		}
		// hir.ZeroAllocExpr requires no traversal because its fields are
		// populated by subsequent linear AssignInst operations in the MIR.
	}
	return changed
}
