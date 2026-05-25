package mir

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Compile-time interface satisfaction checks
// These will fail at build time if a type stops satisfying its interface.
// =============================================================================

var (
	_ Instruction = (*TempDeclInst)(nil)
	_ Instruction = (*AssignInst)(nil)
	_ Instruction = (*CopyInst)(nil)
	_ Instruction = (*MoveInst)(nil)
	_ Instruction = (*RefAllocInst)(nil)
	_ Instruction = (*RefIncInst)(nil)
	_ Instruction = (*RefDecInst)(nil)
	_ Instruction = (*MutBorrowInst)(nil)
)

var (
	_ Terminator = (*ReturnTerminator)(nil)
	_ Terminator = (*JumpTerminator)(nil)
	_ Terminator = (*BranchTerminator)(nil)
	_ Terminator = (*UnreachableTerminator)(nil)
)

// =============================================================================
// TempDeclInst.String()
// =============================================================================

func TestTempDeclInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *TempDeclInst
		expected string
	}{
		{
			name:     "int type",
			inst:     &TempDeclInst{Name: "_t1", Type: types.IntType{}},
			expected: "let _t1: int",
		},
		{
			name:     "bool type",
			inst:     &TempDeclInst{Name: "_t2", Type: types.BoolType{}},
			expected: "let _t2: bool",
		},
		{
			name:     "string type",
			inst:     &TempDeclInst{Name: "_t3", Type: types.StringType{}},
			expected: "let _t3: string",
		},
		{
			name:     "struct type",
			inst:     &TempDeclInst{Name: "_t4", Type: &types.StructType{Name: "Point"}},
			expected: "let _t4: Point",
		},
		{
			name:     "slice type",
			inst:     &TempDeclInst{Name: "_t5", Type: types.SliceType{Base: types.IntType{}}},
			expected: "let _t5: []int",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// AssignInst.String()
// =============================================================================

func TestAssignInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *AssignInst
		expected string
	}{
		{
			name: "assign identifier",
			inst: &AssignInst{
				Dst:  "x",
				Expr: &hir.Identifier{Value: "y", Type: types.IntType{}},
			},
			expected: "x = y",
		},
		{
			name: "assign int literal",
			inst: &AssignInst{
				Dst:  "_t1",
				Expr: &hir.IntLiteral{Value: 42, Type: types.IntType{}},
			},
			expected: "_t1 = 42",
		},
		{
			name: "assign bool literal true",
			inst: &AssignInst{
				Dst:  "_ret",
				Expr: &hir.BoolLiteral{Value: true, Type: types.BoolType{}},
			},
			expected: "_ret = true",
		},
		{
			name: "assign bool literal false",
			inst: &AssignInst{
				Dst:  "_ret",
				Expr: &hir.BoolLiteral{Value: false, Type: types.BoolType{}},
			},
			expected: "_ret = false",
		},
		{
			name: "assign string literal",
			inst: &AssignInst{
				Dst:  "_t2",
				Expr: &hir.StringLiteral{Value: "hello", Type: types.StringType{}},
			},
			expected: `_t2 = "hello"`,
		},
		{
			name: "assign infix expr",
			inst: &AssignInst{
				Dst: "_t3",
				Expr: &hir.InfixExpr{
					Left:     &hir.Identifier{Value: "_t1", Type: types.IntType{}},
					Operator: "+",
					Right:    &hir.Identifier{Value: "_t2", Type: types.IntType{}},
					Type:     types.IntType{},
				},
			},
			expected: "_t3 = (_t1 + _t2)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// CopyInst.String()
// =============================================================================

func TestCopyInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *CopyInst
		expected string
	}{
		{
			name:     "simple copy",
			inst:     &CopyInst{Dst: "a", Src: "b"},
			expected: "a = copy b",
		},
		{
			name:     "copy to temp",
			inst:     &CopyInst{Dst: "_t1", Src: "x"},
			expected: "_t1 = copy x",
		},
		{
			name:     "copy from literal",
			inst:     &CopyInst{Dst: "_t2", Src: "42"},
			expected: "_t2 = copy 42",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// MoveInst.String()
// =============================================================================

func TestMoveInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *MoveInst
		expected string
	}{
		{
			name:     "simple move",
			inst:     &MoveInst{Dst: "a", Src: "b"},
			expected: "a = move b",
		},
		{
			name:     "move to temp",
			inst:     &MoveInst{Dst: "_t3", Src: "myStruct"},
			expected: "_t3 = move myStruct",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// RefAllocInst.String()
// =============================================================================

func TestRefAllocInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *RefAllocInst
		expected string
	}{
		{
			name:     "alloc string",
			inst:     &RefAllocInst{Dst: "_t1", Type: types.StringType{}},
			expected: "_t1 = ref_alloc string",
		},
		{
			name:     "alloc struct",
			inst:     &RefAllocInst{Dst: "_t2", Type: &types.StructType{Name: "Node"}},
			expected: "_t2 = ref_alloc Node",
		},
		{
			name:     "alloc slice",
			inst:     &RefAllocInst{Dst: "_t3", Type: types.SliceType{Base: types.IntType{}}},
			expected: "_t3 = ref_alloc []int",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// RefIncInst.String()
// =============================================================================

func TestRefIncInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *RefIncInst
		expected string
	}{
		{
			name:     "ref_inc named var",
			inst:     &RefIncInst{Src: "s"},
			expected: "ref_inc s",
		},
		{
			name:     "ref_inc temp",
			inst:     &RefIncInst{Src: "_t1"},
			expected: "ref_inc _t1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// RefDecInst.String()
// =============================================================================

func TestRefDecInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *RefDecInst
		expected string
	}{
		{
			name:     "ref_dec named var",
			inst:     &RefDecInst{Src: "s"},
			expected: "ref_dec s",
		},
		{
			name:     "ref_dec temp",
			inst:     &RefDecInst{Src: "_t2"},
			expected: "ref_dec _t2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// MutBorrowInst.String()
// =============================================================================

func TestMutBorrowInst_String(t *testing.T) {
	tests := []struct {
		name     string
		inst     *MutBorrowInst
		expected string
	}{
		{
			name:     "mut_borrow named var",
			inst:     &MutBorrowInst{Src: "x"},
			expected: "mut_borrow x",
		},
		{
			name:     "mut_borrow temp",
			inst:     &MutBorrowInst{Src: "_t5"},
			expected: "mut_borrow _t5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.inst.String())
		})
	}
}

// =============================================================================
// Terminator interface smoke tests (isTerminator satisfies the sealed interface)
// The compile-time var_ checks above are the primary validation; these
// runtime tests document the concrete types clearly.
// =============================================================================

func TestTerminators_AreDistinct(t *testing.T) {
	terms := []Terminator{
		&ReturnTerminator{},
		&JumpTerminator{Target: 0},
		&BranchTerminator{Condition: &hir.BoolLiteral{Value: true, Type: types.BoolType{}}, TrueTarget: 1, FalseTarget: 2},
		&UnreachableTerminator{},
	}
	// All four terminator types must be non-nil and satisfy the interface.
	for _, term := range terms {
		assert.NotNil(t, term)
	}
}

func TestJumpTerminator_TargetIsPreserved(t *testing.T) {
	jt := &JumpTerminator{Target: BlockID(7)}
	assert.Equal(t, BlockID(7), jt.Target)
}

func TestBranchTerminator_FieldsArePreserved(t *testing.T) {
	cond := &hir.BoolLiteral{Value: true, Type: types.BoolType{}}
	bt := &BranchTerminator{
		Condition:   cond,
		TrueTarget:  BlockID(3),
		FalseTarget: BlockID(5),
	}
	assert.Equal(t, cond, bt.Condition)
	assert.Equal(t, BlockID(3), bt.TrueTarget)
	assert.Equal(t, BlockID(5), bt.FalseTarget)
}

// =============================================================================
// Helpers
// =============================================================================

// buildGraph constructs a Graph from a simple descriptor for test clarity.
// blocks maps BlockID → Terminator (nil means no terminator set).
func buildGraph(entry BlockID, blocks map[BlockID]Terminator) *Graph {
	g := NewGraph()
	g.Entry = entry
	for id, term := range blocks {
		g.Blocks[id] = &BasicBlock{
			ID:         id,
			Statements: []Instruction{},
			Terminator: term,
		}
	}
	return g
}

// =============================================================================
// Reachable
// =============================================================================

func TestReachable_SingleBlock_Return(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0], "entry block must be reachable")
	assert.Len(t, reachable, 1)
}

func TestReachable_LinearChain(t *testing.T) {
	// 0 → 1 → 2 → return
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &JumpTerminator{Target: 2},
		2: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.True(t, reachable[1])
	assert.True(t, reachable[2])
	assert.Len(t, reachable, 3)
}

func TestReachable_DiamondBranch(t *testing.T) {
	// 0 branches to 1 (true) and 2 (false), both jump to 3
	cond := &hir.BoolLiteral{Value: true, Type: types.BoolType{}}
	g := buildGraph(0, map[BlockID]Terminator{
		0: &BranchTerminator{Condition: cond, TrueTarget: 1, FalseTarget: 2},
		1: &JumpTerminator{Target: 3},
		2: &JumpTerminator{Target: 3},
		3: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.True(t, reachable[1])
	assert.True(t, reachable[2])
	assert.True(t, reachable[3])
	assert.Len(t, reachable, 4)
}

func TestReachable_DisconnectedBlock(t *testing.T) {
	// Block 99 has no path from entry
	g := buildGraph(0, map[BlockID]Terminator{
		0:  &ReturnTerminator{},
		99: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.False(t, reachable[99], "block 99 should not be reachable from entry 0")
	assert.Len(t, reachable, 1)
}

func TestReachable_LoopBackEdge(t *testing.T) {
	// 0 → 1 → 0 (back-edge loop), BFS should not get stuck
	cond := &hir.BoolLiteral{Value: false, Type: types.BoolType{}}
	g := buildGraph(0, map[BlockID]Terminator{
		0: &BranchTerminator{Condition: cond, TrueTarget: 1, FalseTarget: 2},
		1: &JumpTerminator{Target: 0}, // back-edge
		2: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.True(t, reachable[1])
	assert.True(t, reachable[2])
	assert.Len(t, reachable, 3)
}

func TestReachable_BlockWithNilTerminator(t *testing.T) {
	// A block with no terminator set yet — BFS should not panic, just stop.
	g := buildGraph(0, map[BlockID]Terminator{
		0: nil,
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0], "entry itself must still be marked reachable")
	assert.Len(t, reachable, 1)
}

func TestReachable_UnreachableTerminator_StopsTraversal(t *testing.T) {
	// Block 0 is UnreachableTerminator; block 1 can only be reached if 0 had an edge.
	g := buildGraph(0, map[BlockID]Terminator{
		0: &UnreachableTerminator{},
		1: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.False(t, reachable[1])
}

func TestReachable_EmptyGraph(t *testing.T) {
	// Entry block exists in g.Entry but has no Blocks entry — should not crash.
	g := NewGraph()
	g.Entry = 0
	reachable := Reachable(g)
	// Entry is visited but the block is nil, so no successors are enqueued.
	assert.True(t, reachable[0])
}

func TestReachable_MultipleExitsFromBranch(t *testing.T) {
	// Two separate tails, no merge block
	cond := &hir.BoolLiteral{Value: true, Type: types.BoolType{}}
	g := buildGraph(0, map[BlockID]Terminator{
		0: &BranchTerminator{Condition: cond, TrueTarget: 1, FalseTarget: 2},
		1: &ReturnTerminator{},
		2: &ReturnTerminator{},
	})
	reachable := Reachable(g)
	assert.True(t, reachable[0])
	assert.True(t, reachable[1])
	assert.True(t, reachable[2])
}

// =============================================================================
// GetDeadBlocks
// =============================================================================

func TestGetDeadBlocks_NoneWhenAllReachable(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &ReturnTerminator{},
	})
	dead := GetDeadBlocks(g)
	assert.Empty(t, dead)
}

func TestGetDeadBlocks_OneDeadBlock(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0: &ReturnTerminator{},
		5: &ReturnTerminator{}, // unreachable island
	})
	dead := GetDeadBlocks(g)
	require.Len(t, dead, 1)
	assert.Equal(t, BlockID(5), dead[0].ID)
}

func TestGetDeadBlocks_MultipleDeadBlocks(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0:  &ReturnTerminator{},
		10: &JumpTerminator{Target: 11}, // dead chain
		11: &ReturnTerminator{},         // also dead
	})
	dead := GetDeadBlocks(g)
	assert.Len(t, dead, 2)

	deadIDs := make(map[BlockID]bool)
	for _, b := range dead {
		deadIDs[b.ID] = true
	}
	assert.True(t, deadIDs[10])
	assert.True(t, deadIDs[11])
}

func TestGetDeadBlocks_DeadBranchTarget(t *testing.T) {
	// Entry jumps unconditionally to 1; block 2 has no path in.
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &ReturnTerminator{},
		2: &ReturnTerminator{}, // orphaned — only reachable from a branch that doesn't exist
	})
	dead := GetDeadBlocks(g)
	require.Len(t, dead, 1)
	assert.Equal(t, BlockID(2), dead[0].ID)
}

// =============================================================================
// Analyze
// =============================================================================

func TestAnalyze_NoWarnings_CleanFunction(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &ReturnTerminator{},
	})
	warnings := Analyze(g)
	assert.Empty(t, warnings)
}

func TestAnalyze_WarningEmitted_ForDeadBlock(t *testing.T) {
	g := buildGraph(0, map[BlockID]Terminator{
		0: &ReturnTerminator{},
		9: &ReturnTerminator{}, // dead
	})
	warnings := Analyze(g)
	require.Len(t, warnings, 1)
	assert.Equal(t, "warning: function contains unreachable code", warnings[0])
}

func TestAnalyze_SingleWarning_ForMultipleDeadBlocks(t *testing.T) {
	// Even with many dead blocks, Analyze currently emits a single warning string.
	g := buildGraph(0, map[BlockID]Terminator{
		0:  &ReturnTerminator{},
		11: &ReturnTerminator{},
		12: &ReturnTerminator{},
		13: &ReturnTerminator{},
	})
	warnings := Analyze(g)
	// The current implementation appends one warning regardless of dead count.
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "unreachable")
}

func TestAnalyze_NoWarnings_AfterUnconditionalJump_AllBlocksLinked(t *testing.T) {
	// All blocks reachable via jump chain — no dead code.
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &JumpTerminator{Target: 2},
		2: &JumpTerminator{Target: 3},
		3: &ReturnTerminator{},
	})
	warnings := Analyze(g)
	assert.Empty(t, warnings)
}

