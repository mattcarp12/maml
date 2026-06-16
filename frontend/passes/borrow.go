package passes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/mir"
)

// BindingState manages the flow-sensitive lifecycle of a variable.
type BindingState struct {
	Invalidated bool   // True if the variable was moved (use-after-move guard)
	MutLockedBy string // The name of the reference variable currently holding the exclusive lock
}

type BlockState struct {
	Bindings map[string]*BindingState
}

func newBlockState() *BlockState {
	return &BlockState{Bindings: make(map[string]*BindingState)}
}

func (b *BlockState) clone() *BlockState {
	c := newBlockState()
	for k, v := range b.Bindings {
		c.Bindings[k] = &BindingState{
			Invalidated: v.Invalidated,
			MutLockedBy: v.MutLockedBy,
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

// Analyze performs a forward dataflow analysis over the MIR CFG, using Liveness
// to automatically release Non-Lexical Lifetime (NLL) borrows.
func (a *Analyzer) Analyze(g *mir.Graph, live *LivenessResult) []ast.CompileError {
	stateIn := make(map[mir.BlockID]*BlockState)
	stateOut := make(map[mir.BlockID]*BlockState)
	visited := make(map[mir.BlockID]bool)

	for id := range g.Blocks {
		stateIn[id] = newBlockState()
		stateOut[id] = newBlockState()
	}

	changed := true
	for changed {
		changed = false

		for _, block := range g.SortedBlocks() {
			mergedIn := a.mergePredecessors(g, block, stateOut, visited)
			stateIn[block.ID] = mergedIn

			currentState := mergedIn.clone()

			// 1. Walk instructions and apply locking/moving rules
			for _, inst := range block.Statements {
				a.analyzeInstruction(inst, currentState)
			}

			// 2. Validate Terminators (Async boundary checks)
			a.analyzeTerminator(block.Terminator, currentState)

			// 3. MAGIC: Automatic NLL Unlock at the end of the block
			// If the reference holding the lock is mathematically dead, unlock the data!
			for _, binding := range currentState.Bindings {
				if binding.MutLockedBy != "" {
					if !live.LiveOut[block.ID][binding.MutLockedBy] {
						binding.MutLockedBy = "" // The reference is dead. Free the lock!
					}
				}
			}

			visited[block.ID] = true

			if !statesEqual(stateOut[block.ID], currentState) {
				stateOut[block.ID] = currentState
				changed = true
			}
		}
	}

	return a.errors
}

func (a *Analyzer) mergePredecessors(g *mir.Graph, block *mir.BasicBlock, stateOut map[mir.BlockID]*BlockState, visited map[mir.BlockID]bool) *BlockState {
	merged := newBlockState()
	allPreds := getPredecessors(g, block.ID)

	var preds []mir.BlockID
	for _, p := range allPreds {
		if visited[p] {
			preds = append(preds, p)
		}
	}

	if len(preds) == 0 {
		return merged
	}

	// Pass 1: collect the union of all binding keys across every predecessor.
	for _, p := range preds {
		for k := range stateOut[p].Bindings {
			if _, exists := merged.Bindings[k]; !exists {
				merged.Bindings[k] = &BindingState{}
			}
		}
	}

	// Pass 2: for every key in the union, OR together the Invalidated flag
	// across all predecessors. If a predecessor doesn't define the key at all,
	// treat that as Invalidated=true (the variable wasn't live on that path).
	// MutLockedBy is taken from the first predecessor that has a non-empty lock.
	for k, mergedVal := range merged.Bindings {
		for _, p := range preds {
			predState := stateOut[p]
			if predVal, exists := predState.Bindings[k]; exists {
				mergedVal.Invalidated = mergedVal.Invalidated || predVal.Invalidated
				if mergedVal.MutLockedBy == "" && predVal.MutLockedBy != "" {
					mergedVal.MutLockedBy = predVal.MutLockedBy
				}
			} else {
				// Variable not defined on this incoming path → conservatively invalidated.
				mergedVal.Invalidated = true
			}
		}
	}

	return merged
}

func getPredecessors(g *mir.Graph, target mir.BlockID) []mir.BlockID {
	var preds []mir.BlockID
	for _, block := range g.SortedBlocks() {
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
		if !ok || v1.Invalidated != v2.Invalidated || v1.MutLockedBy != v2.MutLockedBy {
			return false
		}
	}
	return true
}

func (a *Analyzer) analyzeInstruction(inst mir.Instruction, state *BlockState) {
	pos := ast.Position{} // Fallback position

	// HELPER: If a reference variable is reassigned/overwritten, it drops any locks it previously held!
	releaseLocksHeldBy := func(refName string) {
		for _, binding := range state.Bindings {
			if binding.MutLockedBy == refName {
				binding.MutLockedBy = ""
			}
		}
	}

	// HELPER: Reassigning a variable revives it from an Invalidated (moved) state.
	initOrRevive := func(name string) {
		if binding, exists := state.Bindings[name]; exists {
			binding.Invalidated = false
		} else {
			state.Bindings[name] = &BindingState{}
		}
	}

	switch i := inst.(type) {
	case *mir.TempDeclInst:
		initOrRevive(i.Name)

	case *mir.RefAllocInst:
		initOrRevive(i.Dst)

	case *mir.AssignInst:
		a.checkOperandAccess(i.RValue, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.MutBorrowInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		if binding, exists := state.Bindings[i.Src]; exists {
			binding.MutLockedBy = i.Dst
		}
		initOrRevive(i.Dst)

	case *mir.CopyInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.IndexAssignInst:
		a.checkOperandAccess(i.Index, state, pos)
		a.checkOperandAccess(i.Value, state, pos)
		a.checkStringAccess(i.Target, state, pos)

	case *mir.StructInitInst:
		a.checkOperandAccess(i.Value, state, pos)
		a.checkStringAccess(i.Dst, state, pos)

	case *mir.MoveInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)

		if binding, exists := state.Bindings[i.Src]; exists {
			if !isCompilerGenerated(i.Src) && !isCompilerGenerated(i.Dst) {
				binding.Invalidated = true
			}
		}
		initOrRevive(i.Dst)

	case *mir.BinaryOpInst:
		a.checkOperandAccess(i.Left, state, pos)
		a.checkOperandAccess(i.Right, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.CallInst:
		a.checkOperandAccess(i.Function, state, pos)
		for _, arg := range i.Arguments {
			a.checkOperandAccess(arg.Argument, state, pos)
		}
		if i.Dst != "" && i.Dst != "_" {
			releaseLocksHeldBy(i.Dst)
			initOrRevive(i.Dst)
		}
	}
}

func (a *Analyzer) analyzeTerminator(term mir.Terminator, state *BlockState) {
	pos := ast.Position{}

	switch term.(type) {
	case *mir.CoroSuspendTerminator:
		// Rule 3: Mutable borrows cannot cross await boundaries!
		var names []string
		for name := range state.Bindings {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			binding := state.Bindings[name]
			if binding.MutLockedBy != "" {
				a.errorf(pos, "mutable reference '%s' borrowing '%s' cannot be held across an `await` point", binding.MutLockedBy, name)
			}
		}
	}
}

func (a *Analyzer) checkOperandAccess(op mir.Value, state *BlockState, pos ast.Position) {
	if reg, ok := op.(*mir.Register); ok {
		a.checkStringAccess(reg.Name, state, pos)
	}
}

func (a *Analyzer) checkStringAccess(name string, state *BlockState, pos ast.Position) {
	binding, exists := state.Bindings[name]
	if !exists || isCompilerGenerated(name) {
		return
	}

	if binding.Invalidated {
		a.errorf(pos, "use of moved variable '%s'", name)
	} else if binding.MutLockedBy != "" {
		a.errorf(pos, "cannot access variable '%s' because it is currently mutably borrowed by '%s'", name, binding.MutLockedBy)
	}
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Ownership",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func isCompilerGenerated(name string) bool {
	return strings.HasPrefix(name, "__") || strings.HasPrefix(name, "_t")
}
