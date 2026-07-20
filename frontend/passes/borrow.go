package passes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/parser/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

type LockState int

const (
	ExclusiveWrite LockState = iota
	SharedRead
	MaybeInvalidated
	Invalidated
)

func (s LockState) String() string {
	switch s {
	case ExclusiveWrite:
		return "ExclusiveWrite"
	case SharedRead:
		return "SharedRead"
	case MaybeInvalidated:
		return "MaybeInvalidated"
	case Invalidated:
		return "Invalidated"
	default:
		return "Unknown"
	}
}

func joinStates(s1, s2 LockState) LockState {
	if s1 == s2 {
		return s1
	}
	if s1 == Invalidated || s2 == Invalidated {
		return MaybeInvalidated
	}
	if s1 == MaybeInvalidated || s2 == MaybeInvalidated {
		return MaybeInvalidated
	}
	if s1 == SharedRead || s2 == SharedRead {
		return SharedRead
	}
	return ExclusiveWrite
}

// =============================================================================
// Environments & Deep Tracking
// =============================================================================

type BindingState struct {
	State       LockState
	MutLockedBy string
	DependsOn   string
	AliasOf     string // NEW: Tracks Reference Type Aliasing
	Fields      map[string]*BindingState
}

func (b *BindingState) AggregateState() LockState {
	effective := b.State
	for _, field := range b.Fields {
		fieldState := field.AggregateState()
		if fieldState == Invalidated || fieldState == MaybeInvalidated {
			if effective == ExclusiveWrite || effective == SharedRead {
				effective = MaybeInvalidated
			}
		}
	}
	return effective
}

func (b *BindingState) clone() *BindingState {
	if b == nil {
		return nil
	}
	c := &BindingState{
		State:       b.State,
		MutLockedBy: b.MutLockedBy,
		DependsOn:   b.DependsOn,
		AliasOf:     b.AliasOf, // Copied!
		Fields:      make(map[string]*BindingState),
	}
	for k, v := range b.Fields {
		c.Fields[k] = v.clone()
	}
	return c
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
		c.Bindings[k] = v.clone()
	}
	return c
}

// =============================================================================
// Dataflow Analyzer
// =============================================================================

type Analyzer struct {
	errors       []ast.CompileError
	varTypes     map[string]types.Type
	reportErrors bool
}

func New() *Analyzer {
	return &Analyzer{
		errors:   []ast.CompileError{},
		varTypes: make(map[string]types.Type),
	}
}

func (a *Analyzer) isRef(name string) bool {
	t, ok := a.varTypes[name]
	if !ok || t == nil {
		return false
	}

	switch t.(type) {
	case *types.RefType, *types.WeakRefType:
		return true
	default:
		return false
	}
}

func (a *Analyzer) Analyze(g *mir.Graph, locals map[string]types.Type, live *LivenessResult) []ast.CompileError {
	// 1. Initialize type registry from Function-level Locals
	for _, p := range g.Params {
		a.varTypes[p.Name] = p.Type
	}
	for name, t := range locals {
		a.varTypes[name] = t
	}

	stateIn := make(map[mir.BlockID]*BlockState)
	stateOut := make(map[mir.BlockID]*BlockState)
	visited := make(map[mir.BlockID]bool)

	for id := range g.Blocks {
		stateIn[id] = newBlockState()
		stateOut[id] = newBlockState()
	}

	var worklist []mir.BlockID
	inWorklist := make(map[mir.BlockID]bool)

	for _, block := range g.SortedBlocks() {
		worklist = append(worklist, block.ID)
		inWorklist[block.ID] = true
	}

	// --- PASS 1: fixed-point convergence, no diagnostics -------------------
	a.reportErrors = false
	// maxIters := len(g.Blocks) * 4
	maxIters := 10000
	iters := 0

	for len(worklist) > 0 {
		if iters > maxIters {
			panic(fmt.Sprintf("compiler-internal error: fixed-point solver failed to converge after %d iterations", iters))
		}
		iters++
		id := worklist[0]
		worklist = worklist[1:]
		inWorklist[id] = false

		block := g.Blocks[id]
		mergedIn := a.mergePredecessors(g, block, stateOut, visited)
		stateIn[block.ID] = mergedIn
		currentState := mergedIn.clone()

		stmtLiveness := AnalyzeStatementLiveness(block, live.LiveOut[block.ID], live.Aliases)
		a.runBlock(block, currentState, stmtLiveness, live)

		visited[block.ID] = true

		if !statesEqual(stateOut[block.ID], currentState) {
			stateOut[block.ID] = currentState
			for _, succID := range getSuccessors(block) {
				if !inWorklist[succID] {
					worklist = append(worklist, succID)
					inWorklist[succID] = true
				}
			}
		}
	}

	// --- PASS 2: converged state, diagnostics enabled, each block once -----
	a.reportErrors = true

	for _, block := range g.SortedBlocks() {
		mergedIn := stateIn[block.ID]
		currentState := mergedIn.clone()
		stmtLiveness := AnalyzeStatementLiveness(block, live.LiveOut[block.ID], live.Aliases)
		a.runBlock(block, currentState, stmtLiveness, live)
	}

	a.validateScopeExit(g, stateOut)
	return a.errors
}