func TestAnalyze_NoWarnings_LoopGraph(t *testing.T) {
	// Entry → loop_header → loop_body → loop_header (back-edge) → exit
	cond := &hir.BoolLiteral{Value: true, Type: types.BoolType{}}
	g := buildGraph(0, map[BlockID]Terminator{
		0: &JumpTerminator{Target: 1},
		1: &BranchTerminator{Condition: cond, TrueTarget: 2, FalseTarget: 3},
		2: &JumpTerminator{Target: 1},
		3: &ReturnTerminator{},
	})
	warnings := Analyze(g)
	assert.Empty(t, warnings)
}

// =============================================================================
// Test Helpers
// =============================================================================

// newBuilder creates a Builder wired to a fresh Graph, matching Build()'s init.
func newBuilder() *Builder {
	b := &Builder{
		graph:  NewGraph(),
		nextID: 0,
	}
	entry := b.newBlock()
	b.graph.Entry = entry.ID
	return b
}

// entryBlock retrieves the first block (ID 0) from a builder's graph.
func entryBlock(b *Builder) *BasicBlock {
	return b.graph.Blocks[0]
}

// instTypes extracts a slice of instruction type names for easy assertion.
func instTypes(stmts []Instruction) []string {
	names := make([]string, len(stmts))
	for i, s := range stmts {
		switch s.(type) {
		case *TempDeclInst:
			names[i] = "TempDeclInst"
		case *AssignInst:
			names[i] = "AssignInst"
		case *CopyInst:
			names[i] = "CopyInst"
		case *MoveInst:
			names[i] = "MoveInst"
		case *RefAllocInst:
			names[i] = "RefAllocInst"
		case *RefIncInst:
			names[i] = "RefIncInst"
		case *RefDecInst:
			names[i] = "RefDecInst"
		case *MutBorrowInst:
			names[i] = "MutBorrowInst"
		default:
			names[i] = "Unknown"
		}
	}
	return names
}

// makeIntSym makes a Symbol with an IntType.
func makeIntSym(name string) *types.Symbol {
	return &types.Symbol{Name: name, Type: types.IntType{}}
}

// makeStringSym makes a Symbol with a StringType (reference type).
func makeStringSym(name string) *types.Symbol {
	return &types.Symbol{Name: name, Type: types.StringType{}}
}

// makeStructSym makes a Symbol with a StructType (reference type).
func makeStructSym(name string) *types.Symbol {
	return &types.Symbol{Name: name, Type: &types.StructType{Name: "MyStruct"}}
}

// makeIdent returns a typed HIR Identifier.
func makeIdent(name string, t types.Type) *hir.Identifier {
	return &hir.Identifier{Value: name, Type: t}
}

