package passes

import (
	"fmt"
	"sort"
	"testing"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Helper to format map keys cleanly for comparison
func formatSet(set map[string]bool) []string {
	var keys []string
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLiveness_LoopVariable(t *testing.T) {
	// Let's model a graph with a loop where a variable 'x' is used across blocks.
	// Block 0 (Entry): defines x -> Jumps to Block 1
	// Block 1 (Loop Header): uses x -> Branches to Block 1 (loop back) or Block 2 (exit)
	// Block 2 (Exit): Returns
	g := mir.NewGraph()
	g.Entry = 0

	b0 := &mir.BasicBlock{ID: 0}
	b1 := &mir.BasicBlock{ID: 1}
	b2 := &mir.BasicBlock{ID: 2}

	g.Blocks[0] = b0
	g.Blocks[1] = b1
	g.Blocks[2] = b2

	// Block 0: x = RefAlloc
	b0.Statements = append(b0.Statements, &mir.RefAllocInst{Dst: "x", Type: &types.VectorType{}})
	b0.Terminator = &mir.JumpTerminator{Target: 1}

	// Block 1: Loop Header uses 'x'
	// We mimic a use via an AssignInst read or passing it somewhere
	b1.Statements = append(b1.Statements, &mir.AssignInst{
		Dst:    "temp",
		RValue: &mir.Register{Name: "x", Type: &types.VectorType{}},
	})
	b1.Terminator = &mir.BranchTerminator{Condition: &mir.Register{Name: "cond"}, TrueTarget: 1, FalseTarget: 2}

	// Block 2: Exit
	b2.Terminator = &mir.ReturnTerminator{}

	// Run Liveness Analysis
	result := AnalyzeLiveness(g)

	// --- Step 4.1 Debug Dump Output ---
	fmt.Println("--- Liveness Debug Output ---")
	for _, b := range g.SortedBlocks() {
		fmt.Printf("Block %d:\n", b.ID)
		fmt.Printf("  LiveIn:  %v\n", formatSet(result.LiveIn[b.ID]))
		fmt.Printf("  LiveOut: %v\n", formatSet(result.LiveOut[b.ID]))
	}

	// --- Assertions ---
	// 'x' should be LiveIn and LiveOut of Block 1 because it's evaluated in the loop body/header
	if !result.LiveIn[1]["x"] {
		t.Errorf("Expected 'x' to be LiveIn to the loop header (Block 1)")
	}
	// 'x' should be LiveOut of Block 0 because it flows into Block 1
	if !result.LiveOut[0]["x"] {
		t.Errorf("Expected 'x' to be LiveOut from entry block (Block 0)")
	}
	// 'x' should NOT be LiveIn or LiveOut of Block 2 (it died at loop exit)
	if result.LiveIn[2]["x"] {
		t.Errorf("Expected 'x' to be dead entering the exit block (Block 2)")
	}
}
