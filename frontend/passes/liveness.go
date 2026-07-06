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

	// Local helper closures
	addUse := func(op mir.Value) {
		if reg, ok := op.(*mir.Register); ok && reg != nil {
			useSet[reg.Name] = true
		}
	}

	addUseName := func(name string) {
		if name != "" && name != "_" {
			useSet[name] = true
			if root := resolveAlias(name, aliases); root != name {
				useSet[root] = true
			}
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
		case *mir.LoadPtrInst:
			addDef(i.Dst)
			addUse(i.Ptr)
		case *mir.StoreInst:
			addUseName(i.DstPtr)
			addUse(i.Value)
		case *mir.BitcastPtrInst:
			addDef(i.Dst)
			addUse(i.Src)
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
		case *mir.IndexAddrInst:
			addDef(i.Dst)
			addUse(i.Source)
			addUse(i.Index)
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
		addUseName(i.DstPtr)
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

			// Reborrow propagation through stores — but ONLY when the
			// destination belongs to a View. Views never own memory, so
			// this can't create a second owner / double free; it just
			// lets a View correctly "remember" the buffer it was built
			// from, for liveness purposes.
			case *mir.StoreInst:
				if srcReg, isReg := i.Value.(*mir.Register); isReg {
					dstRoot := resolveAlias(i.DstPtr, aliases)
					if isView(dstRoot) {
						srcRoot := resolveAlias(srcReg.Name, aliases)
						if srcRoot != dstRoot {
							aliases[dstRoot] = srcRoot
						}
					}
				}
			}

			// Propagate aliases through moves, but ONLY for View-typed
			// values. Views own nothing, so re-pointing a view's alias
			// chain on a move is always safe. We deliberately do NOT do
			// this for owning types (Vec, Map, String, ...): for those the
			// move is a genuine ownership transfer, and treating the
			// destination as "borrowing from" the (now-dead) source temp
			// wrongly keeps that source temp eligible for its own separate
			// drop — which is exactly what caused the double frees.
			if mv, ok := inst.(*mir.MoveInst); ok && isView(mv.Dst) {
				if root := resolveAlias(mv.Src, aliases); root != mv.Src {
					aliases[mv.Dst] = root
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