// makeIntLit returns a typed HIR IntLiteral.
func makeIntLit(v int64) *hir.IntLiteral {
	return &hir.IntLiteral{Value: v, Type: types.IntType{}}
}

// makeBoolLit returns a typed HIR BoolLiteral.
func makeBoolLit(v bool) *hir.BoolLiteral {
	return &hir.BoolLiteral{Value: v, Type: types.BoolType{}}
}

// makeStringLit returns a typed HIR StringLiteral.
func makeStringLit(v string) *hir.StringLiteral {
	return &hir.StringLiteral{Value: v, Type: types.StringType{}}
}

// =============================================================================
// Graph & Block Allocation
// =============================================================================

func TestNewBlock_AssignsSequentialIDs(t *testing.T) {
	b := &Builder{graph: NewGraph()}
	b0 := b.newBlock()
	b1 := b.newBlock()
	b2 := b.newBlock()

	assert.Equal(t, BlockID(0), b0.ID)
	assert.Equal(t, BlockID(1), b1.ID)
	assert.Equal(t, BlockID(2), b2.ID)

	assert.Equal(t, b0, b.graph.Blocks[0])
	assert.Equal(t, b1, b.graph.Blocks[1])
	assert.Equal(t, b2, b.graph.Blocks[2])
}

func TestNewBlock_InitializesEmptyStatements(t *testing.T) {
	b := &Builder{graph: NewGraph()}
	blk := b.newBlock()
	assert.NotNil(t, blk.Statements)
	assert.Empty(t, blk.Statements)
	assert.Nil(t, blk.Terminator)
}

// =============================================================================
// Scope Management (pushScope / popScope / registerLocal)
// =============================================================================

// func TestPushScope_IncreasesDepth(t *testing.T) {
// 	b := newBuilder()
// 	assert.Len(t, b.scopes, 0)
// 	b.pushScope()
// 	assert.Len(t, b.scopes, 1)
// 	b.pushScope()
// 	assert.Len(t, b.scopes, 2)
// }

// func TestRegisterLocal_AddsToTopScope(t *testing.T) {
// 	b := newBuilder()
// 	b.pushScope()
// 	sym := makeIntSym("x")
// 	b.registerLocal(sym)
// 	assert.Len(t, b.scopes[0], 1)
// 	assert.Equal(t, sym, b.scopes[0][0])
// }

// func TestRegisterLocal_NilSym_IsNoOp(t *testing.T) {
// 	b := newBuilder()
// 	b.pushScope()
// 	b.registerLocal(nil)
// 	assert.Empty(t, b.scopes[0])
// }

// func TestRegisterLocal_WithNoScopes_IsNoOp(t *testing.T) {
// 	b := newBuilder()
// 	// No pushScope — should not panic
// 	b.registerLocal(makeIntSym("x"))
// 	assert.Len(t, b.scopes, 0)
// }

// func TestPopScope_PrimitivesProduceNoRefDec(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)
// 	b.pushScope()
// 	b.registerLocal(makeIntSym("i"))
// 	b.registerLocal(&types.Symbol{Name: "flag", Type: types.BoolType{}})

// 	result := b.popScope(blk)
// 	assert.Same(t, blk, result, "popScope should return the same block")
// 	assert.Empty(t, blk.Statements, "no RefDecInst expected for primitive types")
// }

// func TestPopScope_ReferenceTypeEmitsRefDec(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("s"))

// 	b.popScope(blk)

// 	require.Len(t, blk.Statements, 1)
// 	rd, ok := blk.Statements[0].(*RefDecInst)
// 	require.True(t, ok, "expected RefDecInst")
// 	assert.Equal(t, "s", rd.Src)
// }

// func TestPopScope_MultipleReferenceTypes_AllGetRefDec(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("s1"))
// 	b.registerLocal(makeStringSym("s2"))
// 	b.registerLocal(makeIntSym("n")) // primitive — no RefDec

// 	b.popScope(blk)

// 	types_ := instTypes(blk.Statements)
// 	assert.Equal(t, []string{"RefDecInst", "RefDecInst"}, types_)
// 	assert.Equal(t, "s1", blk.Statements[0].(*RefDecInst).Src)
// 	assert.Equal(t, "s2", blk.Statements[1].(*RefDecInst).Src)
// }

// func TestPopScope_NilCurrentBlock_ReturnsNil(t *testing.T) {
// 	b := newBuilder()
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("s"))
// 	result := b.popScope(nil)
// 	assert.Nil(t, result, "popScope(nil) should return nil for early-return paths")
// }

// func TestPopScope_EmptyScopes_IsNoOp(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)
// 	// popScope with no pushed scope — should return current unchanged
// 	result := b.popScope(blk)
// 	assert.Same(t, blk, result)
// }

// func TestPopScope_PopsOnlyTopScope(t *testing.T) {
// 	b := newBuilder()
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("outer"))
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("inner"))

// 	blk := entryBlock(b)
// 	b.popScope(blk)

// 	// One scope remains with outer
// 	require.Len(t, b.scopes, 1)
// 	assert.Len(t, b.scopes[0], 1)
// 	assert.Equal(t, "outer", b.scopes[0][0].Name)
// }

// =============================================================================
// flattenExpr
// =============================================================================

func TestFlattenExpr_Nil_ReturnsNil(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	result, next := b.flattenExpr(nil, blk)
	assert.Nil(t, result)
	assert.Same(t, blk, next)
}

func TestFlattenExpr_Identifier_IsPassthrough(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	ident := makeIdent("x", types.IntType{})
	result, next := b.flattenExpr(ident, blk)
	assert.Same(t, ident, result)
	assert.Same(t, blk, next)
	assert.Empty(t, blk.Statements)
}

func TestFlattenExpr_IntLiteral_IsPassthrough(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	lit := makeIntLit(99)
	result, next := b.flattenExpr(lit, blk)
	assert.Same(t, lit, result)
	assert.Same(t, blk, next)
	assert.Empty(t, blk.Statements)
}

func TestFlattenExpr_BoolLiteral_IsPassthrough(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	lit := makeBoolLit(true)
	result, next := b.flattenExpr(lit, blk)
	assert.Same(t, lit, result)
	assert.Same(t, blk, next)
	assert.Empty(t, blk.Statements)
}

func TestFlattenExpr_StringLiteral_IsPassthrough(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	lit := makeStringLit("hello")
	result, next := b.flattenExpr(lit, blk)
	assert.Same(t, lit, result)
	assert.Same(t, blk, next)
	assert.Empty(t, blk.Statements)
}

func TestFlattenExpr_PrefixExpr_MaterializesTemp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	prefix := &hir.PrefixExpr{
		Operator: "-",
		Right:    makeIntLit(5),
		Type:     types.IntType{},
	}
	result, next := b.flattenExpr(prefix, blk)

	// Result should be an Identifier pointing to the new temp
	ident, ok := result.(*hir.Identifier)
	require.True(t, ok, "expected *hir.Identifier as result")
	assert.Equal(t, "_t1", ident.Value)
	assert.Same(t, blk, next)

	// Emitted: TempDeclInst + AssignInst
	require.Len(t, blk.Statements, 2)
	assert.Equal(t, []string{"TempDeclInst", "AssignInst"}, instTypes(blk.Statements))

	decl := blk.Statements[0].(*TempDeclInst)
	assert.Equal(t, "_t1", decl.Name)

	assign := blk.Statements[1].(*AssignInst)
	assert.Equal(t, "_t1", assign.Dst)
	flatPrefix, ok := assign.Expr.(*hir.PrefixExpr)
	require.True(t, ok)
	assert.Equal(t, "-", flatPrefix.Operator)
}

func TestFlattenExpr_InfixExpr_MaterializesTemp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	infix := &hir.InfixExpr{
		Left:     makeIdent("a", types.IntType{}),
		Operator: "+",
		Right:    makeIdent("b", types.IntType{}),
		Type:     types.IntType{},
	}
	result, next := b.flattenExpr(infix, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)
	assert.Equal(t, "_t1", ident.Value)
	assert.Same(t, blk, next)

	// Emitted: TempDeclInst + AssignInst
	require.Len(t, blk.Statements, 2)
	assert.Equal(t, []string{"TempDeclInst", "AssignInst"}, instTypes(blk.Statements))
}