// runBlock is the per-block transfer function shared by both passes: it
// mutates `currentState` in place by walking the block's instructions and
// terminator. Diagnostics are emitted only when a.reportErrors is true
// (checked inside a.errorf), so this same code path is safe to run
// speculatively during convergence.
func (a *Analyzer) runBlock(block *mir.BasicBlock, currentState *BlockState, stmtLiveness *BlockStatementLiveness, live *LivenessResult) {
	for i, inst := range block.Statements {
		a.analyzeInstruction(inst, currentState)
		for _, binding := range currentState.Bindings {
			if binding.MutLockedBy != "" {
				if !stmtLiveness.LiveOut[i][binding.MutLockedBy] {
					binding.MutLockedBy = ""
				}
			}
		}
	}

	a.analyzeTerminator(block.Terminator, currentState, live.LiveOut[block.ID])
	for _, binding := range currentState.Bindings {
		if binding.MutLockedBy != "" {
			if !live.LiveOut[block.ID][binding.MutLockedBy] {
				binding.MutLockedBy = ""
			}
		}
	}

	var invalidateDeadProvenance func(b *BindingState)
	invalidateDeadProvenance = func(b *BindingState) {
		if b == nil {
			return
		}
		if b.DependsOn != "" {
			if !live.LiveOut[block.ID][b.DependsOn] {
				b.State = Invalidated
			}
		}
		for _, f := range b.Fields {
			invalidateDeadProvenance(f)
		}
	}
	for _, binding := range currentState.Bindings {
		invalidateDeadProvenance(binding)
	}
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

	for _, p := range preds {
		for k := range stateOut[p].Bindings {
			if _, exists := merged.Bindings[k]; !exists {
				merged.Bindings[k] = nil
			}
		}
	}

	for k := range merged.Bindings {
		var currentMerged *BindingState
		for _, p := range preds {
			predVal := stateOut[p].Bindings[k]
			if currentMerged == nil {
				if predVal != nil {
					currentMerged = predVal.clone()
				} else {
					currentMerged = &BindingState{State: Invalidated, Fields: make(map[string]*BindingState)}
				}
			} else {
				currentMerged = mergeBindings(currentMerged, predVal)
			}
		}
		merged.Bindings[k] = currentMerged
	}
	return merged
}

