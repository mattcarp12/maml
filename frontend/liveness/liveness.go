package liveness

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

	changed := true
	for changed {
		changed = false

		// Iterate backward through blocks (reverse order speeds up convergence)
		for id := mir.BlockID(len(g.Blocks) - 1); id >= 0; id-- {
			block, exists := g.Blocks[id]
			if !exists {
				continue
			}

			// 1. LiveOut = Union of LiveIn of all successors
			newLiveOut := make(map[string]bool)
			succs := getSuccessors(block)
			for _, succID := range succs {
				for v := range res.LiveIn[succID] {
					newLiveOut[v] = true
				}
			}

			// 2. Compute Use and Def for this block
			useSet, defSet := computeBlockUseDef(block)

			// 3. LiveIn = Use U (LiveOut - Def)
			newLiveIn := make(map[string]bool)
			for v := range useSet {
				newLiveIn[v] = true
			}
			for v := range newLiveOut {
				if !defSet[v] {
					newLiveIn[v] = true
				}
			}

			// 4. Check for changes
			if !mapEquals(res.LiveIn[id], newLiveIn) || !mapEquals(res.LiveOut[id], newLiveOut) {
				res.LiveIn[id] = newLiveIn
				res.LiveOut[id] = newLiveOut
				changed = true
			}
		}
	}

	return res
}

func computeBlockUseDef(block *mir.BasicBlock) (map[string]bool, map[string]bool) {
	useSet := make(map[string]bool)
	defSet := make(map[string]bool)

	// Traverse forward to compute block-level summary
	for _, inst := range block.Statements {
		switch i := inst.(type) {
		case *mir.TempDeclInst:
			defSet[i.Name] = true
		case *mir.RefAllocInst:
			defSet[i.Dst] = true
		case *mir.AssignInst:
			for _, u := range extractUses(i.Expr) {
				if !defSet[u] {
					useSet[u] = true
				}
			}
			defSet[i.Dst] = true
		case *mir.CopyInst:
			if !defSet[i.Src] {
				useSet[i.Src] = true
			}
			defSet[i.Dst] = true
		case *mir.MoveInst:
			if !defSet[i.Src] {
				useSet[i.Src] = true
			}
			defSet[i.Dst] = true
		}
	}

	// Terminators also USE variables (e.g., branching on a condition)
	switch t := block.Terminator.(type) {
	case *mir.BranchTerminator:
		for _, u := range extractUses(t.Condition) {
			if !defSet[u] {
				useSet[u] = true
			}
		}
	case *mir.ReturnTerminator:
		// Functions always return via _ret
		if !defSet["_ret"] {
			useSet["_ret"] = true
		}
	}

	return useSet, defSet
}

func extractUses(expr hir.Expr) []string {
	var uses []string
	switch e := expr.(type) {
	case *hir.Identifier:
		uses = append(uses, e.Value)
	case *hir.InfixExpr:
		uses = append(uses, extractUses(e.Left)...)
		uses = append(uses, extractUses(e.Right)...)
	case *hir.PrefixExpr:
		uses = append(uses, extractUses(e.Right)...)
	case *hir.CallExpr:
		uses = append(uses, extractUses(e.Function)...)
		for _, arg := range e.Arguments {
			uses = append(uses, extractUses(arg.Argument)...)
		}
	case *hir.SliceExpr:
		uses = append(uses, extractUses(e.Left)...)
		uses = append(uses, extractUses(e.Low)...)
		uses = append(uses, extractUses(e.High)...)
	case *hir.IndexExpr:
		uses = append(uses, extractUses(e.Left)...)
		uses = append(uses, extractUses(e.Index)...)
	case *hir.FieldAccess:
		uses = append(uses, extractUses(e.Object)...)
	}
	return uses
}

func getSuccessors(block *mir.BasicBlock) []mir.BlockID {
	switch t := block.Terminator.(type) {
	case *mir.JumpTerminator:
		return []mir.BlockID{t.Target}
	case *mir.BranchTerminator:
		return []mir.BlockID{t.TrueTarget, t.FalseTarget}
	}
	return nil
}

func mapEquals(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
