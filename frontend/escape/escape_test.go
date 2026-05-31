package escape

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/tast"
)

// =============================================================================
// Phase 1: Test Graph & Instruction Helper Utilities
// =============================================================================

func newTestGraph(entryID int) *mir.Graph {
	g := mir.NewGraph()
	g.Entry = mir.BlockID(entryID)
	return g
}

func addBlock(g *mir.Graph, id int) *mir.BasicBlock {
	block := &mir.BasicBlock{
		ID:         mir.BlockID(id),
		Statements: []mir.Instruction{},
	}
	g.Blocks[mir.BlockID(id)] = block
	return block
}

func identExpr(name string) *tast.Identifier {
	return &tast.Identifier{
		Value: name,
	}
}

func assignInst(dst string, expr tast.Expr) *mir.AssignInst {
	return &mir.AssignInst{
		Dst:    dst,
		RValue: expr,
	}
}

func copyInst(dst, src string) *mir.CopyInst {
	return &mir.CopyInst{
		Dst: dst,
		Src: src,
	}
}

func moveInst(dst, src string) *mir.MoveInst {
	return &mir.MoveInst{
		Dst: dst,
		Src: src,
	}
}

func callExpr(fnName string, args ...tast.CallArg) *tast.CallExpr {
	return &tast.CallExpr{
		Function:  identExpr(fnName),
		Arguments: args,
	}
}

func callArg(name string, own, mut bool) tast.CallArg {
	return tast.CallArg{
		Argument: identExpr(name),
		Own:      own,
		Mut:      mut,
	}
}

// =============================================================================
// Phase 2: Table-Driven Test Structure Definitions
// =============================================================================

type escapeTestCase struct {
	name        string
	setupGraph  func() *mir.Graph
	expectedMap map[string]EscapeState
}

// =============================================================================
// Phase 3: Populating the Table Matrix with Concrete CFG Structures
// =============================================================================

func TestAnalyzeEscape(t *testing.T) {
	tests := []escapeTestCase{
		{
			name: "Base Case - No Escape",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b0.Statements = append(b0.Statements, copyInst("x", "y"))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret": StateHeap,
			},
		},
		{
			name: "Direct Return Escape via AssignInst",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b0.Statements = append(b0.Statements, assignInst("_ret", identExpr("x")))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret": StateHeap,
				"x":    StateHeap,
			},
		},
		{
			name: "Direct Return Escape via Copy and Move",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b0.Statements = append(b0.Statements, copyInst("_ret", "y"))
				b0.Statements = append(b0.Statements, moveInst("_ret", "z"))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret": StateHeap,
				"y":    StateHeap,
				"z":    StateHeap,
			},
		},
		{
			name: "Transitive Chain Escape (Fixed-Point Verification)",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b0.Statements = append(b0.Statements, assignInst("_ret", identExpr("a")))
				b0.Statements = append(b0.Statements, assignInst("a", identExpr("b")))
				b0.Statements = append(b0.Statements, copyInst("b", "c"))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret": StateHeap,
				"a":    StateHeap,
				"b":    StateHeap,
				"c":    StateHeap,
			},
		},
		{
			name: "Control Flow Convergent - Out of Order Blocks",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b1 := addBlock(g, 1)
				b0.Statements = append(b0.Statements, moveInst("x", "y"))
				b1.Statements = append(b1.Statements, assignInst("_ret", identExpr("x")))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret": StateHeap,
				"x":    StateHeap,
				"y":    StateHeap,
			},
		},
		{
			name: "Complex Unhandled Expression Skipping & Conservative Call Evaluation",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0)
				b0.Statements = append(b0.Statements, assignInst("_ret", &tast.IntLiteral{Value: 42}))
				b0.Statements = append(b0.Statements, assignInst("_ret", callExpr("foo", callArg("param1", true, false))))
				return g
			},
			expectedMap: map[string]EscapeState{
				"_ret":   StateHeap,
				"param1": StateHeap,
			},
		},
	}

	// =============================================================================
	// Phase 4: Execution Logic & Assertions
	// =============================================================================

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := tc.setupGraph()
			require.NotNil(t, g, "The generated test MIR Graph must not be nil")

			actualMap := AnalyzeEscape(g)

			assert.Equal(t, tc.expectedMap, actualMap, "The computed EscapeMap did not match the expected state")
		})
	}
}
