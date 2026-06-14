package passes

import (
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

// AnalyzeEscape performs a fixed-point dataflow analysis over the flattened MIR CFG.
func AnalyzeEscape(g *mir.Graph) map[string]EscapeState {
	a := &EscapeAnalyzer{
		EscapeMap: make(map[string]EscapeState),
	}

	changed := true
	for changed {
		changed = false
		for _, block := range g.SortedBlocks() {
			// 1. Process the basic block terminator (e.g., Returns)
			if a.analyzeTerminator(block.Terminator) {
				changed = true
			}

			// 2. Iterate backwards through linear instructions for fast convergence
			for i := len(block.Statements) - 1; i >= 0; i-- {
				inst := block.Statements[i]
				if a.analyzeInstruction(inst) {
					changed = true
				}
			}
		}
	}

	return a.EscapeMap
}

func (a *EscapeAnalyzer) analyzeTerminator(term mir.Terminator) bool {
	changed := false

	// Only variables returned from the function that are reference types
	// (or contain references) need to escape to the heap. Primitive copies do not.
	if ret, ok := term.(*mir.ReturnTerminator); ok && ret.Value != nil {
		if reg, isReg := ret.Value.(*mir.Register); isReg {
			if reg.Type != nil && reg.Type.IsReferenceType() {
				if a.EscapeMap[reg.Name] != StateHeap {
					a.EscapeMap[reg.Name] = StateHeap
					changed = true
				}
			}
		}
	}

	return changed
}

func (a *EscapeAnalyzer) analyzeInstruction(inst mir.Instruction) bool {
	changed := false

	switch i := inst.(type) {
	// -------------------------------------------------------------------------
	// Direct Assignments & Memory Transfers
	// -------------------------------------------------------------------------
	case *mir.AssignInst:
		if a.EscapeMap[i.Dst] == StateHeap {
			if a.propagateFromOperand(i.RValue) {
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

	case *mir.CastInst:
		if a.EscapeMap[i.Dst] == StateHeap {
			if a.propagateFromOperand(i.Src) {
				changed = true
			}
		}

	case *mir.StoreInst:
		if a.EscapeMap[i.DstPtr] == StateHeap {
			if a.propagateFromOperand(i.Value) {
				changed = true
			}
		}

	// -------------------------------------------------------------------------
	// Container Initialization & Mutation
	// -------------------------------------------------------------------------
	case *mir.StructInitInst:
		// If the struct escapes, anything placed inside it must also escape.
		if a.EscapeMap[i.Dst] == StateHeap {
			if a.propagateFromOperand(i.Value) {
				changed = true
			}
		}

	case *mir.ArrayInitInst:
		if a.EscapeMap[i.Dst] == StateHeap {
			if a.propagateFromOperand(i.Value) {
				changed = true
			}
		}

	case *mir.VariantInitInst:
		if a.EscapeMap[i.Dst] == StateHeap {
			for _, p := range i.Payloads {
				if a.propagateFromOperand(p) {
					changed = true
				}
			}
		}

	case *mir.IndexAssignInst:
		if a.EscapeMap[i.Target] == StateHeap {
			if a.propagateFromOperand(i.Value) {
				changed = true
			}
		}

	// -------------------------------------------------------------------------
	// Function Boundaries
	// -------------------------------------------------------------------------
	case *mir.CallInst:
		// Conservative boundary: Any reference type passed to an external
		// function is assumed to escape (unless intra-procedural analysis proves otherwise).
		for _, arg := range i.Arguments {
			if reg, isReg := arg.Argument.(*mir.Register); isReg {
				if reg.Type != nil && reg.Type.IsReferenceType() {
					if a.EscapeMap[reg.Name] != StateHeap {
						a.EscapeMap[reg.Name] = StateHeap
						changed = true
					}
				}
			}
		}
	}

	return changed
}

// propagateFromOperand cleanly handles flat MIR values without needing recursion.
func (a *EscapeAnalyzer) propagateFromOperand(op mir.Value) bool {
	if reg, ok := op.(*mir.Register); ok {
		if a.EscapeMap[reg.Name] != StateHeap {
			a.EscapeMap[reg.Name] = StateHeap
			return true
		}
	}
	return false
}
