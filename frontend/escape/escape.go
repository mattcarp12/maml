package escape

// import (
// 	"github.com/mattcarp12/maml/frontend/mir"
// 	"github.com/mattcarp12/maml/frontend/tast"
// )

// type EscapeState int

// const (
// 	StateStack EscapeState = iota
// 	StateHeap
// )

// type EscapeAnalyzer struct {
// 	EscapeMap map[string]EscapeState
// }

// // AnalyzeEscape performs a fixed-point dataflow analysis over the MIR.
// func AnalyzeEscape(g *mir.Graph) map[string]EscapeState {
// 	a := &EscapeAnalyzer{
// 		EscapeMap: make(map[string]EscapeState),
// 	}

// 	// Seed known escaping boundaries
// 	a.EscapeMap["_ret"] = StateHeap

// 	changed := true
// 	for changed {
// 		changed = false
// 		for _, block := range g.SortedBlocks() {
// 			// Iterate backwards through basic block statements for fast convergence.
// 			for i := len(block.Statements) - 1; i >= 0; i-- {
// 				inst := block.Statements[i]
// 				if a.analyzeInstruction(inst) {
// 					changed = true
// 				}
// 			}
// 		}
// 	}

// 	return a.EscapeMap
// }

// func (a *EscapeAnalyzer) analyzeInstruction(inst mir.Instruction) bool {
// 	changed := false

// 	switch i := inst.(type) {
// 	case *mir.AssignInst:
// 		// Function calls must be evaluated unconditionally. Even if the assignment
// 		// destination variable stays on the stack (or is discarded), any variables
// 		// passed as arguments to the function must conservatively escape to the heap.
// 		if call, ok := i.RValue.(*tast.CallExpr); ok {
// 			for _, arg := range call.Arguments {
// 				if ident, ok := arg.Argument.(*tast.Identifier); ok {
// 					if a.EscapeMap[ident.Value] != StateHeap {
// 						a.EscapeMap[ident.Value] = StateHeap
// 						changed = true
// 					}
// 				}
// 			}
// 		}

// 		// Standard propagation path: if the destination variable escapes,
// 		// track its assignment expression dependencies backwards.
// 		if a.EscapeMap[i.Dst] == StateHeap {
// 			if a.propagateFromExpr(i.RValue) {
// 				changed = true
// 			}
// 		}

// 	case *mir.CopyInst:
// 		if a.EscapeMap[i.Dst] == StateHeap && a.EscapeMap[i.Src] != StateHeap {
// 			a.EscapeMap[i.Src] = StateHeap
// 			changed = true
// 		}

// 	case *mir.MoveInst:
// 		if a.EscapeMap[i.Dst] == StateHeap && a.EscapeMap[i.Src] != StateHeap {
// 			a.EscapeMap[i.Src] = StateHeap
// 			changed = true
// 		}

// 		// NOTE: *mir.MutBorrowInst is intentionally omitted here.
// 		// In this MIR architecture, MutBorrowInst is a value-less statement representing
// 		// an exclusivity lock marker rather than an assignment. The dataflow propagation
// 		// itself is fully handled by the subsequent *mir.AssignInst.
// 	}

// 	return changed
// }

// func (a *EscapeAnalyzer) propagateFromExpr(expr tast.Expr) bool {
// 	changed := false
// 	switch e := expr.(type) {
// 	case *tast.Identifier:
// 		if a.EscapeMap[e.Value] != StateHeap {
// 			a.EscapeMap[e.Value] = StateHeap
// 			changed = true
// 		}
// 	case *tast.CallExpr:
// 		// Safe redundant fallback: if a function call expression is assigned
// 		// directly to an escaping destination, guarantee all arguments escape.
// 		for _, arg := range e.Arguments {
// 			if ident, ok := arg.Argument.(*tast.Identifier); ok {
// 				if a.EscapeMap[ident.Value] != StateHeap {
// 					a.EscapeMap[ident.Value] = StateHeap
// 					changed = true
// 				}
// 			}
// 		}
// 	}
// 	return changed
// }