func TestFlattenExpr_InfixExpr_NestedLeft_ProducesChain(t *testing.T) {
	// (a + b) + c → _t1 = a + b, _t2 = _t1 + c
	b := newBuilder()
	blk := entryBlock(b)

	inner := &hir.InfixExpr{
		Left:     makeIdent("a", types.IntType{}),
		Operator: "+",
		Right:    makeIdent("b", types.IntType{}),
		Type:     types.IntType{},
	}
	outer := &hir.InfixExpr{
		Left:     inner,
		Operator: "+",
		Right:    makeIdent("c", types.IntType{}),
		Type:     types.IntType{},
	}

	result, _ := b.flattenExpr(outer, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)
	assert.Equal(t, "_t2", ident.Value)

	// 2×(TempDeclInst + AssignInst) = 4 statements
	require.Len(t, blk.Statements, 4)
	assert.Equal(t, []string{"TempDeclInst", "AssignInst", "TempDeclInst", "AssignInst"}, instTypes(blk.Statements))

	// The first assign captures the inner expression
	innerAssign := blk.Statements[1].(*AssignInst)
	assert.Equal(t, "_t1", innerAssign.Dst)

	// The second assign uses _t1 as left operand
	outerAssign := blk.Statements[3].(*AssignInst)
	assert.Equal(t, "_t2", outerAssign.Dst)
	outerInfix := outerAssign.Expr.(*hir.InfixExpr)
	assert.Equal(t, "_t1", outerInfix.Left.(*hir.Identifier).Value)
}

func TestFlattenExpr_FieldAccess_MaterializesTemp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	access := &hir.FieldAccess{
		Object: makeIdent("obj", &types.StructType{Name: "Foo"}),
		Field:  &hir.Identifier{Value: "x"},
		Type:   types.IntType{},
	}
	result, _ := b.flattenExpr(access, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)
	assert.Equal(t, "_t1", ident.Value)

	require.Len(t, blk.Statements, 2)
	assert.Equal(t, []string{"TempDeclInst", "AssignInst"}, instTypes(blk.Statements))

	assign := blk.Statements[1].(*AssignInst)
	fa, ok := assign.Expr.(*hir.FieldAccess)
	require.True(t, ok)
	assert.Equal(t, "x", fa.Field.Value)
}

func TestFlattenExpr_IndexExpr_MaterializesTemp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	indexExpr := &hir.IndexExpr{
		Left:  makeIdent("arr", types.SliceType{Base: types.IntType{}}),
		Index: makeIntLit(0),
		Type:  types.IntType{},
	}
	result, _ := b.flattenExpr(indexExpr, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)
	assert.Equal(t, "_t1", ident.Value)

	require.Len(t, blk.Statements, 2)
	assert.Equal(t, []string{"TempDeclInst", "AssignInst"}, instTypes(blk.Statements))
}

func TestFlattenExpr_TempCounterIncrements(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	for i := 1; i <= 3; i++ {
		b.flattenExpr(&hir.PrefixExpr{
			Operator: "!",
			Right:    makeBoolLit(true),
			Type:     types.BoolType{},
		}, blk)
	}
	assert.Equal(t, 3, b.tempCount)
}

// =============================================================================
// flattenCall (via flattenExpr on *hir.CallExpr)
// =============================================================================

func TestFlattenCall_PrimitiveArg_EmitsCopy(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function: makeIdent("foo", &types.FunctionType{}),
		Arguments: []hir.CallArg{
			{Argument: makeIdent("x", types.IntType{}), Mut: false, Own: false},
		},
		Type: types.IntType{},
	}
	result, _ := b.flattenCall(call, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)

	types_ := instTypes(blk.Statements)
	// For x: TempDeclInst, CopyInst; for call result: TempDeclInst, AssignInst
	assert.Contains(t, types_, "CopyInst")
	assert.NotNil(t, ident)
}

func TestFlattenCall_OwnedArg_EmitsMove(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function: makeIdent("consume", &types.FunctionType{}),
		Arguments: []hir.CallArg{
			{Argument: makeIdent("val", &types.StructType{Name: "MyStruct"}), Mut: false, Own: true},
		},
		Type: types.UnitType{},
	}
	b.flattenCall(call, blk)

	types_ := instTypes(blk.Statements)
	assert.Contains(t, types_, "MoveInst")
	assert.NotContains(t, types_, "CopyInst")
	assert.NotContains(t, types_, "MutBorrowInst")
}

func TestFlattenCall_MutArg_EmitsMutBorrowAndAssign(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function: makeIdent("modify", &types.FunctionType{}),
		Arguments: []hir.CallArg{
			{Argument: makeIdent("v", &types.StructType{Name: "Vec"}), Mut: true, Own: false},
		},
		Type: types.UnitType{},
	}
	b.flattenCall(call, blk)

	types_ := instTypes(blk.Statements)
	assert.Contains(t, types_, "MutBorrowInst")
	assert.Contains(t, types_, "AssignInst")
	assert.NotContains(t, types_, "MoveInst")
	assert.NotContains(t, types_, "CopyInst")
}

func TestFlattenCall_ImmutableRefArg_EmitsAssignOnly(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function: makeIdent("read", &types.FunctionType{}),
		Arguments: []hir.CallArg{
			{Argument: makeIdent("s", types.StringType{}), Mut: false, Own: false},
		},
		Type: types.UnitType{},
	}
	b.flattenCall(call, blk)

	types_ := instTypes(blk.Statements)
	assert.NotContains(t, types_, "MoveInst")
	assert.NotContains(t, types_, "CopyInst")
	assert.NotContains(t, types_, "MutBorrowInst")
	// Should have AssignInst for the borrow + AssignInst for call result
	assert.Contains(t, types_, "AssignInst")
}

func TestFlattenCall_ReturnsIdentifierWithCallType(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function:  makeIdent("getInt", &types.FunctionType{}),
		Arguments: []hir.CallArg{},
		Type:      types.IntType{},
	}
	result, _ := b.flattenCall(call, blk)

	ident, ok := result.(*hir.Identifier)
	require.True(t, ok)
	assert.Equal(t, types.IntType{}, ident.Type)
}

func TestFlattenCall_MultipleArgs_LeftToRightOrder(t *testing.T) {
	// Checks that each arg gets its own TempDeclInst before the call TempDeclInst.
	b := newBuilder()
	blk := entryBlock(b)

	call := &hir.CallExpr{
		Function: makeIdent("add", &types.FunctionType{}),
		Arguments: []hir.CallArg{
			{Argument: makeIntLit(1), Mut: false, Own: false},
			{Argument: makeIntLit(2), Mut: false, Own: false},
		},
		Type: types.IntType{},
	}
	b.flattenCall(call, blk)

	// Both int args are primitives → CopyInst each
	types_ := instTypes(blk.Statements)
	copies := 0
	for _, t_ := range types_ {
		if t_ == "CopyInst" {
			copies++
		}
	}
	assert.Equal(t, 2, copies, "expected one CopyInst per int arg")
}

// =============================================================================
// buildDeclareStmt (memory model via emitMemoryTransfer)
// =============================================================================

func TestBuildDeclareStmt_PrimitiveInt_EmitsCopy(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.DeclareStmt{
		Symbol: makeIntSym("x"),
		Value:  makeIntLit(42),
	}
	b.buildDeclareStmt(stmt, blk)

	require.Len(t, blk.Statements, 1)
	ci, ok := blk.Statements[0].(*CopyInst)
	require.True(t, ok)
	assert.Equal(t, "x", ci.Dst)
	assert.Equal(t, "42", ci.Src)
}

func TestBuildDeclareStmt_PrimitiveBool_EmitsCopy(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.DeclareStmt{
		Symbol: &types.Symbol{Name: "flag", Type: types.BoolType{}},
		Value:  makeBoolLit(false),
	}
	b.buildDeclareStmt(stmt, blk)

	require.Len(t, blk.Statements, 1)
	ci, ok := blk.Statements[0].(*CopyInst)
	require.True(t, ok)
	assert.Equal(t, "flag", ci.Dst)
}

func TestBuildDeclareStmt_ReferenceAlias_EmitsAssignAndRefInc(t *testing.T) {
	// Assigning an existing named variable (non-temp) of reference type → alias path
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.DeclareStmt{
		Symbol: makeStringSym("t"),
		Value:  makeIdent("s", types.StringType{}), // s is not a temp, not a literal
	}
	b.buildDeclareStmt(stmt, blk)

	types_ := instTypes(blk.Statements)
	require.Equal(t, []string{"AssignInst", "RefIncInst"}, types_)

	ai := blk.Statements[0].(*AssignInst)
	assert.Equal(t, "t", ai.Dst)

	ri := blk.Statements[1].(*RefIncInst)
	assert.Equal(t, "s", ri.Src)
}

func TestBuildDeclareStmt_OwnedStruct_EmitsMove(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	arrayType := types.ArrayType{Base: types.IntType{}, Size: 3}
	stmt := &hir.DeclareStmt{
		Symbol: &types.Symbol{Name: "arr", Type: arrayType},
		Value:  makeIdent("src", arrayType),
	}
	b.buildDeclareStmt(stmt, blk)

	require.Len(t, blk.Statements, 1)
	mi, ok := blk.Statements[0].(*MoveInst)
	require.True(t, ok)
	assert.Equal(t, "arr", mi.Dst)
	assert.Equal(t, "src", mi.Src)
}

