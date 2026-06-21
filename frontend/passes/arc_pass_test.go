package passes

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Helper to count the number of RefDecInst for a given variable name in a block
func countReleases(block *mir.BasicBlock, varName string) int {
	count := 0
	for _, stmt := range block.Statements {
		if dec, ok := stmt.(*mir.RefDecInst); ok && dec.Src == varName {
			count++
		}
	}
	return count
}

// Scenario 1: Basic local owning reference lifecycle.
// A Vector is allocated locally and becomes dead at block exit. It must receive a release call.
func TestARC_LocalLifecycle(t *testing.T) {
	g := mir.NewGraph()
	g.Entry = 0
	b0 := &mir.BasicBlock{ID: 0}
	g.Blocks[0] = b0

	// mut v := Vec<int>{}
	b0.Statements = append(b0.Statements, &mir.RefAllocInst{Dst: "v", Type: &types.VectorType{}})
	b0.Terminator = &mir.ReturnTerminator{}

	// Run dataflow dependencies
	livenessRes := AnalyzeLiveness(g)
	InjectARC(g, livenessRes, nil)

	releases := countReleases(b0, "v")
	if releases != 1 {
		t.Errorf("Expected exactly 1 maml_release (RefDecInst) for local variable 'v', got %d", releases)
	}
}

// Scenario 2: Function Parameter Protection (Step 4.4)
// A Vector is passed in as a parameter. It goes out of scope, but since the caller owns it,
// the callee must NOT inject a release call.
func TestARC_ParameterProtection(t *testing.T) {
	g := mir.NewGraph()
	g.Entry = 0
	g.Params = []mir.Param{{"param_vec", &types.VectorType{}}} // Tracked as function parameter
	b0 := &mir.BasicBlock{ID: 0}
	g.Blocks[0] = b0

	// Parameter is registered as temporary inside the function scope but is live-in
	b0.Statements = append(b0.Statements, &mir.TempDeclInst{Name: "param_vec", Type: &types.VectorType{}})
	b0.Terminator = &mir.ReturnTerminator{}

	livenessRes := AnalyzeLiveness(g)
	InjectARC(g, livenessRes, nil)

	releases := countReleases(b0, "param_vec")
	if releases != 0 {
		t.Errorf("Security Violation: Caller-owned parameter 'param_vec' was incorrectly released by the callee!")
	}
}

// Scenario 3: Non-owning View Type Interference Guard (Step 4.2)
// A slice/view type is reference-based but doesn't own its heap data. It must never trigger ARC.
func TestARC_ViewTypeExclusion(t *testing.T) {
	g := mir.NewGraph()
	g.Entry = 0
	b0 := &mir.BasicBlock{ID: 0}
	g.Blocks[0] = b0

	// mut view := slice
	b0.Statements = append(b0.Statements, &mir.RefAllocInst{Dst: "my_view", Type: &types.ViewType{}})
	b0.Terminator = &mir.ReturnTerminator{}

	livenessRes := AnalyzeLiveness(g)
	InjectARC(g, livenessRes, nil)

	releases := countReleases(b0, "my_view")
	if releases != 0 {
		t.Errorf("Memory Corruption: Non-owning ViewType 'my_view' had an %d ARC releases injected!", releases)
	}
}