func mergeBindings(b1, b2 *BindingState) *BindingState {
	if b1 == nil && b2 == nil {
		return nil
	}

	if b1 == nil {
		return b2.clone()
	}
	if b2 == nil {
		return b1.clone()
	}

	res := &BindingState{
		State:  joinStates(b1.State, b2.State),
		Fields: make(map[string]*BindingState),
	}

	if b1.MutLockedBy != "" {
		res.MutLockedBy = b1.MutLockedBy
	} else if b2.MutLockedBy != "" {
		res.MutLockedBy = b2.MutLockedBy
	}

	if b1.DependsOn != "" {
		res.DependsOn = b1.DependsOn
	} else if b2.DependsOn != "" {
		res.DependsOn = b2.DependsOn
	}

	// Alias inherit
	if b1.AliasOf != "" {
		res.AliasOf = b1.AliasOf
	} else if b2.AliasOf != "" {
		res.AliasOf = b2.AliasOf
	}

	allKeys := make(map[string]bool)
	for k := range b1.Fields {
		allKeys[k] = true
	}
	for k := range b2.Fields {
		allKeys[k] = true
	}

	for k := range allKeys {
		f1 := b1.Fields[k]
		f2 := b2.Fields[k]

		if f1 == nil {
			f1 = &BindingState{State: b1.State}
		}
		if f2 == nil {
			f2 = &BindingState{State: b2.State}
		}

		res.Fields[k] = mergeBindings(f1, f2)
	}
	return res
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

func getSuccessors(block *mir.BasicBlock) []mir.BlockID {
	if block.Terminator == nil {
		return nil
	}
	switch t := block.Terminator.(type) {
	case *mir.JumpTerminator:
		return []mir.BlockID{t.Target}
	case *mir.BranchTerminator:
		return []mir.BlockID{t.TrueTarget, t.FalseTarget}
	case *mir.CoroSuspendTerminator:
		return []mir.BlockID{t.ResumeBlock, t.CleanupBlock}
	}
	return nil
}

func statesEqual(s1, s2 *BlockState) bool {
	if len(s1.Bindings) != len(s2.Bindings) {
		return false
	}
	for k, v1 := range s1.Bindings {
		v2, ok := s2.Bindings[k]
		if !ok || !bindingsEqual(v1, v2) {
			return false
		}
	}
	return true
}

func bindingsEqual(b1, b2 *BindingState) bool {
	if b1 == nil && b2 == nil {
		return true
	}
	if b1 == nil || b2 == nil {
		return false
	}
	if b1.State != b2.State || b1.MutLockedBy != b2.MutLockedBy {
		return false
	}
	if b1.DependsOn != b2.DependsOn || b1.AliasOf != b2.AliasOf {
		return false
	}
	if len(b1.Fields) != len(b2.Fields) {
		return false
	}
	for k, f1 := range b1.Fields {
		f2, ok := b2.Fields[k]
		if !ok || !bindingsEqual(f1, f2) {
			return false
		}
	}
	return true
}

// =============================================================================
// Instruction Transfer Functions
// =============================================================================

func (a *Analyzer) analyzeInstruction(inst mir.Instruction, state *BlockState) {
	pos := ast.Position{}

	releaseLocksHeldBy := func(refName string) {
		for _, binding := range state.Bindings {
			if binding.MutLockedBy == refName {
				binding.MutLockedBy = ""
			}
		}
	}

	initOrRevive := func(name string) {
		if binding, exists := state.Bindings[name]; exists {
			binding.State = ExclusiveWrite
			binding.Fields = make(map[string]*BindingState)
			binding.AliasOf = ""
			binding.DependsOn = ""
		} else {
			state.Bindings[name] = &BindingState{State: ExclusiveWrite}
		}
	}

	switch i := inst.(type) {
	case *mir.AssignInst:
		a.checkOperandAccess(i.RValue, state, pos)
		releaseLocksHeldBy(i.Dst)
		if reg, ok := i.RValue.(*mir.Register); ok {
			if rBinding, exists := state.Bindings[reg.Name]; exists {
				newBinding := rBinding.clone()
				// Shared Alias Tracking (Ignore compiler temporaries)
				if a.isRef(reg.Name) && !isCompilerGenerated(reg.Name) {
					newBinding.AliasOf = reg.Name
					newBinding.State = joinStates(newBinding.AggregateState(), SharedRead)
					rBinding.State = joinStates(rBinding.AggregateState(), SharedRead)
				} else {
					newBinding.AliasOf = ""
				}
				state.Bindings[i.Dst] = newBinding
			} else {
				initOrRevive(i.Dst)
			}
		} else {
			initOrRevive(i.Dst)
		}

	case *mir.CopyInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		if sBinding, exists := state.Bindings[i.Src]; exists {
			newBinding := sBinding.clone()
			// Shared Alias Tracking (Ignore compiler temporaries)
			if a.isRef(i.Src) && !isCompilerGenerated(i.Src) {
				newBinding.AliasOf = i.Src
				newBinding.State = joinStates(newBinding.AggregateState(), SharedRead)
				sBinding.State = joinStates(sBinding.AggregateState(), SharedRead)
			} else {
				newBinding.AliasOf = ""
			}
			state.Bindings[i.Dst] = newBinding
		} else {
			initOrRevive(i.Dst)
		}

	case *mir.MoveInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		if binding, exists := state.Bindings[i.Src]; exists {
			newBinding := binding.clone()
			// Shared Alias Tracking (Ignore compiler temporaries)
			if a.isRef(i.Src) && !isCompilerGenerated(i.Src) {
				newBinding.AliasOf = i.Src
				newBinding.State = joinStates(newBinding.AggregateState(), SharedRead)
				binding.State = joinStates(binding.AggregateState(), SharedRead)
			} else {
				newBinding.AliasOf = ""
				binding.State = Invalidated
			}
			state.Bindings[i.Dst] = newBinding
		} else {
			initOrRevive(i.Dst)
		}

	case *mir.BorrowInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)

		if binding, exists := state.Bindings[i.Src]; exists {
			if i.IsMut {
				// XOR Mutability: Reject mutable borrow if it's aliased or already borrowed
				if binding.AliasOf != "" {
					a.errorf(pos, "cannot take mutable borrow of aliased data '%s'", i.Src)
				} else if binding.AggregateState() != ExclusiveWrite {
					a.errorf(pos, "cannot borrow '%s' mutably; current state is %s", i.Src, binding.AggregateState())
				}
				binding.MutLockedBy = i.Dst
			} else {
				// Shared Read Borrow
				binding.State = joinStates(binding.AggregateState(), SharedRead)
			}
		}
		initOrRevive(i.Dst)

		// CRITICAL: Track provenance so validateScopeExit knows this is tied to the source!
		state.Bindings[i.Dst].DependsOn = i.Src

	case *mir.BitcastPtrInst:
		a.checkOperandAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

		if reg, ok := i.Src.(*mir.Register); ok {
			if objBinding, exists := state.Bindings[reg.Name]; exists {
				dependsOn := reg.Name
				// Inherit parent dependency if the source is already a borrow/view
				if objBinding.DependsOn != "" {
					dependsOn = objBinding.DependsOn
				}

				// CRITICAL: Tie the casted pointer to its memory owner
				state.Bindings[i.Dst].DependsOn = dependsOn
			}
		}

	case *mir.FieldAddrInst:
		a.checkOperandAccess(i.Object, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

		if reg, ok := i.Object.(*mir.Register); ok {
			if objBinding, exists := state.Bindings[reg.Name]; exists {
				// 1. A pointer to a field inherently depends on the base object.
				dependsOn := reg.Name

				// 2. If the base object is ALREADY a borrow/view, inherit its parent dependency instead.
				if objBinding.DependsOn != "" {
					dependsOn = objBinding.DependsOn
				}

				// 3. Inherit deep-field aliasing if we are tracking this specific field
				if fieldBinding, exists := objBinding.Fields[i.FieldName]; exists {
					if fieldBinding.DependsOn != "" {
						dependsOn = fieldBinding.DependsOn
					}
					state.Bindings[i.Dst].AliasOf = fieldBinding.AliasOf
				}

				// CRITICAL: Tie the new pointer to its memory owner!
				// This ensures `validateScopeExit` and `analyzeTerminator` will catch
				// if this pointer tries to escape the function or live across an `await`.
				state.Bindings[i.Dst].DependsOn = dependsOn
			}
		}

	case *mir.IndexAddrInst:
		a.checkOperandAccess(i.Source, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

		if reg, ok := i.Source.(*mir.Register); ok {
			if objBinding, exists := state.Bindings[reg.Name]; exists {
				dependsOn := reg.Name
				// Inherit parent dependency if the source is already a borrow/view
				if objBinding.DependsOn != "" {
					dependsOn = objBinding.DependsOn
				}

				// CRITICAL: Tie the indexed pointer to its memory owner
				state.Bindings[i.Dst].DependsOn = dependsOn
			}
		}

	case *mir.BinaryOpInst:
		a.checkOperandAccess(i.Left, state, pos)
		a.checkOperandAccess(i.Right, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.CallInst:
		a.checkOperandAccess(i.Function, state, pos)
		for _, arg := range i.Arguments {
			a.checkOperandAccess(arg, state, pos)
		}
		if i.Dst != "" && i.Dst != "_" {
			releaseLocksHeldBy(i.Dst)
			initOrRevive(i.Dst)

			// Track provenance for specific runtime accessor functions
			if reg, ok := i.Function.(*mir.Register); ok {
				if reg.Name == "maml_vec_get" || reg.Name == "maml_map_get" {
					if len(i.Arguments) > 0 {
						if argReg, ok := i.Arguments[0].(*mir.Register); ok {
							state.Bindings[i.Dst].DependsOn = argReg.Name
						}
					}
				}
			}
		}

	case *mir.UnaryOpInst:
		a.checkOperandAccess(i.Operand, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.CastInst:
		a.checkOperandAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.StoreInst:
		a.checkOperandAccess(i.DstPtr, state, pos)
		a.checkOperandAccess(i.Value, state, pos)

		// Inside analyzeInstruction, after the existing cases:
	case *mir.LoadPtrInst:
		a.checkOperandAccess(i.Ptr, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

		// If the pointer we loaded from points to a field of an owning object,
		// the loaded value is a non-owning view of that object.
		if ptrReg, ok := i.Ptr.(*mir.Register); ok {
			if ptrBinding, exists := state.Bindings[ptrReg.Name]; exists && ptrBinding.DependsOn != "" {
				if a.isRef(i.Dst) {
					state.Bindings[i.Dst].DependsOn = ptrBinding.DependsOn
					state.Bindings[i.Dst].State = SharedRead
				}
			}
		}

	}
}

func (a *Analyzer) analyzeTerminator(term mir.Terminator, state *BlockState, liveOut map[string]bool) {
	pos := ast.Position{}

	switch t := term.(type) {
	case *mir.CoroSuspendTerminator:
		var names []string
		for name := range state.Bindings {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			binding := state.Bindings[name]

			// 1. Is something mutably locking this variable across the await?
			if binding.MutLockedBy != "" && liveOut[binding.MutLockedBy] {
				a.errorf(pos, "mutable reference '%s' borrowing '%s' cannot be held across an `await` point", binding.MutLockedBy, name)
			}

			// 2. Is this variable ITSELF a borrow/view that is living across the await?
			if binding.DependsOn != "" && liveOut[name] {
				a.errorf(pos, "borrow or view '%s' cannot be held across an `await` point", name)
			}
		}
	case *mir.ReturnTerminator:
		if t.Value != nil {
			a.checkOperandAccess(t.Value, state, pos)
		}
	case *mir.BranchTerminator:
		if t.Condition != nil {
			a.checkOperandAccess(t.Condition, state, pos)
		}
	}
}

// =============================================================================
// Validation Handlers
// =============================================================================

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

	agg := binding.AggregateState()

	if agg == Invalidated {
		if binding.DependsOn != "" {
			a.errorf(pos, "use of invalidated view '%s' (its parent buffer '%s' was dropped)", name, binding.DependsOn)
		} else {
			a.errorf(pos, "use of moved variable '%s'", name)
		}
	} else if agg == MaybeInvalidated {
		a.errorf(pos, "use of conditionally moved (MaybeInvalidated) variable '%s'", name)
	} else if binding.MutLockedBy != "" {
		a.errorf(pos, "cannot access variable '%s' because it is currently mutably borrowed by '%s'", name, binding.MutLockedBy)
	}
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	if !a.reportErrors {
		return
	}
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Ownership",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func isCompilerGenerated(name string) bool {
	return strings.HasPrefix(name, "__") || strings.HasPrefix(name, "_t")
}

func (a *Analyzer) validateScopeExit(g *mir.Graph, stateOut map[mir.BlockID]*BlockState) {
	isParam := make(map[string]bool)
	for _, p := range g.Params {
		isParam[p.Name] = true
	}

	for _, block := range g.Blocks {
		if ret, ok := block.Terminator.(*mir.ReturnTerminator); ok {
			state := stateOut[block.ID]
			var retName string
			if reg, isReg := ret.Value.(*mir.Register); isReg {
				retName = reg.Name
			}

			for name, binding := range state.Bindings {
				if name == retName || isParam[name] {
					a.auditBinding(name, binding, isParam, ast.Position{})
				}
			}
		}
	}
}

func (a *Analyzer) auditBinding(name string, b *BindingState, isParam map[string]bool, pos ast.Position) {
	if b.State == MaybeInvalidated {
		a.errorf(pos, "binding '%s' is in a conditionally moved (MaybeInvalidated) state and cannot be returned.", name)
	}

	if b.DependsOn != "" && !isParam[b.DependsOn] {
		a.errorf(pos, "Lifetime Escape Error: cannot return view '%s' because it depends on local variable '%s' which will be dropped", name, b.DependsOn)
	}

	for fieldName, field := range b.Fields {
		a.auditBinding(name+"."+fieldName, field, isParam, pos)
	}
}