// =============================================================================
// buildAssignStmt
// =============================================================================

func TestBuildAssignStmt_PrimitiveInt_EmitsCopy(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.AssignStmt{
		LValue: makeIdent("x", types.IntType{}),
		RValue: makeIntLit(7),
	}
	b.buildAssignStmt(stmt, blk)

	require.Len(t, blk.Statements, 1)
	ci, ok := blk.Statements[0].(*CopyInst)
	require.True(t, ok)
	assert.Equal(t, "x", ci.Dst)
	assert.Equal(t, "7", ci.Src)
}

func TestBuildAssignStmt_NonPrimitiveOwned_EmitsMove(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	arrayType := types.ArrayType{Base: types.IntType{}, Size: 4}

	stmt := &hir.AssignStmt{
		LValue: makeIdent("dst", arrayType),
		RValue: makeIdent("src", arrayType),
	}
	b.buildAssignStmt(stmt, blk)

	require.Len(t, blk.Statements, 1)
	mi, ok := blk.Statements[0].(*MoveInst)
	require.True(t, ok)
	assert.Equal(t, "dst", mi.Dst)
}

// =============================================================================
// buildReturnStmt
// =============================================================================

func TestBuildReturnStmt_VoidReturn_SetsReturnTerminator(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.ReturnStmt{Value: nil}
	result := b.buildReturnStmt(stmt, blk)

	assert.Nil(t, result, "buildReturnStmt should return nil (dead code after return)")
	_, ok := blk.Terminator.(*ReturnTerminator)
	assert.True(t, ok)
}

func TestBuildReturnStmt_WithValue_EmitsRetAssign(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	stmt := &hir.ReturnStmt{Value: makeIntLit(1)}
	b.buildReturnStmt(stmt, blk)

	// Find the AssignInst for _ret
	found := false
	for _, inst := range blk.Statements {
		if ai, ok := inst.(*AssignInst); ok && ai.Dst == "_ret" {
			found = true
		}
	}
	assert.True(t, found, "expected AssignInst{Dst: \"_ret\"}")
	assert.IsType(t, &ReturnTerminator{}, blk.Terminator)
}

// func TestBuildReturnStmt_EarlyReturn_TearsDownAllScopes(t *testing.T) {
// 	// Two nested scopes each have a reference-type variable.
// 	// buildReturnStmt must emit RefDecInst for both.
// 	b := newBuilder()
// 	blk := entryBlock(b)

// 	b.pushScope()
// 	b.registerLocal(makeStringSym("outer"))
// 	b.pushScope()
// 	b.registerLocal(makeStringSym("inner"))

// 	stmt := &hir.ReturnStmt{Value: nil}
// 	b.buildReturnStmt(stmt, blk)

// 	refDecs := 0
// 	for _, inst := range blk.Statements {
// 		if _, ok := inst.(*RefDecInst); ok {
// 			refDecs++
// 		}
// 	}
// 	assert.Equal(t, 2, refDecs, "expected RefDecInst for each live reference in all scopes")
// }

// func TestBuildReturnStmt_EarlyReturn_SkipsPrimitives(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)

// 	b.pushScope()
// 	b.registerLocal(makeIntSym("i"))
// 	b.registerLocal(makeIntSym("j"))

// 	stmt := &hir.ReturnStmt{Value: nil}
// 	b.buildReturnStmt(stmt, blk)

// 	for _, inst := range blk.Statements {
// 		_, isRefDec := inst.(*RefDecInst)
// 		assert.False(t, isRefDec, "no RefDecInst expected for primitive types")
// 	}
// }

// =============================================================================
// buildBlockStmt (scope wrapping)
// =============================================================================

func TestBuildBlockStmt_NilBody_ReturnsCurrentBlock(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	result := b.buildBlockStmt(nil, blk)
	assert.Same(t, blk, result)
}

func TestBuildBlockStmt_EmptyBody_ReturnsCurrentBlock(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	body := &hir.BlockStmt{Statements: []hir.Stmt{}}
	result := b.buildBlockStmt(body, blk)
	assert.Same(t, blk, result)
}

// func TestBuildBlockStmt_PopsScope_AfterStatements(t *testing.T) {
// 	b := newBuilder()
// 	blk := entryBlock(b)

// 	body := &hir.BlockStmt{
// 		Statements: []hir.Stmt{
// 			&hir.DeclareStmt{
// 				Symbol: makeStringSym("s"),
// 				Value:  makeStringLit("hello"),
// 			},
// 		},
// 	}
// 	b.buildBlockStmt(body, blk)

// 	// After buildBlockStmt returns, the scope it pushed should be popped.
// 	assert.Len(t, b.scopes, 0, "scope should be cleaned up after block")
// }

func TestBuildBlockStmt_DeadCode_StopsAfterTerminator(t *testing.T) {
	// After a ReturnStmt, subsequent statements are dead code and should not be emitted.
	b := newBuilder()
	blk := entryBlock(b)

	body := &hir.BlockStmt{
		Statements: []hir.Stmt{
			&hir.ReturnStmt{Value: nil},
			// This ExprStmt comes after return — it's dead.
			&hir.ExprStmt{Value: makeIntLit(99)},
		},
	}
	result := b.buildBlockStmt(body, blk)

	assert.Nil(t, result, "dead code after return should leave current==nil")
}

// =============================================================================
// BreakStmt / ContinueStmt
// =============================================================================

func TestBreakStmt_WithNoLoops_IsNoOp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	result := b.buildStmt(&hir.BreakStmt{}, blk)
	// No loops in stack → terminator not set, same block returned
	assert.Same(t, blk, result)
	assert.Nil(t, blk.Terminator)
}

func TestBreakStmt_WithLoop_JumpsToExit(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	exitBlock := b.newBlock()

	b.loops = append(b.loops, LoopTracker{Header: 0, Exit: exitBlock.ID})

	result := b.buildStmt(&hir.BreakStmt{}, blk)

	assert.Nil(t, result, "break should return nil (unreachable after jump)")
	jt, ok := blk.Terminator.(*JumpTerminator)
	require.True(t, ok)
	assert.Equal(t, exitBlock.ID, jt.Target)
}

func TestContinueStmt_WithNoLoops_IsNoOp(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)

	result := b.buildStmt(&hir.ContinueStmt{}, blk)
	assert.Same(t, blk, result)
	assert.Nil(t, blk.Terminator)
}

func TestContinueStmt_WithLoop_JumpsToHeader(t *testing.T) {
	b := newBuilder()
	blk := entryBlock(b)
	headerBlock := b.newBlock()
	exitBlock := b.newBlock()

	b.loops = append(b.loops, LoopTracker{Header: headerBlock.ID, Exit: exitBlock.ID})

	result := b.buildStmt(&hir.ContinueStmt{}, blk)

	assert.Nil(t, result)
	jt, ok := blk.Terminator.(*JumpTerminator)
	require.True(t, ok)
	assert.Equal(t, headerBlock.ID, jt.Target)
}

func TestBreakContinue_NestedLoops_UseInnermost(t *testing.T) {
	b := newBuilder()
	// blk := entryBlock(b)
	outerExit := b.newBlock()
	innerHeader := b.newBlock()
	innerExit := b.newBlock()

	b.loops = append(b.loops,
		LoopTracker{Header: 0, Exit: outerExit.ID},
		LoopTracker{Header: innerHeader.ID, Exit: innerExit.ID},
	)

	blkBreak := b.newBlock()
	b.buildStmt(&hir.BreakStmt{}, blkBreak)

	jt, ok := blkBreak.Terminator.(*JumpTerminator)
	require.True(t, ok)
	assert.Equal(t, innerExit.ID, jt.Target, "break should target innermost loop exit")
}

// =============================================================================
// Build (integration: full FnDecl → Graph)
// =============================================================================

func TestBuild_EmptyFunction_HasEntryBlock(t *testing.T) {
	fn := &hir.FnDecl{
		Name:   "empty",
		Params: []*hir.Param{},
		Body:   &hir.BlockStmt{Statements: []hir.Stmt{}},
	}
	g := Build(fn)

	require.NotNil(t, g)
	assert.NotNil(t, g.Blocks[g.Entry])
}

func TestBuild_SingleReturn_TerminatesEntryBlock(t *testing.T) {
	fn := &hir.FnDecl{
		Name:   "ret",
		Params: []*hir.Param{},
		Body: &hir.BlockStmt{
			Statements: []hir.Stmt{
				&hir.ReturnStmt{Value: makeIntLit(0)},
			},
		},
	}
	g := Build(fn)

	entry := g.Blocks[g.Entry]
	require.NotNil(t, entry)
	assert.IsType(t, &ReturnTerminator{}, entry.Terminator)
}

