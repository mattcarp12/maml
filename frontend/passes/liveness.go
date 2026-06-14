// ==========================================================================
// Liveness Analysis
// ------------------
// - Currently only computes global block-level analysis
// - TODO - Implement local statement-level analysis
// ==========================================================================

package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

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

func AnalyzeLiveness(g *mir.Graph) *LivenessResult {
	res := newLivenessResult()

	for id := range g.Blocks {
		res.LiveIn[id] = make(map[string]bool)
		res.LiveOut[id] = make(map[string]bool)
	}

	blockUses := make(map[mir.BlockID]map[string]bool)
	blockDefs := make(map[mir.BlockID]map[string]bool)
	for _, block := range g.Blocks {
		useSet, defSet := computeBlockUseDef(block)
		blockUses[block.ID] = useSet
		blockDefs[block.ID] = defSet
	}

	changed := true
	for changed {
		changed = false

		maxID := mir.BlockID(0)
		for id := range g.Blocks {
			if id > maxID {
				maxID = id
			}
		}

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

			useSet := blockUses[id]
			defSet := blockDefs[id]

			// 2. LiveIn = Use U (LiveOut - Def)
			// Track modifications accurately to verify fixed-point completion
			for v := range useSet {
				if !res.LiveIn[id][v] {
					res.LiveIn[id][v] = true
					changed = true
				}
			}
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

	// Local helper closures
	addUse := func(op mir.Value) {
		if reg, ok := op.(*mir.Register); ok && reg != nil {
			useSet[reg.Name] = true
		}
	}

	addUseName := func(name string) {
		if name != "" && name != "_" {
			useSet[name] = true
		}
	}

	addDef := func(name string) {
		if name != "" && name != "_" {
			defSet[name] = true
			delete(useSet, name) // A Def kills any upward Use path inside this block!
		}
	}

	// --- STEP 1: Process Terminator Uses FIRST (Since they sit at the very bottom) ---
	switch t := block.Terminator.(type) {
	case *mir.BranchTerminator:
		addUse(t.Condition)
	case *mir.ReturnTerminator:
		if t.Value != nil {
			addUse(t.Value)
		}
	}

	// --- STEP 2: Traverse Statements BACKWARD (Bottom to Top) ---
	for i := len(block.Statements) - 1; i >= 0; i-- {
		inst := block.Statements[i]
		switch i := inst.(type) {
		case *mir.TempDeclInst:
			addDef(i.Name)
		case *mir.RefAllocInst:
			addDef(i.Dst)

		case *mir.AssignInst:
			addDef(i.Dst)
			addUse(i.RValue)
		case *mir.CopyInst:
			addDef(i.Dst)
			addUseName(i.Src)
		case *mir.MoveInst:
			addDef(i.Dst)
			addUseName(i.Src)
		case *mir.CastInst:
			addDef(i.Dst)
			addUse(i.Src)

		case *mir.BinaryOpInst:
			addDef(i.Dst)
			addUse(i.Left)
			addUse(i.Right)
		case *mir.UnaryOpInst:
			addDef(i.Dst)
			addUse(i.Operand)

		case *mir.StructInitInst:
			addUseName(i.Dst) // Mutation uses reference
			addUse(i.Value)
		case *mir.FieldReadInst:
			addDef(i.Dst)
			addUse(i.Object)
		case *mir.VariantInitInst:
			addDef(i.Dst)
			for _, p := range i.Payloads {
				addUse(p)
			}
		case *mir.VariantReadInst:
			addDef(i.Dst)
			addUse(i.Object)
		case *mir.VariantDiscriminantInst:
			addDef(i.Dst)
			addUse(i.Object)
		case *mir.ArrayInitInst:
			addUseName(i.Dst)
			addUse(i.Value)
		case *mir.SliceInst:
			addDef(i.Dst)
			addUse(i.Left)
			addUse(i.Low)
			addUse(i.High)
		case *mir.IndexReadInst:
			addDef(i.Dst)
			addUse(i.Source)
			addUse(i.Index)
		case *mir.IndexAssignInst:
			addUseName(i.Target)
			addUse(i.Index)
			addUse(i.Value)

		case *mir.LoadPtrInst:
			addDef(i.Dst)
			addUse(i.Ptr)
		case *mir.StoreInst:
			addUseName(i.DstPtr)
			addUse(i.Value)

		case *mir.CallInst:
			if i.Dst != "" && i.Dst != "_" {
				addDef(i.Dst)
			}
			addUse(i.Function)
			for _, arg := range i.Arguments {
				addUse(arg.Argument)
			}

		case *mir.RefIncInst:
			addUseName(i.Src)
		case *mir.RefDecInst:
			addUseName(i.Src)
		case *mir.MutBorrowInst:
			addUseName(i.Src)
		case *mir.KeepAliveInst:
			addUseName(i.Src)
		}
	}

	return useSet, defSet
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