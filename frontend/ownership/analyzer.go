package ownership

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
)

// BindingState manages the flow-sensitive lifecycle of a variable.
type BindingState struct {
	Invalidated bool // True if the variable was moved (use-after-move guard)
	MutLocked   bool // True if the variable is currently exclusively borrowed
}

// BlockState represents the ownership environment at a specific CFG point.
type BlockState struct {
	Bindings map[string]*BindingState
}

func newBlockState() *BlockState {
	return &BlockState{Bindings: make(map[string]*BindingState)}
}

// clone creates a deep copy of the block state for dataflow merging.
func (b *BlockState) clone() *BlockState {
	c := newBlockState()
	for k, v := range b.Bindings {
		c.Bindings[k] = &BindingState{
			Invalidated: v.Invalidated,
			MutLocked:   v.MutLocked,
		}
	}
	return c
}

type Analyzer struct {
	errors []ast.CompileError
}

func New() *Analyzer {
	return &Analyzer{
		errors: []ast.CompileError{},
	}
}

// Analyze performs a forward dataflow analysis over the MIR CFG (Phase 5).
func (a *Analyzer) Analyze(g *mir.Graph) []ast.CompileError {
	stateIn := make(map[mir.BlockID]*BlockState)
	stateOut := make(map[mir.BlockID]*BlockState)

	// Initialize states
	for id := range g.Blocks {
		stateIn[id] = newBlockState()
		stateOut[id] = newBlockState()
	}

	changed := true
	for changed {
		changed = false

		for _, block := range g.Blocks {
			// 1. Merge incoming states from predecessors
			mergedIn := a.mergePredecessors(g, block, stateOut)
			stateIn[block.ID] = mergedIn

			// 2. Apply transfer functions over instructions
			currentState := mergedIn.clone()
			for _, inst := range block.Statements {
				a.analyzeInstruction(inst, currentState)
			}
			a.analyzeTerminator(block.Terminator, currentState)

			// 3. Check for fixed point
			if !statesEqual(stateOut[block.ID], currentState) {
				stateOut[block.ID] = currentState
				changed = true
			}
		}
	}

	return a.errors
}

// mergePredecessors computes the conservative intersection of capabilities.
// If a variable is invalidated in ANY predecessor path, it is invalidated here.
func (a *Analyzer) mergePredecessors(g *mir.Graph, block *mir.BasicBlock, stateOut map[mir.BlockID]*BlockState) *BlockState {
	merged := newBlockState()
	preds := getPredecessors(g, block.ID)

	if len(preds) == 0 {
		return merged
	}

	// Initialize merged state with the first predecessor
	firstPredState := stateOut[preds[0]]
	for k, v := range firstPredState.Bindings {
		merged.Bindings[k] = &BindingState{
			Invalidated: v.Invalidated,
			MutLocked:   v.MutLocked,
		}
	}

	// Intersect with remaining predecessors
	for i := 1; i < len(preds); i++ {
		predState := stateOut[preds[i]]
		for k, mergedVal := range merged.Bindings {
			if predVal, exists := predState.Bindings[k]; exists {
				// Conservative intersection: if it's moved in any path, it's moved.
				mergedVal.Invalidated = mergedVal.Invalidated || predVal.Invalidated
				mergedVal.MutLocked = mergedVal.MutLocked || predVal.MutLocked
			} else {
				// If the variable doesn't exist in a predecessor, it's unsafe to use.
				mergedVal.Invalidated = true
			}
		}
	}

	return merged
}

func getPredecessors(g *mir.Graph, target mir.BlockID) []mir.BlockID {
	var preds []mir.BlockID
	for _, block := range g.Blocks {
		switch t := block.Terminator.(type) {
		case *mir.JumpTerminator:
			if t.Target == target {
				preds = append(preds, block.ID)
			}
		case *mir.BranchTerminator:
			if t.TrueTarget == target || t.FalseTarget == target {
				preds = append(preds, block.ID)
			}
		}
	}
	return preds
}

func statesEqual(s1, s2 *BlockState) bool {
	if len(s1.Bindings) != len(s2.Bindings) {
		return false
	}
	for k, v1 := range s1.Bindings {
		v2, ok := s2.Bindings[k]
		if !ok || v1.Invalidated != v2.Invalidated || v1.MutLocked != v2.MutLocked {
			return false
		}
	}
	return true
}