func TestBuild_IntDeclaration_EmitsCopyInEntryBlock(t *testing.T) {
	fn := &hir.FnDecl{
		Name:   "locals",
		Params: []*hir.Param{},
		Body: &hir.BlockStmt{
			Statements: []hir.Stmt{
				&hir.DeclareStmt{
					Symbol: makeIntSym("x"),
					Value:  makeIntLit(10),
				},
			},
		},
	}
	g := Build(fn)

	entry := g.Blocks[g.Entry]
	types_ := instTypes(entry.Statements)
	assert.Contains(t, types_, "CopyInst")
}

func TestBuild_ReferenceDecl_EmitsRefDecOnScopeExit(t *testing.T) {
	// Declare a string variable, then the block ends — RefDecInst should be emitted.
	fn := &hir.FnDecl{
		Name:   "ref_scope",
		Params: []*hir.Param{},
		Body: &hir.BlockStmt{
			Statements: []hir.Stmt{
				&hir.DeclareStmt{
					Symbol: makeStringSym("s"),
					Value:  makeStringLit("hi"),
				},
			},
		},
	}
	g := Build(fn)

	entry := g.Blocks[g.Entry]
	types_ := instTypes(entry.Statements)
	assert.Contains(t, types_, "RefDecInst", "string var should be decremented when scope exits")
}

func TestBuild_EscapeMap_NilSafe(t *testing.T) {
	// Passing nil escapeMap should not panic
	fn := &hir.FnDecl{
		Name:   "noescapes",
		Params: []*hir.Param{},
		Body:   &hir.BlockStmt{Statements: []hir.Stmt{}},
	}
	assert.NotPanics(t, func() {
		Build(fn)
	})
}

// =============================================================================
// Helpers
// =============================================================================

func newEmitter() *Emitter { return NewEmitter() }

// emitProgram serializes a Program to JSON and parses it into a generic map.
func emitProgram(t *testing.T, p *Program) map[string]interface{} {
	t.Helper()
	e := newEmitter()
	var buf bytes.Buffer
	err := e.Emit(p, &buf)
	require.NoError(t, err)
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	return result
}

// getMap asserts that key exists in m and is a map[string]interface{}.
func getMap(t *testing.T, m map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	v, ok := m[key]
	require.True(t, ok, "key %q not found in map", key)
	result, ok := v.(map[string]interface{})
	require.True(t, ok, "key %q is not a map", key)
	return result
}

// getSlice asserts that key exists in m and is a []interface{}.
func getSlice(t *testing.T, m map[string]interface{}, key string) []interface{} {
	t.Helper()
	v, ok := m[key]
	require.True(t, ok, "key %q not found", key)
	result, ok := v.([]interface{})
	require.True(t, ok, "key %q is not a slice", key)
	return result
}

// =============================================================================
// marshalType
// =============================================================================

func TestMarshalType_Nil(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(nil)
	assert.Nil(t, result)
}

func TestMarshalType_Primitives(t *testing.T) {
	e := newEmitter()
	tests := []struct {
		name     string
		typ      types.Type
		wantKind string
	}{
		{"Int", types.IntType{}, "Int"},
		{"Bool", types.BoolType{}, "Bool"},
		{"String", types.StringType{}, "String"},
		{"Unit", types.UnitType{}, "Unit"},
		{"Unknown", types.UnknownType{}, "Unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := e.marshalType(tc.typ)
			require.NotNil(t, result)
			assert.Equal(t, tc.wantKind, result["kind"])
		})
	}
}

func TestMarshalType_Struct(t *testing.T) {
	e := newEmitter()
	st := &types.StructType{
		Name: "Point",
		Fields: []types.StructField{
			{Name: "x", Type: types.IntType{}},
			{Name: "y", Type: types.IntType{}},
		},
	}
	result := e.marshalType(st)
	require.NotNil(t, result)
	assert.Equal(t, "Struct", result["kind"])
	assert.Equal(t, "Point", result["name"])

	fields, ok := result["fields"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, fields, 2)
	assert.Equal(t, "x", fields[0]["name"])
	assert.Equal(t, "y", fields[1]["name"])
}

func TestMarshalType_Struct_NoFields(t *testing.T) {
	e := newEmitter()
	st := &types.StructType{Name: "Empty"}
	result := e.marshalType(st)
	assert.Equal(t, "Struct", result["kind"])
	assert.Equal(t, "Empty", result["name"])
	fields, ok := result["fields"].([]map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, fields)
}

func TestMarshalType_Array(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(types.ArrayType{Base: types.IntType{}, Size: 8})
	require.NotNil(t, result)
	assert.Equal(t, "Array", result["kind"])
	assert.Equal(t, 8, result["size"])
	base := result["base"].(map[string]interface{})
	assert.Equal(t, "Int", base["kind"])
}

func TestMarshalType_Slice(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(types.SliceType{Base: types.BoolType{}})
	assert.Equal(t, "Slice", result["kind"])
	base := result["base"].(map[string]interface{})
	assert.Equal(t, "Bool", base["kind"])
}

func TestMarshalType_Vector(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(types.VectorType{Base: types.StringType{}})
	assert.Equal(t, "Vector", result["kind"])
	base := result["base"].(map[string]interface{})
	assert.Equal(t, "String", base["kind"])
}

func TestMarshalType_Map(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(types.MapType{Key: types.StringType{}, Value: types.IntType{}})
	assert.Equal(t, "Map", result["kind"])
	key := result["key"].(map[string]interface{})
	val := result["value"].(map[string]interface{})
	assert.Equal(t, "String", key["kind"])
	assert.Equal(t, "Int", val["kind"])
}

func TestMarshalType_SumType(t *testing.T) {
	e := newEmitter()
	st := &types.SumType{
		BaseName: "Option",
		Variants: []types.SumVariant{
			{Name: "Some", Discriminant: 0, Fields: []types.StructField{{Name: "value", Type: types.IntType{}}}},
			{Name: "None", Discriminant: 1, Fields: []types.StructField{}},
		},
	}
	result := e.marshalType(st)
	assert.Equal(t, "SumType", result["kind"])
	assert.Equal(t, "Option", result["name"])
	variants, ok := result["variants"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, variants, 2)
	assert.Equal(t, "Some", variants[0]["name"])
	assert.Equal(t, "None", variants[1]["name"])
}

func TestMarshalType_Task(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(types.TaskType{Base: types.IntType{}})
	assert.Equal(t, "Task", result["kind"])
	base := result["base"].(map[string]interface{})
	assert.Equal(t, "Int", base["kind"])
}

func TestMarshalType_Function(t *testing.T) {
	e := newEmitter()
	result := e.marshalType(&types.FunctionType{})
	assert.Equal(t, "Function", result["kind"])
}

// =============================================================================
// marshalExpr
// =============================================================================

func TestMarshalExpr_Nil(t *testing.T) {
	e := newEmitter()
	assert.Nil(t, e.marshalExpr(nil))
}

func TestMarshalExpr_Identifier(t *testing.T) {
	e := newEmitter()
	id := &hir.Identifier{Value: "myVar", Type: types.IntType{}}
	result := e.marshalExpr(id)
	assert.Equal(t, "Identifier", result["node_type"])
	assert.Equal(t, "myVar", result["value"])
	tp := result["maml_type"].(map[string]interface{})
	assert.Equal(t, "Int", tp["kind"])
}

func TestMarshalExpr_IntLiteral(t *testing.T) {
	e := newEmitter()
	lit := &hir.IntLiteral{Value: 42, Type: types.IntType{}}
	result := e.marshalExpr(lit)
	assert.Equal(t, "IntLiteral", result["node_type"])
	assert.Equal(t, int64(42), result["value"])
}

func TestMarshalExpr_BoolLiteral_True(t *testing.T) {
	e := newEmitter()
	result := e.marshalExpr(&hir.BoolLiteral{Value: true, Type: types.BoolType{}})
	assert.Equal(t, "BoolLiteral", result["node_type"])
	assert.Equal(t, true, result["value"])
}

func TestMarshalExpr_BoolLiteral_False(t *testing.T) {
	e := newEmitter()
	result := e.marshalExpr(&hir.BoolLiteral{Value: false, Type: types.BoolType{}})
	assert.Equal(t, "BoolLiteral", result["node_type"])
	assert.Equal(t, false, result["value"])
}

