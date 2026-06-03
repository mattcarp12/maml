package passes

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
)

// LivenessResult holds the LiveIn and LiveOut sets for every BasicBlock.
type LivenessResult struct {
	LiveIn  map[mir.BlockID]map[string]bool
	LiveOut map[mir.BlockID]map[string]bool
}

func newLivenessResult() *LivenessResult {
	return &LivenessResult{
		LiveIn:  make(map[mir.BlockID]map[string]bool),
		LiveOut: make(map[mir.BlockID]map[string]bool),
	}
}

// AnalyzeLiveness computes the liveness of variables across the MIR CFG (Phase 5).
func AnalyzeLiveness(g *mir.Graph) *LivenessResult {
	res := newLivenessResult()

	// Initialize sets
	for id := range g.Blocks {
		res.LiveIn[id] = make(map[string]bool)
		res.LiveOut[id] = make(map[string]bool)
	}

	// Precompute block summaries once upfront
	blockUses := make(map[mir.BlockID]map[string]bool)
	blockDefs := make(map[mir.BlockID]map[string]bool)
	for _, block := range g.SortedBlocks() {
		useSet, defSet := computeBlockUseDef(block)
		blockUses[block.ID] = useSet
		blockDefs[block.ID] = defSet
	}

	changed := true
	for changed {
		changed = false

		// Find the true maximum block ID in the graph
		maxID := mir.BlockID(0)
		for id := range g.Blocks {
			if id > maxID {
				maxID = id
			}
		}

		// Iterate backward through blocks (reverse order speeds up convergence)
		for id := maxID; id >= 0; id-- {
			block, exists := g.Blocks[id]
			if !exists {
				continue
			}

			// 1. LiveOut = Union of LiveIn of all successors
			succs := getSuccessors(block)
			for _, succID := range succs {
				for v := range res.LiveIn[succID] {
					if !res.LiveOut[id][v] {
						res.LiveOut[id][v] = true
						changed = true
					}
				}
			}

			// Get Use and Def for this block
			useSet := blockUses[id]
			defSet := blockDefs[id]

			// 2. LiveIn = Use U (LiveOut - Def)
			// First, fold in the local uses
			for v := range useSet {
				if !res.LiveIn[id][v] {
					res.LiveIn[id][v] = true
					changed = true
				}
			}
			// Next, pass through the non-redefined LiveOut variables
			for v := range res.LiveOut[id] {
				if !defSet[v] {
					if !res.LiveIn[id][v] {
						res.LiveIn[id][v] = true
						changed = true
					}
				}
			}
		}
	}

	return res
}

func computeBlockUseDef(block *mir.BasicBlock) (map[string]bool, map[string]bool) {
	useSet := make(map[string]bool)
	defSet := make(map[string]bool)

	// Helper to safely add an operand to the Use set (if it's a variable and not yet defined)
	addUse := func(op hir.Operand) {
		if ident, ok := op.(*hir.Identifier); ok && ident != nil {
			if !defSet[ident.Value] {
				useSet[ident.Value] = true
			}
		}
	}

	// Helper to add explicit string variable names to the Use set
	addUseName := func(name string) {
		if !defSet[name] {
			useSet[name] = true
		}
	}

	// Traverse forward to compute block-level summary
	for _, inst := range block.Statements {
		switch i := inst.(type) {
		// --- Declarations & Allocations (Defs only) ---
		case *mir.TempDeclInst:
			defSet[i.Name] = true
		case *mir.RefAllocInst:
			defSet[i.Dst] = true

		// --- Assignments (Use RHS, then Def LHS) ---
		case *mir.AssignInst:
			addUse(i.RValue)
			defSet[i.Dst] = true
		case *mir.CopyInst:
			addUseName(i.Src)
			defSet[i.Dst] = true
		case *mir.MoveInst:
			addUseName(i.Src)
			defSet[i.Dst] = true
		case *mir.CastInst:
			addUse(i.Src)
			defSet[i.Dst] = true

		// --- Arithmetic & Logic ---
		case *mir.BinaryOpInst:
			addUse(i.Left)
			addUse(i.Right)
			defSet[i.Dst] = true
		case *mir.UnaryOpInst:
			addUse(i.Operand)
			defSet[i.Dst] = true

		// --- Containers & Structs ---
		case *mir.StructInitInst:
			addUse(i.Value)
			addUseName(i.Dst) // Mutating a struct uses the struct pointer/reference!
		case *mir.FieldReadInst:
			addUse(i.Object)
			defSet[i.Dst] = true
		case *mir.VariantInitInst:
			for _, p := range i.Payloads {
				addUse(p)
			}
			defSet[i.Dst] = true
		case *mir.VariantReadInst:
			addUse(i.Object)
			defSet[i.Dst] = true
		case *mir.VariantDiscriminantInst:
			addUse(i.Object)
			defSet[i.Dst] = true
		case *mir.ArrayInitInst:
			addUse(i.Value)
			addUseName(i.Dst) // Array mutation uses the array
		case *mir.SliceInst:
			addUse(i.Left)
			addUse(i.Low)
			addUse(i.High)
			defSet[i.Dst] = true
		case *mir.IndexReadInst:
			addUse(i.Source)
			addUse(i.Index)
			defSet[i.Dst] = true
		case *mir.IndexAssignInst:
			addUse(i.Index)
			addUse(i.Value)
			addUseName(i.Target) // Mutating an index uses the container!

		// --- Pointers ---
		case *mir.LoadPtrInst:
			addUse(i.Ptr)
			defSet[i.Dst] = true
		case *mir.StoreInst:
			addUse(i.Value)
			addUseName(i.DstPtr) // Storing into a pointer uses the pointer

		// --- Functions ---
		case *mir.CallInst:
			addUse(i.Function)
			for _, arg := range i.Arguments {
				addUse(arg.Argument)
			}
			if i.Dst != "" && i.Dst != "_" {
				defSet[i.Dst] = true
			}

		// --- Memory/Ownership Intrinsic Markers (Uses) ---
		case *mir.RefIncInst:
			addUseName(i.Src)
		case *mir.RefDecInst:
			addUseName(i.Src)
		case *mir.MutBorrowInst:
			addUseName(i.Src)
		}
	}

	// Terminators also USE variables
	switch t := block.Terminator.(type) {
	case *mir.BranchTerminator:
		addUse(t.Condition)
	case *mir.ReturnTerminator:
		if t.Value != nil {
			addUse(t.Value)
		}
	}

	return useSet, defSet
}

func getSuccessors(block *mir.BasicBlock) []mir.BlockID {
	switch t := block.Terminator.(type) {
	case *mir.JumpTerminator:
		return []mir.BlockID{t.Target}
	case *mir.BranchTerminator:
		return []mir.BlockID{t.TrueTarget, t.FalseTarget}
	case *mir.CoroSuspendTerminator:
		// When a coroutine suspends, control flows either to the resume block (when awoken)
		// or the cleanup block (if cancelled/destroyed).
		return []mir.BlockID{t.ResumeBlock, t.CleanupBlock}
	}
	return nil
}
