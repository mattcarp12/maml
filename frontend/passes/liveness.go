package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

type LivenessResult struct {
	LiveIn  map[mir.BlockID]map[string]bool
	LiveOut map[mir.BlockID]map[string]bool
}

type BlockStatementLiveness struct {
	LiveIn      []map[string]bool
	LiveOut     []map[string]bool
	TermLiveIn  map[string]bool
	TermLiveOut map[string]bool
}

// Helper to clone a set so we don't accidentally mutate shared maps
func cloneSet(s map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(s))
	for k, v := range s {
		clone[k] = v
	}
	return clone
}

func AnalyzeLiveness(g *mir.Graph) *LivenessResult {
	res := &LivenessResult{
		LiveIn:  make(map[mir.BlockID]map[string]bool),
		LiveOut: make(map[mir.BlockID]map[string]bool),
	}

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
				addUse(arg)
			}
		case *mir.BorrowInst:
			addUseName(i.Src)
			addDef(i.Dst)
		case *mir.KeepAliveInst:
			addUseName(i.Src)
		case *mir.FieldAddrInst:
			addDef(i.Dst)
			addUse(i.Object)
		case *mir.DropInst:
			addUseName(i.Src)
		}
	}

	return useSet, defSet
}

// AnalyzeStatementLiveness computes the exact liveness across every instruction
// in a single block. This is required for Non-Lexical Lifetime (NLL) borrow checking.
func AnalyzeStatementLiveness(block *mir.BasicBlock, blockLiveOut map[string]bool) *BlockStatementLiveness {
	res := &BlockStatementLiveness{
		LiveIn:      make([]map[string]bool, len(block.Statements)),
		LiveOut:     make([]map[string]bool, len(block.Statements)),
		TermLiveIn:  make(map[string]bool),
		TermLiveOut: cloneSet(blockLiveOut),
	}

	// 1. Start with the block's global LiveOut
	currentLive := cloneSet(blockLiveOut)

	// 2. Process the Terminator
	termUses := getTerminatorUses(block.Terminator)
	for _, u := range termUses {
		currentLive[u] = true
	}
	res.TermLiveIn = cloneSet(currentLive)

	// 3. Walk statements backward
	for i := len(block.Statements) - 1; i >= 0; i-- {
		inst := block.Statements[i]

		// The LiveOut of this instruction is the current live set
		res.LiveOut[i] = cloneSet(currentLive)

		uses, defs := getInstUseDef(inst)

		// Equation: LiveIn = (LiveOut - Defs) U Uses

		// First, kill the defs
		for _, d := range defs {
			delete(currentLive, d)
		}

		// Then, gen the uses
		for _, u := range uses {
			currentLive[u] = true
		}

		// The new set is the LiveIn for this instruction
		res.LiveIn[i] = cloneSet(currentLive)
	}

	return res
}

func getTerminatorUses(term mir.Terminator) []string {
	var uses []string
	addUse := func(op mir.Value) {
		if reg, ok := op.(*mir.Register); ok && reg != nil {
			uses = append(uses, reg.Name)
		}
	}

	switch t := term.(type) {
	case *mir.BranchTerminator:
		addUse(t.Condition)
	case *mir.ReturnTerminator:
		if t.Value != nil {
			addUse(t.Value)
		}
	case *mir.CoroSuspendTerminator:
		// Suspend terminators themselves don't typically use local registers directly
	}
	return uses
}

func getInstUseDef(inst mir.Instruction) (uses []string, defs []string) {
	addUseVal := func(op mir.Value) {
		if reg, ok := op.(*mir.Register); ok && reg != nil {
			uses = append(uses, reg.Name)
		}
	}
	addUseName := func(name string) {
		if name != "" && name != "_" {
			uses = append(uses, name)
		}
	}
	addDef := func(name string) {
		if name != "" && name != "_" {
			defs = append(defs, name)
		}
	}

	switch i := inst.(type) {
	case *mir.TempDeclInst:
		addDef(i.Name)
	case *mir.AssignInst:
		addDef(i.Dst)
		addUseVal(i.RValue)
	case *mir.CopyInst:
		addDef(i.Dst)
		addUseName(i.Src)
	case *mir.MoveInst:
		addDef(i.Dst)
		addUseName(i.Src)
	case *mir.BorrowInst:
		addDef(i.Dst)
		addUseName(i.Src)
	case *mir.CastInst:
		addDef(i.Dst)
		addUseVal(i.Src)
	case *mir.BinaryOpInst:
		addDef(i.Dst)
		addUseVal(i.Left)
		addUseVal(i.Right)
	case *mir.UnaryOpInst:
		addDef(i.Dst)
		addUseVal(i.Operand)
	case *mir.StructInitInst:
		addUseName(i.Dst) // Mutation of existing container is a use
		addUseVal(i.Value)
	case *mir.FieldAddrInst:
		addDef(i.Dst)
		addUseVal(i.Object)
	case *mir.VariantInitInst:
		addDef(i.Dst)
		for _, p := range i.Payloads {
			addUseVal(p)
		}
	case *mir.VariantReadInst:
		addDef(i.Dst)
		addUseVal(i.Object)
	case *mir.VariantDiscriminantInst:
		addDef(i.Dst)
		addUseVal(i.Object)
	case *mir.ArrayInitInst:
		addUseName(i.Dst)
		addUseVal(i.Value)
	case *mir.SliceInst:
		addDef(i.Dst)
		addUseVal(i.Left)
		addUseVal(i.Low)
		addUseVal(i.High)
	case *mir.LoadPtrInst:
		addDef(i.Dst)
		addUseVal(i.Ptr)
	case *mir.StoreInst:
		addUseName(i.DstPtr)
		addUseVal(i.Value)
	case *mir.CallInst:
		if i.Dst != "" && i.Dst != "_" {
			addDef(i.Dst)
		}
		addUseVal(i.Function)
		for _, arg := range i.Arguments {
			addUseVal(arg)
		}
	case *mir.DropInst:
		addUseName(i.Src)
	case *mir.KeepAliveInst:
		addUseName(i.Src)
	}

	return uses, defs
}