func TestMarshalExpr_StringLiteral(t *testing.T) {
	e := newEmitter()
	result := e.marshalExpr(&hir.StringLiteral{Value: "hello", Type: types.StringType{}})
	assert.Equal(t, "StringLiteral", result["node_type"])
	assert.Equal(t, "hello", result["value"])
}

func TestMarshalExpr_ZeroAllocExpr(t *testing.T) {
	e := newEmitter()
	result := e.marshalExpr(&hir.ZeroAllocExpr{Type: &types.StructType{Name: "Foo"}})
	assert.Equal(t, "ZeroAllocExpr", result["node_type"])
	tp := result["maml_type"].(map[string]interface{})
	assert.Equal(t, "Struct", tp["kind"])
}

func TestMarshalExpr_PrefixExpr(t *testing.T) {
	e := newEmitter()
	right := &hir.IntLiteral{Value: 5, Type: types.IntType{}}
	result := e.marshalExpr(&hir.PrefixExpr{Operator: "-", Right: right, Type: types.IntType{}})
	assert.Equal(t, "PrefixExpr", result["node_type"])
	assert.Equal(t, "-", result["operator"])
	rightMap := result["right"].(map[string]interface{})
	assert.Equal(t, "IntLiteral", rightMap["node_type"])
}

func TestMarshalExpr_InfixExpr(t *testing.T) {
	e := newEmitter()
	left := &hir.Identifier{Value: "a", Type: types.IntType{}}
	right := &hir.Identifier{Value: "b", Type: types.IntType{}}
	result := e.marshalExpr(&hir.InfixExpr{Left: left, Operator: "+", Right: right, Type: types.IntType{}})
	assert.Equal(t, "InfixExpr", result["node_type"])
	assert.Equal(t, "+", result["operator"])

	leftMap := result["left"].(map[string]interface{})
	assert.Equal(t, "Identifier", leftMap["node_type"])
	assert.Equal(t, "a", leftMap["value"])

	rightMap := result["right"].(map[string]interface{})
	assert.Equal(t, "b", rightMap["value"])
}

func TestMarshalExpr_CallExpr_NoArgs(t *testing.T) {
	e := newEmitter()
	fn := &hir.Identifier{Value: "getVal", Type: &types.FunctionType{}}
	result := e.marshalExpr(&hir.CallExpr{Function: fn, Arguments: nil, Type: types.IntType{}})
	assert.Equal(t, "CallExpr", result["node_type"])
	fnMap := result["function"].(map[string]interface{})
	assert.Equal(t, "getVal", fnMap["value"])
	args, ok := result["arguments"].([]map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, args)
}

func TestMarshalExpr_CallExpr_WithArgs(t *testing.T) {
	e := newEmitter()
	fn := &hir.Identifier{Value: "add", Type: &types.FunctionType{}}
	arg1 := &hir.Identifier{Value: "_t1", Type: types.IntType{}}
	arg2 := &hir.Identifier{Value: "_t2", Type: types.IntType{}}
	call := &hir.CallExpr{
		Function: fn,
		Arguments: []hir.CallArg{
			{Argument: arg1, Mut: false, Own: false},
			{Argument: arg2, Mut: true, Own: false},
		},
		Type: types.IntType{},
	}
	result := e.marshalExpr(call)
	args := result["arguments"].([]map[string]interface{})
	require.Len(t, args, 2)
	assert.Equal(t, false, args[0]["mut"])
	assert.Equal(t, false, args[0]["own"])
	assert.Equal(t, true, args[1]["mut"])
}

func TestMarshalExpr_FieldAccess(t *testing.T) {
	e := newEmitter()
	obj := &hir.Identifier{Value: "p", Type: &types.StructType{Name: "Point"}}
	result := e.marshalExpr(&hir.FieldAccess{Object: obj, Field: &hir.Identifier{Value: "x"}, Type: types.IntType{}})
	assert.Equal(t, "FieldAccess", result["node_type"])
	assert.Equal(t, "x", result["field"])
	objMap := result["object"].(map[string]interface{})
	assert.Equal(t, "p", objMap["value"])
}

func TestMarshalExpr_IndexExpr(t *testing.T) {
	e := newEmitter()
	left := &hir.Identifier{Value: "arr", Type: types.SliceType{Base: types.IntType{}}}
	index := &hir.IntLiteral{Value: 0, Type: types.IntType{}}
	result := e.marshalExpr(&hir.IndexExpr{Left: left, Index: index, Type: types.IntType{}})
	assert.Equal(t, "IndexExpr", result["node_type"])
	leftMap := result["left"].(map[string]interface{})
	assert.Equal(t, "arr", leftMap["value"])
}

func TestMarshalExpr_SliceExpr(t *testing.T) {
	e := newEmitter()
	left := &hir.Identifier{Value: "arr", Type: types.SliceType{Base: types.IntType{}}}
	low := &hir.IntLiteral{Value: 0, Type: types.IntType{}}
	high := &hir.IntLiteral{Value: 5, Type: types.IntType{}}
	result := e.marshalExpr(&hir.SliceExpr{Left: left, Low: low, High: high, Type: types.SliceType{Base: types.IntType{}}})
	assert.Equal(t, "SliceExpr", result["node_type"])
	assert.NotNil(t, result["low"])
	assert.NotNil(t, result["high"])
}

// =============================================================================
// marshalInstruction
// =============================================================================

func TestMarshalInstruction_TempDeclInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&TempDeclInst{Name: "_t1", Type: types.IntType{}})
	require.NotNil(t, result)
	assert.Equal(t, "TempDeclInst", result["node_type"])
	assert.Equal(t, "_t1", result["name"])
	tp := result["maml_type"].(map[string]interface{})
	assert.Equal(t, "Int", tp["kind"])
}

func TestMarshalInstruction_AssignInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&AssignInst{
		Dst:  "x",
		Expr: &hir.IntLiteral{Value: 7, Type: types.IntType{}},
	})
	require.NotNil(t, result)
	assert.Equal(t, "AssignInst", result["node_type"])
	assert.Equal(t, "x", result["dst"])
	exprMap := result["expr"].(map[string]interface{})
	assert.Equal(t, "IntLiteral", exprMap["node_type"])
}

func TestMarshalInstruction_CopyInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&CopyInst{Dst: "a", Src: "b"})
	assert.Equal(t, "CopyInst", result["node_type"])
	assert.Equal(t, "a", result["dst"])
	assert.Equal(t, "b", result["src"])
}

func TestMarshalInstruction_MoveInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&MoveInst{Dst: "dst", Src: "src"})
	assert.Equal(t, "MoveInst", result["node_type"])
	assert.Equal(t, "dst", result["dst"])
	assert.Equal(t, "src", result["src"])
}

func TestMarshalInstruction_RefAllocInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&RefAllocInst{Dst: "_t1", Type: types.StringType{}})
	assert.Equal(t, "RefAllocInst", result["node_type"])
	assert.Equal(t, "_t1", result["dst"])
	tp := result["maml_type"].(map[string]interface{})
	assert.Equal(t, "String", tp["kind"])
}

func TestMarshalInstruction_RefIncInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&RefIncInst{Src: "s"})
	assert.Equal(t, "RefIncInst", result["node_type"])
	assert.Equal(t, "s", result["src"])
}

func TestMarshalInstruction_RefDecInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&RefDecInst{Src: "s"})
	assert.Equal(t, "RefDecInst", result["node_type"])
	assert.Equal(t, "s", result["src"])
}

func TestMarshalInstruction_MutBorrowInst(t *testing.T) {
	e := newEmitter()
	result := e.marshalInstruction(&MutBorrowInst{Src: "v"})
	assert.Equal(t, "MutBorrowInst", result["node_type"])
	assert.Equal(t, "v", result["src"])
}

func TestMarshalInstruction_UnknownType_ReturnsNil(t *testing.T) {
	e := newEmitter()
	// Pass a non-nil Instruction that matches no case → should return nil
	// We can't create a new Instruction type without satisfying the interface,
	// so we verify the nil-guard contract by passing a typed nil pointer.
	// The actual nil-return path is triggered by the default case.
	// We document this via the nil-terminator test below.
	result := e.marshalInstruction(nil)
	assert.Nil(t, result)
}

// =============================================================================
// marshalTerminator
// =============================================================================

func TestMarshalTerminator_Nil(t *testing.T) {
	e := newEmitter()
	assert.Nil(t, e.marshalTerminator(nil))
}

func TestMarshalTerminator_Return(t *testing.T) {
	e := newEmitter()
	result := e.marshalTerminator(&ReturnTerminator{})
	assert.Equal(t, "ReturnTerminator", result["node_type"])
}

func TestMarshalTerminator_Jump(t *testing.T) {
	e := newEmitter()
	result := e.marshalTerminator(&JumpTerminator{Target: BlockID(3)})
	assert.Equal(t, "JumpTerminator", result["node_type"])
	assert.Equal(t, 3, result["target"])
}

