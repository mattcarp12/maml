package passes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/mir"
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

// joinStates implements the explicit two-way merge table from Section 4.4.
func joinStates(s1, s2 LockState) LockState {
	if s1 == s2 {
		return s1
	}

	// Invalidated + any non-Invalidated state = MaybeInvalidated
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

// BindingState manages the flow-sensitive lifecycle of a variable and its structural fields.
type BindingState struct {
	State       LockState
	MutLockedBy string
	DependsOn   string
	Fields      map[string]*BindingState // Recursive tracking for struct fields
}

// AggregateState computes the effective state of a struct as a whole (Section 9.3).
func (b *BindingState) AggregateState() LockState {
	effective := b.State
	for _, field := range b.Fields {
		fieldState := field.AggregateState()
		// If a field degrades to Invalidated or MaybeInvalidated, the struct
		// cannot be moved or frozen as a whole.
		if fieldState == Invalidated || fieldState == MaybeInvalidated {
			if effective == ExclusiveWrite || effective == SharedRead {
				effective = MaybeInvalidated
			}
		}
	}
	return effective
}

// setPathState applies an ownership phase change to a specific nested field.
func (b *BindingState) setPathState(path []string, state LockState) {
	if len(path) == 0 {
		b.State = state
		return
	}

	if b.Fields == nil {
		b.Fields = make(map[string]*BindingState)
	}

	head := path[0]
	child, exists := b.Fields[head]
	if !exists {
		child = &BindingState{State: b.State, Fields: make(map[string]*BindingState)}
		b.Fields[head] = child
	}

	child.setPathState(path[1:], state)
}

func (b *BindingState) setPathDependsOn(path []string, parent string) {
	if len(path) == 0 {
		b.DependsOn = parent
		return
	}
	if b.Fields == nil {
		b.Fields = make(map[string]*BindingState)
	}
	head := path[0]
	child, exists := b.Fields[head]
	if !exists {
		child = &BindingState{State: b.State, Fields: make(map[string]*BindingState)}
		b.Fields[head] = child
	}
	child.setPathDependsOn(path[1:], parent)
}

func (b *BindingState) clone() *BindingState {
	if b == nil {
		return nil
	}
	c := &BindingState{
		State:       b.State,
		MutLockedBy: b.MutLockedBy,
		DependsOn:   b.DependsOn,
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
	errors []ast.CompileError
}

func New() *Analyzer {
	return &Analyzer{
		errors: []ast.CompileError{},
	}
}

// Analyze performs a forward dataflow analysis over the MIR CFG.
func (a *Analyzer) Analyze(g *mir.Graph, live *LivenessResult) []ast.CompileError {
	stateIn := make(map[mir.BlockID]*BlockState)
	stateOut := make(map[mir.BlockID]*BlockState)
	visited := make(map[mir.BlockID]bool)

	for id := range g.Blocks {
		stateIn[id] = newBlockState()
		stateOut[id] = newBlockState()
	}

	// TODO - refactor to dense bitset and rpo priority queue
	var worklist []mir.BlockID
	inWorklist := make(map[mir.BlockID]bool)

	for _, block := range g.SortedBlocks() {
		worklist = append(worklist, block.ID)
		inWorklist[block.ID] = true
	}

	for len(worklist) > 0 {
		id := worklist[0]
		worklist = worklist[1:]
		inWorklist[id] = false

		block := g.Blocks[id]
		mergedIn := a.mergePredecessors(g, block, stateOut, visited)
		stateIn[block.ID] = mergedIn

		currentState := mergedIn.clone()

		for _, inst := range block.Statements {
			a.analyzeInstruction(inst, currentState)
		}

		a.analyzeTerminator(block.Terminator, currentState)

		// MAGIC: Automatic NLL Unlock at the end of the block
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
			// If this view depends on a parent, and the parent is dead at the end of this block...
			if b.DependsOn != "" {
				if !live.LiveOut[block.ID][b.DependsOn] {
					b.State = Invalidated // The parent died! This view is now a dangling pointer.
				}
			}
			for _, f := range b.Fields {
				invalidateDeadProvenance(f)
			}
		}
		for _, binding := range currentState.Bindings {
			invalidateDeadProvenance(binding)
		}

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

	a.validateScopeExit(g, stateOut)

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

	// Unify root keys across all predecessors
	for _, p := range preds {
		for k := range stateOut[p].Bindings {
			if _, exists := merged.Bindings[k]; !exists {
				merged.Bindings[k] = nil // Placeholder for recursive merge
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

	s1 := Invalidated
	if b1 != nil {
		s1 = b1.State
	}

	s2 := Invalidated
	if b2 != nil {
		s2 = b2.State
	}

	res := &BindingState{
		State:  joinStates(s1, s2),
		Fields: make(map[string]*BindingState),
	}

	if b1 != nil && b1.MutLockedBy != "" {
		res.MutLockedBy = b1.MutLockedBy
	} else if b2 != nil && b2.MutLockedBy != "" {
		res.MutLockedBy = b2.MutLockedBy
	}

	// Inherit provenance on branch merges
	if b1 != nil && b1.DependsOn != "" {
		res.DependsOn = b1.DependsOn
	} else if b2 != nil && b2.DependsOn != "" {
		res.DependsOn = b2.DependsOn
	}

	allKeys := make(map[string]bool)
	if b1 != nil {
		for k := range b1.Fields {
			allKeys[k] = true
		}
	}
	if b2 != nil {
		for k := range b2.Fields {
			allKeys[k] = true
		}
	}

	for k := range allKeys {
		var f1, f2 *BindingState
		if b1 != nil {
			f1 = b1.Fields[k]
		}
		if b2 != nil {
			f2 = b2.Fields[k]
		}

		// If a field isn't explicitly tracked on one path, it inherits its parent's base state.
		if f1 == nil && b1 != nil {
			f1 = &BindingState{State: b1.State}
		}
		if f2 == nil && b2 != nil {
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
	pos := ast.Position{} // Fallback

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
			binding.Fields = make(map[string]*BindingState) // Wipes out partial sub-states
		} else {
			state.Bindings[name] = &BindingState{State: ExclusiveWrite}
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

	case *mir.OwnInst:
		a.checkOperandAccess(i.Root, state, pos)
		if reg, ok := i.Root.(*mir.Register); ok && !isCompilerGenerated(reg.Name) {
			if binding, exists := state.Bindings[reg.Name]; exists {
				// Apply Invalidated precisely to the path designated by the syntax tree
				binding.setPathState(i.Path, Invalidated)
			}
		}
		initOrRevive(i.Dst)

	case *mir.FreezeInst:
		a.checkOperandAccess(i.Root, state, pos)
		if reg, ok := i.Root.(*mir.Register); ok && !isCompilerGenerated(reg.Name) {
			if binding, exists := state.Bindings[reg.Name]; exists {
				agg := binding.AggregateState()
				if agg != ExclusiveWrite {
					a.errorf(pos, "freeze requires an ExclusiveWrite binding, found %s", agg.String())
				}
				binding.setPathState(i.Path, Invalidated)
			}
		}
		if binding, exists := state.Bindings[i.Dst]; exists {
			binding.State = SharedRead
		} else {
			state.Bindings[i.Dst] = &BindingState{State: SharedRead}
		}

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

	case *mir.MoveInst:
		a.checkStringAccess(i.Src, state, pos)
		releaseLocksHeldBy(i.Dst)
		if binding, exists := state.Bindings[i.Src]; exists && !isCompilerGenerated(i.Src) {
			binding.State = Invalidated
		}
		initOrRevive(i.Dst)

	case *mir.IndexAssignInst:
		a.checkOperandAccess(i.Index, state, pos)
		a.checkOperandAccess(i.Value, state, pos)
		a.checkStringAccess(i.Target, state, pos)

	case *mir.StructInitInst:
		a.checkOperandAccess(i.Value, state, pos)
		a.checkStringAccess(i.Dst, state, pos)
		// Flow provenance into the struct field!
		if reg, ok := i.Value.(*mir.Register); ok {
			if vBinding, exists := state.Bindings[reg.Name]; exists && vBinding.DependsOn != "" {
				if dstBinding, exists := state.Bindings[i.Dst]; exists {
					// Attach the parent buffer's name to this specific struct field
					dstBinding.setPathDependsOn([]string{i.FieldName}, vBinding.DependsOn)
				}
			}
		}

	case *mir.ArrayInitInst:
		a.checkOperandAccess(i.Value, state, pos)
		a.checkStringAccess(i.Dst, state, pos)

	case *mir.BinaryOpInst:
		a.checkOperandAccess(i.Left, state, pos)
		a.checkOperandAccess(i.Right, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)

	case *mir.CallInst:
		a.checkOperandAccess(i.Function, state, pos)
		for _, arg := range i.Arguments {
			a.checkOperandAccess(arg.Argument, state, pos)

			// 2. mut arguments require ExclusiveWrite
			if arg.Mut {
				if reg, ok := arg.Argument.(*mir.Register); ok && !isCompilerGenerated(reg.Name) {
					if binding, exists := state.Bindings[reg.Name]; exists {
						agg := binding.AggregateState()
						if agg != ExclusiveWrite {
							a.errorf(pos, "cannot pass '%s' as a mutable argument because its current state is %s", reg.Name, agg.String())
						}
					}
				}
			}
		}
		if i.Dst != "" && i.Dst != "_" {
			releaseLocksHeldBy(i.Dst)
			initOrRevive(i.Dst)
		}

	case *mir.SliceInst:
		a.checkOperandAccess(i.Left, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)
		// Link the View to its parent buffer!
		if reg, ok := i.Left.(*mir.Register); ok {
			state.Bindings[i.Dst].DependsOn = reg.Name
		}

	case *mir.IndexReadInst:
		a.checkOperandAccess(i.Source, state, pos)
		a.checkOperandAccess(i.Index, state, pos)
		releaseLocksHeldBy(i.Dst)
		initOrRevive(i.Dst)
	}
}

func (a *Analyzer) analyzeTerminator(term mir.Terminator, state *BlockState) {
	pos := ast.Position{}

	switch term.(type) {
	case *mir.CoroSuspendTerminator:
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
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Ownership",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

func isCompilerGenerated(name string) bool {
	return strings.HasPrefix(name, "__") || strings.HasPrefix(name, "_t")
}

// validateScopeExit enforces Axiom 12: No partially moved states can escape a function scope.
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

// Pass isParam down so we can verify the provenance target
func (a *Analyzer) auditBinding(name string, b *BindingState, isParam map[string]bool, pos ast.Position) {
	if b.State == MaybeInvalidated {
		a.errorf(pos, "binding '%s' is in a conditionally moved (MaybeInvalidated) state and cannot be returned.", name)
	}

	// Lifetime Escape Error
	if b.DependsOn != "" && !isParam[b.DependsOn] {
		a.errorf(pos, "Lifetime Escape Error: cannot return view '%s' because it depends on local variable '%s' which will be dropped", name, b.DependsOn)
	}

	for fieldName, field := range b.Fields {
		a.auditBinding(name+"."+fieldName, field, isParam, pos)
	}
}