func (a *Analyzer) analyzeInstruction(inst mir.Instruction, state *BlockState) {
	// A zero-position fallback; in a complete compiler, MIR instructions
	// should retain the ast.Position from their source HIR node.
	pos := hir.Position{}

	switch i := inst.(type) {
	case *mir.TempDeclInst:
		state.Bindings[i.Name] = &BindingState{Invalidated: false, MutLocked: false}

	case *mir.RefAllocInst:
		state.Bindings[i.Dst] = &BindingState{Invalidated: false, MutLocked: false}

	case *mir.AssignInst:
		a.checkExprAccess(i.Expr, state)
		// Reassignment revives the variable with a clean state
		state.Bindings[i.Dst] = &BindingState{Invalidated: false, MutLocked: false}

	case *mir.CopyInst:
		a.checkAccess(i.Src, state, pos)
		state.Bindings[i.Dst] = &BindingState{Invalidated: false, MutLocked: false}

	case *mir.MoveInst:
		a.checkAccess(i.Src, state, pos)
		if binding, exists := state.Bindings[i.Src]; exists {
			binding.Invalidated = true // Affine transfer of ownership
		}
		state.Bindings[i.Dst] = &BindingState{Invalidated: false, MutLocked: false}

	case *mir.MutBorrowInst:
		a.checkAccess(i.Src, state, pos)
		if binding, exists := state.Bindings[i.Src]; exists {
			binding.MutLocked = true // Exclusivity lock established
		}

	case *mir.RefIncInst:
		a.checkAccess(i.Src, state, pos)

	case *mir.RefDecInst:
		a.checkAccess(i.Src, state, pos)
	}
}

func (a *Analyzer) analyzeTerminator(term mir.Terminator, state *BlockState) {
	// A zero-position fallback; in a complete compiler, MIR terminators
	// should retain the ast.Position from their source HIR node for exact error tracing.
	pos := hir.Position{}

	switch term.(type) {
	case *mir.CoroSuspendTerminator:
		// Phase 5 Invariant: Mutable borrows cannot cross await boundaries.
		// If the task is suspended and rescheduled onto a different thread while
		// holding an exclusive lock, it creates a severe data race.
		for name, binding := range state.Bindings {
			if binding.MutLocked {
				a.errorf(pos, "mutable borrow of variable '%s' cannot survive across an async suspension point", name)
			}
		}

	case *mir.ReturnTerminator, *mir.JumpTerminator, *mir.BranchTerminator, *mir.UnreachableTerminator:
		// Standard control-flow terminators do not violate exclusivity rules.
		// Liveness analysis and ARC injection (Phase 7) independently handle
		// safely dropping these variables when they go out of scope.
		return
	}
}

func (a *Analyzer) errorf(pos hir.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Ownership",
		Pos:   pos.ToAST(), // TODO: In a full MIR implementation, position info should be threaded through MIR nodes
		Msg:   fmt.Sprintf(format, args...),
	})
}

// checkAccess verifies that a variable is legally readable/writable.
func (a *Analyzer) checkAccess(name string, state *BlockState, pos hir.Position) {
	binding, exists := state.Bindings[name]
	if !exists {
		return // Ignore unregistered symbols (e.g., globals or built-ins)
	}

	if binding.Invalidated {
		a.errorf(pos, "use of moved variable '%s'", name)
	} else if binding.MutLocked {
		a.errorf(pos, "cannot access variable '%s' because it is mutably borrowed", name)
	}
}

// checkExprAccess recursively validates access to variables inside a flattened expression.
func (a *Analyzer) checkExprAccess(expr hir.Expr, state *BlockState) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *hir.Identifier:
		a.checkAccess(e.Value, state, e.Pos())
	case *hir.InfixExpr:
		a.checkExprAccess(e.Left, state)
		a.checkExprAccess(e.Right, state)
	case *hir.PrefixExpr:
		a.checkExprAccess(e.Right, state)
	case *hir.CallExpr:
		a.checkExprAccess(e.Function, state)
		for _, arg := range e.Arguments {
			a.checkExprAccess(arg.Argument, state)
		}
	case *hir.IndexExpr:
		a.checkExprAccess(e.Left, state)
		a.checkExprAccess(e.Index, state)
	case *hir.SliceExpr:
		a.checkExprAccess(e.Left, state)
		if e.Low != nil {
			a.checkExprAccess(e.Low, state)
		}
		if e.High != nil {
			a.checkExprAccess(e.High, state)
		}
	case *hir.FieldAccess:
		a.checkExprAccess(e.Object, state)
	}
}