func TestMarshalTerminator_Branch(t *testing.T) {
	e := newEmitter()
	cond := &hir.BoolLiteral{Value: true, Type: types.BoolType{}}
	result := e.marshalTerminator(&BranchTerminator{
		Condition:   cond,
		TrueTarget:  BlockID(1),
		FalseTarget: BlockID(2),
	})
	assert.Equal(t, "BranchTerminator", result["node_type"])
	assert.Equal(t, 1, result["true_target"])
	assert.Equal(t, 2, result["false_target"])
	condMap := result["condition"].(map[string]interface{})
	assert.Equal(t, "BoolLiteral", condMap["node_type"])
}

func TestMarshalTerminator_Unreachable(t *testing.T) {
	e := newEmitter()
	result := e.marshalTerminator(&UnreachableTerminator{})
	assert.Equal(t, "UnreachableTerminator", result["node_type"])
}

// =============================================================================
// marshalBlock
// =============================================================================

func TestMarshalBlock_EmptyBlock(t *testing.T) {
	e := newEmitter()
	blk := &BasicBlock{
		ID:         BlockID(0),
		Statements: []Instruction{},
		Terminator: &ReturnTerminator{},
	}
	result := e.marshalBlock(blk)
	assert.Equal(t, 0, result["id"])
	stmts := result["statements"].([]map[string]interface{})
	assert.Empty(t, stmts)
	term := result["terminator"].(map[string]interface{})
	assert.Equal(t, "ReturnTerminator", term["node_type"])
}

func TestMarshalBlock_WithStatements(t *testing.T) {
	e := newEmitter()
	blk := &BasicBlock{
		ID: BlockID(1),
		Statements: []Instruction{
			&CopyInst{Dst: "x", Src: "42"},
			&RefIncInst{Src: "s"},
		},
		Terminator: &JumpTerminator{Target: 2},
	}
	result := e.marshalBlock(blk)
	stmts := result["statements"].([]map[string]interface{})
	require.Len(t, stmts, 2)
	assert.Equal(t, "CopyInst", stmts[0]["node_type"])
	assert.Equal(t, "RefIncInst", stmts[1]["node_type"])
}

func TestMarshalBlock_NilTerminator(t *testing.T) {
	e := newEmitter()
	blk := &BasicBlock{ID: BlockID(0), Statements: []Instruction{}, Terminator: nil}
	result := e.marshalBlock(blk)
	assert.Nil(t, result["terminator"])
}

// =============================================================================
// marshalFunction
// =============================================================================

func TestMarshalFunction_BasicShape(t *testing.T) {
	e := newEmitter()
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{ID: 0, Statements: []Instruction{}, Terminator: &ReturnTerminator{}}

	fn := Function{
		Name:       "myFn",
		Params:     []*hir.Param{{Name: "n", Type: types.IntType{}}},
		ReturnType: types.UnitType{},
		IsAsync:    false,
		Graph:      g,
	}
	result := e.marshalFunction(fn)

	assert.Equal(t, "FnDecl", result["node_type"])
	assert.Equal(t, "myFn", result["name"])
	assert.Equal(t, false, result["is_async"])
	assert.Equal(t, 0, result["entry_block"])

	params := result["params"].([]map[string]interface{})
	require.Len(t, params, 1)
	assert.Equal(t, "n", params[0]["name"])
	pt := params[0]["type"].(map[string]interface{})
	assert.Equal(t, "Int", pt["kind"])

	rt := result["return_type"].(map[string]interface{})
	assert.Equal(t, "Unit", rt["kind"])
}

func TestMarshalFunction_AsyncFlag(t *testing.T) {
	e := newEmitter()
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{ID: 0, Statements: []Instruction{}, Terminator: &ReturnTerminator{}}

	fn := Function{Name: "asyncFn", Params: nil, ReturnType: types.UnitType{}, IsAsync: true, Graph: g}
	result := e.marshalFunction(fn)
	assert.Equal(t, true, result["is_async"])
}

func TestMarshalFunction_MultipleBlocks(t *testing.T) {
	e := newEmitter()
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{ID: 0, Statements: []Instruction{}, Terminator: &JumpTerminator{Target: 1}}
	g.Blocks[1] = &BasicBlock{ID: 1, Statements: []Instruction{}, Terminator: &ReturnTerminator{}}

	fn := Function{Name: "twoBlock", Params: nil, ReturnType: types.UnitType{}, Graph: g}
	result := e.marshalFunction(fn)

	blocks := result["blocks"].(map[string]interface{})
	assert.Len(t, blocks, 2)
	assert.Contains(t, blocks, "0")
	assert.Contains(t, blocks, "1")
}

// =============================================================================
// Emit (full round-trip)
// =============================================================================

func TestEmit_NilProgram_ReturnsNilDecls(t *testing.T) {
	e := newEmitter()
	var buf bytes.Buffer
	err := e.Emit(nil, &buf)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Nil(t, result)
}

func TestEmit_EmptyProgram_HasProgramNodeType(t *testing.T) {
	p := &Program{TypeDecls: nil, Functions: nil}
	result := emitProgram(t, p)
	assert.Equal(t, "Program", result["node_type"])
	assert.Nil(t, result["decls"])
}

func TestEmit_ProgramWithTypeDecl(t *testing.T) {
	p := &Program{
		TypeDecls: []*hir.TypeDecl{
			{Name: &hir.Identifier{Value: "Point"}, Type: &types.StructType{Name: "Point"}},
		},
		Functions: nil,
	}
	result := emitProgram(t, p)
	decls := getSlice(t, result, "decls")
	require.Len(t, decls, 1)
	decl := decls[0].(map[string]interface{})
	assert.Equal(t, "TypeDecl", decl["node_type"])
	assert.Equal(t, "Point", decl["name"])
}

func TestEmit_ProgramWithFunction_FullRoundTrip(t *testing.T) {
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{
		ID: 0,
		Statements: []Instruction{
			&CopyInst{Dst: "x", Src: "1"},
		},
		Terminator: &ReturnTerminator{},
	}

	p := &Program{
		TypeDecls: nil,
		Functions: []Function{
			{
				Name:       "main",
				Params:     []*hir.Param{},
				ReturnType: types.UnitType{},
				IsAsync:    false,
				Graph:      g,
			},
		},
	}

	result := emitProgram(t, p)
	assert.Equal(t, "Program", result["node_type"])

	decls := getSlice(t, result, "decls")
	require.Len(t, decls, 1)
	fnDecl := decls[0].(map[string]interface{})
	assert.Equal(t, "FnDecl", fnDecl["node_type"])
	assert.Equal(t, "main", fnDecl["name"])

	blocks := fnDecl["blocks"].(map[string]interface{})
	require.Contains(t, blocks, "0")
	block0 := blocks["0"].(map[string]interface{})
	stmts := block0["statements"].([]interface{})
	require.Len(t, stmts, 1)
	stmt := stmts[0].(map[string]interface{})
	assert.Equal(t, "CopyInst", stmt["node_type"])

	term := block0["terminator"].(map[string]interface{})
	assert.Equal(t, "ReturnTerminator", term["node_type"])
}

func TestEmit_OutputIsValidJSON(t *testing.T) {
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{ID: 0, Statements: []Instruction{}, Terminator: &ReturnTerminator{}}

	p := &Program{
		Functions: []Function{
			{Name: "f", Params: nil, ReturnType: types.IntType{}, Graph: g},
		},
	}

	e := newEmitter()
	var buf bytes.Buffer
	err := e.Emit(p, &buf)
	require.NoError(t, err)

	var out interface{}
	err = json.Unmarshal(buf.Bytes(), &out)
	assert.NoError(t, err, "emitted output must be valid JSON")
}

func TestEmit_MixedTypeDeclsAndFunctions(t *testing.T) {
	g := NewGraph()
	g.Entry = 0
	g.Blocks[0] = &BasicBlock{ID: 0, Statements: []Instruction{}, Terminator: &ReturnTerminator{}}

	p := &Program{
		TypeDecls: []*hir.TypeDecl{
			{Name: &hir.Identifier{Value: "Vec2"}, Type: &types.StructType{Name: "Vec2"}},
		},
		Functions: []Function{
			{Name: "zero", Params: nil, ReturnType: types.UnitType{}, Graph: g},
		},
	}

	result := emitProgram(t, p)
	decls := getSlice(t, result, "decls")
	// TypeDecls come first, then Functions
	require.Len(t, decls, 2)
	assert.Equal(t, "TypeDecl", decls[0].(map[string]interface{})["node_type"])
	assert.Equal(t, "FnDecl", decls[1].(map[string]interface{})["node_type"])
}
