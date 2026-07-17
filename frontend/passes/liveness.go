package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

type LivenessResult struct {
	LiveIn  map[mir.BlockID]map[string]bool
	LiveOut map[mir.BlockID]map[string]bool
	Aliases map[string]string
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

func AnalyzeLiveness(g *mir.Graph, locals map[string]types.Type) *LivenessResult {
	res := &LivenessResult{
		LiveIn:  make(map[mir.BlockID]map[string]bool),
		LiveOut: make(map[mir.BlockID]map[string]bool),
		Aliases: buildAliasMap(g, locals),
	}

	for id := range g.Blocks {
		res.LiveIn[id] = make(map[string]bool)
		res.LiveOut[id] = make(map[string]bool)
	}

	blockUses := make(map[mir.BlockID]map[string]bool)
	blockDefs := make(map[mir.BlockID]map[string]bool)
	for _, block := range g.Blocks {
		useSet, defSet := computeBlockUseDef(block, res.Aliases)
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

func computeBlockUseDef(block *mir.BasicBlock, aliases map[string]string) (map[string]bool, map[string]bool) {
	useSet := make(map[string]bool)
	defSet := make(map[string]bool)

	// Helper to add a use and its resolved alias
	addUse := func(name string) {
		useSet[name] = true
		if root := resolveAlias(name, aliases); root != name {
			useSet[root] = true
		}
	}

	// --- STEP 1: Process Terminator Uses FIRST ---
	termUses := getTerminatorUses(block.Terminator)
	for _, u := range termUses {
		addUse(u)
	}

	// --- STEP 2: Traverse Statements BACKWARD (Bottom to Top) ---
	for i := len(block.Statements) - 1; i >= 0; i-- {
		inst := block.Statements[i]

		// Use the single source of truth for instruction use/defs
		uses, defs := getInstUseDef(inst)

		// A Def kills any upward Use path inside this block
		for _, d := range defs {
			defSet[d] = true
			delete(useSet, d)
		}

		// Add uses and their aliases
		for _, u := range uses {
			addUse(u)
		}
	}

	return useSet, defSet
}

// AnalyzeStatementLiveness computes the exact liveness across every instruction
// in a single block. This is required for Non-Lexical Lifetime (NLL) borrow checking.
func AnalyzeStatementLiveness(block *mir.BasicBlock, blockLiveOut map[string]bool, aliases map[string]string) *BlockStatementLiveness {
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
		if root := resolveAlias(u, aliases); root != u {
			currentLive[root] = true
		}
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
			if root := resolveAlias(u, aliases); root != u {
				currentLive[root] = true
			}
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
	case *mir.BitcastPtrInst:
		addDef(i.Dst)
		addUseVal(i.Src)
	case *mir.FieldAddrInst:
		addDef(i.Dst)
		addUseVal(i.Object)
	case *mir.LoadPtrInst:
		addDef(i.Dst)
		addUseVal(i.Ptr)
	case *mir.StoreInst:
		addUseVal(i.DstPtr)
		addUseVal(i.Value)
	case *mir.IndexAddrInst:
		addDef(i.Dst)
		addUseVal(i.Source)
		addUseVal(i.Index)
	case *mir.CallInst:
		if i.Dst != "" && i.Dst != "_" {
			addDef(i.Dst)
		}
		addUseVal(i.Function)
		for _, arg := range i.Arguments {
			addUseVal(arg)
		}
	case *mir.KeepAliveInst:
		addUseName(i.Src)
	}

	return uses, defs
}

func buildAliasMap(g *mir.Graph, locals map[string]types.Type) map[string]string {
	aliases := make(map[string]string)

	isView := func(name string) bool {
		t, ok := locals[name]
		if !ok {
			return false
		}
		_, ok = t.(*types.ViewType)
		return ok
	}

	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			switch i := inst.(type) {
			case *mir.BorrowInst:
				aliases[i.Dst] = i.Src
			case *mir.FieldAddrInst:
				if reg, ok := i.Object.(*mir.Register); ok {
					aliases[i.Dst] = reg.Name
				}
			case *mir.IndexAddrInst:
				if reg, ok := i.Source.(*mir.Register); ok {
					aliases[i.Dst] = reg.Name
				}
			case *mir.AddressOfInst:
				aliases[i.Dst] = i.Src
			case *mir.LoadPtrInst:
				if reg, ok := i.Ptr.(*mir.Register); ok {
					if root := resolveAlias(reg.Name, aliases); root != reg.Name {
						aliases[i.Dst] = root
					}
				}
			case *mir.BitcastPtrInst:
				if reg, ok := i.Src.(*mir.Register); ok {
					aliases[i.Dst] = reg.Name
				}
			case *mir.CallInst:
				if reg, ok := i.Function.(*mir.Register); ok {
					if reg.Name == "maml_vec_get" || reg.Name == "maml_map_get" {
						if len(i.Arguments) > 0 {
							if argReg, ok := i.Arguments[0].(*mir.Register); ok {
								aliases[i.Dst] = argReg.Name
							}
						}
					}
				}
			case *mir.CoroPromisePtrInst:
				if reg, ok := i.Handle.(*mir.Register); ok {
					aliases[i.Dst] = reg.Name
				}
			case *mir.StoreInst:
				if srcReg, isReg := i.Value.(*mir.Register); isReg {
					if dstPtrReg, isPtrReg := i.DstPtr.(*mir.Register); isPtrReg {
						dstRoot := resolveAlias(dstPtrReg.Name, aliases)
						if isView(dstRoot) {
							srcRoot := resolveAlias(srcReg.Name, aliases)
							if srcRoot != dstRoot {
								aliases[dstRoot] = srcRoot
							}
						}
					}
				}
			}

			// --- NEW: Track alias transfers for Move, Copy, and Assignment ---
			if mv, ok := inst.(*mir.MoveInst); ok && isView(mv.Dst) {
				if root := resolveAlias(mv.Src, aliases); root != mv.Src {
					aliases[mv.Dst] = root
				}
			}
			
			if cp, ok := inst.(*mir.CopyInst); ok && isView(cp.Dst) {
				if root := resolveAlias(cp.Src, aliases); root != cp.Src {
					aliases[cp.Dst] = root
				}
			}
			
			if as, ok := inst.(*mir.AssignInst); ok && isView(as.Dst) {
				if reg, ok := as.RValue.(*mir.Register); ok {
					if root := resolveAlias(reg.Name, aliases); root != reg.Name {
						aliases[as.Dst] = root
					}
				}
			}
		}
	}
	return aliases
}

func resolveAlias(name string, aliases map[string]string) string {
	if parent, exists := aliases[name]; exists && parent != name {
		return resolveAlias(parent, aliases)
	}
	return name
}
