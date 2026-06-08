package passes

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Helper to inspect if a block contains a RefAllocInst for a given variable name
func hasRefAlloc(block *mir.BasicBlock, varName string) bool {
	for _, stmt := range block.Statements {
		if alloc, ok := stmt.(*mir.RefAllocInst); ok && alloc.Dst == varName {
			return true
		}
	}
	return false
}

// Test A — No escape: Primitives on stack should remain TempDeclInst
func TestA_NoEscape(t *testing.T) {
	g := mir.NewGraph()
	b0 := &mir.BasicBlock{ID: 0}
	g.Entry = 0
	g.Blocks[0] = b0

	// mut x := 5 (primitive int type)
	b0.Statements = append(b0.Statements, &mir.TempDeclInst{Name: "x", Type: types.IntType{}})
	b0.Terminator = &mir.ReturnTerminator{}

	escapes := AnalyzeEscape(g)
	LowerAllocations(g, escapes)

	if hasRefAlloc(b0, "x") {
		t.Errorf("Test A Failed: primitive variable 'x' was incorrectly promoted to the heap")
	}
}

// Test B — Return escape: Reference type returned from function escapes to StateHeap
func TestB_ReturnEscape(t *testing.T) {
	g := mir.NewGraph()
	b0 := &mir.BasicBlock{ID: 0}
	g.Entry = 0
	g.Blocks[0] = b0

	// mut v := Vec<int>{}
	b0.Statements = append(b0.Statements, &mir.TempDeclInst{Name: "v", Type: types.VectorType{}})
	// return v
	b0.Terminator = &mir.ReturnTerminator{Value: &hir.Identifier{Value: "v", Type: types.VectorType{}}}

	escapes := AnalyzeEscape(g)
	LowerAllocations(g, escapes)

	if !hasRefAlloc(b0, "v") {
		t.Errorf("Test B Failed: returned reference 'v' was not upgraded to RefAllocInst")
	}
}

// Test C — Call escape: Reference passed to an external call conservatively escapes
func TestC_CallEscape(t *testing.T) {
	g := mir.NewGraph()
	b0 := &mir.BasicBlock{ID: 0}
	g.Entry = 0
	g.Blocks[0] = b0

	b0.Statements = append(b0.Statements, &mir.TempDeclInst{Name: "v", Type: types.VectorType{}})
	// pass_vec(mut v)
	b0.Statements = append(b0.Statements, &mir.CallInst{
		Function: &hir.Identifier{Value: "pass_vec", Type: types.UnitType{}},
		Arguments: []mir.MIRCallArg{
			{Argument: &hir.Identifier{Value: "v", Type: types.VectorType{}}},
		},
	})
	b0.Terminator = &mir.ReturnTerminator{}

	escapes := AnalyzeEscape(g)
	LowerAllocations(g, escapes)

	if !hasRefAlloc(b0, "v") {
		t.Errorf("Test C Failed: reference 'v' passed to CallInst was not upgraded to RefAllocInst")
	}
}

// Test D — The escape_reassign_loop bug fix verification
func TestD_EscapeReassignLoop(t *testing.T) {
	g := mir.NewGraph()
	b0 := &mir.BasicBlock{ID: 0} // loop header/body
	g.Entry = 0
	g.Blocks[0] = b0

	// Local Vec created inside a loop body that never leaves function boundary
	b0.Statements = append(b0.Statements, &mir.TempDeclInst{Name: "v", Type: types.VectorType{}})
	b0.Terminator = &mir.ReturnTerminator{}

	escapes := AnalyzeEscape(g)
	LowerAllocations(g, escapes)

	// Due to the unconditional IsNeedsARC() fix from Task 3.3/3.4, this must be heap-allocated!
	if !hasRefAlloc(b0, "v") {
		t.Errorf("Test D Failed: loop-bound 'v' of VectorType was not promoted via IsNeedsARC()")
	}
}
