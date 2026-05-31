package liveness

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/tast"
)

func newTestGraph(entryID int) *mir.Graph {
	g := mir.NewGraph()
	g.Entry = mir.BlockID(entryID)
	return g
}

func addBlock(g *mir.Graph, id int, term mir.Terminator) *mir.BasicBlock {
	block := &mir.BasicBlock{
		ID:         mir.BlockID(id),
		Statements: []mir.Instruction{},
		Terminator: term,
	}
	g.Blocks[mir.BlockID(id)] = block
	return block
}

func identExpr(name string) *tast.Identifier {
	return &tast.Identifier{
		Value: name,
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

func jumpTerm(target int) *mir.JumpTerminator {
	return &mir.JumpTerminator{
		Target: mir.BlockID(target),
	}
}

func branchTerm(cond tast.Expr, trueTarget, falseTarget int) *mir.BranchTerminator {
	return &mir.BranchTerminator{
		Condition:   cond,
		TrueTarget:  mir.BlockID(trueTarget),
		FalseTarget: mir.BlockID(falseTarget),
	}
}

func returnTerm() *mir.ReturnTerminator {
	return &mir.ReturnTerminator{}
}

func tempDeclInst(name string) *mir.TempDeclInst {
	return &mir.TempDeclInst{Name: name}
}

func refAllocInst(dst string) *mir.RefAllocInst {
	return &mir.RefAllocInst{Dst: dst}
}

func assignInst(dst string, expr tast.Expr) *mir.AssignInst {
	return &mir.AssignInst{Dst: dst, RValue: expr}
}

func infixExpr(left tast.Expr, op string, right tast.Expr) *tast.InfixExpr {
	return &tast.InfixExpr{Left: left, Operator: op, Right: right}
}

func prefixExpr(op string, right tast.Expr) *tast.PrefixExpr {
	return &tast.PrefixExpr{Operator: op, Right: right}
}

func callExpr(fn tast.Expr, args []tast.Expr) *tast.CallExpr {
	var callArgs []tast.CallArg
	for _, arg := range args {
		callArgs = append(callArgs, tast.CallArg{Argument: arg})
	}
	return &tast.CallExpr{Function: fn, Arguments: callArgs}
}

func sliceExpr(left, low, high tast.Expr) *tast.SliceExpr {
	return &tast.SliceExpr{Left: left, Low: low, High: high}
}

func indexExpr(left, index tast.Expr) *tast.IndexExpr {
	return &tast.IndexExpr{Left: left, Index: index}
}

func fieldAccess(obj tast.Expr, fieldName string) *tast.FieldAccess {
	return &tast.FieldAccess{
		Object: obj,
		Field:  &tast.Identifier{Value: fieldName},
	}
}

type livenessTestCase struct {
	name            string
	setupGraph      func() *mir.Graph
	expectedLiveIn  map[mir.BlockID]map[string]bool
	expectedLiveOut map[mir.BlockID]map[string]bool
}

func TestAnalyzeLiveness(t *testing.T) {
	tests := []livenessTestCase{
		{
			name: "Straight-line Code - Sequential Uses",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, returnTerm())

				b0.Statements = append(b0.Statements, copyInst("x", "y"))
				b0.Statements = append(b0.Statements, moveInst("_ret", "x"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"y": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
			},
		},
		{
			name: "Variable Death - Clear Out After Last Use",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, jumpTerm(1))
				b1 := addBlock(g, 1, returnTerm())

				b0.Statements = append(b0.Statements, copyInst("x", "y"))
				b1.Statements = append(b1.Statements, copyInst("_ret", "x"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"y": true},
				1: {"x": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {"x": true},
				1: {},
			},
		},
		{
			name: "Conditional Branching - Path Divergence",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				// FIX: Calling addBlock directly because b0 doesn't contain standalone statements
				addBlock(g, 0, branchTerm(identExpr("cond"), 1, 2))
				b1 := addBlock(g, 1, returnTerm())
				b2 := addBlock(g, 2, returnTerm())

				b1.Statements = append(b1.Statements, copyInst("_ret", "a"))
				b2.Statements = append(b2.Statements, copyInst("_ret", "b"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"cond": true, "a": true, "b": true},
				1: {"a": true},
				2: {"b": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {"a": true, "b": true},
				1: {},
				2: {},
			},
		},
		{
			name: "Control Flow Loop - Back-edge Propagation",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, jumpTerm(1))
				// FIX: Calling addBlock directly because b1 doesn't contain standalone statements
				addBlock(g, 1, branchTerm(identExpr("cond"), 2, 3))
				b2 := addBlock(g, 2, jumpTerm(1))
				b3 := addBlock(g, 3, returnTerm())

				b0.Statements = append(b0.Statements, copyInst("x", "y"))
				b2.Statements = append(b2.Statements, copyInst("z", "x"))
				b2.Statements = append(b2.Statements, copyInst("x", "w"))
				b3.Statements = append(b3.Statements, copyInst("_ret", "z"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"y": true, "cond": true, "w": true, "z": true},
				1: {"cond": true, "x": true, "w": true, "z": true},
				2: {"cond": true, "x": true, "w": true},
				3: {"z": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {"cond": true, "x": true, "w": true, "z": true},
				1: {"cond": true, "x": true, "w": true, "z": true},
				2: {"cond": true, "x": true, "w": true, "z": true},
				3: {},
			},
		},
		{
			name: "Function Return - Implicit Return Boundary Tracking",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				addBlock(g, 0, returnTerm())
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"_ret": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
			},
		},
		{
			name: "Sparse Graph - Missing Block IDs",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				// Create blocks 0 and 2, skipping 1.
				// len(g.Blocks) will be 2, so the backward loop checks IDs 1 and 0.
				// Checking ID 1 triggers the !exists coverage path.
				addBlock(g, 0, returnTerm())
				addBlock(g, 2, returnTerm())
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"_ret": true},
				2: {"_ret": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
				2: {},
			},
		},
		{
			name: "Instruction Variants - TempDecl and RefAlloc",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, returnTerm())

				// Exercise remaining statement type switches
				b0.Statements = append(b0.Statements, tempDeclInst("t1"))
				b0.Statements = append(b0.Statements, refAllocInst("r1"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"_ret": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
			},
		},
		{
			name: "Complex Expressions - Deep AST Walkthrough",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, returnTerm())

				// Evaluates: target = myFunc(obj.field[idx], arr[low:high]) + (-prefixVar)
				// Thoroughly covers every remaining branch of extractUses()
				expr := infixExpr(
					callExpr(
						identExpr("myFunc"),
						[]tast.Expr{
							indexExpr(fieldAccess(identExpr("obj"), "field"), identExpr("idx")),
							sliceExpr(identExpr("arr"), identExpr("low"), identExpr("high")),
						},
					),
					"+",
					prefixExpr("-", identExpr("prefixVar")),
				)

				b0.Statements = append(b0.Statements, assignInst("target", expr))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {
					"_ret": true, "myFunc": true, "obj": true, "idx": true,
					"arr": true, "low": true, "high": true, "prefixVar": true,
				},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
			},
		},
		{
			name: "Shadowed Definitions - Def Before Use",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, returnTerm())

				// Define variables first, then use them in the same block.
				// This forces the "if !defSet[u]" and "if !defSet[src]" checks to evaluate to false.
				b0.Statements = append(b0.Statements, copyInst("x", "y"))
				b0.Statements = append(b0.Statements, copyInst("z", "x"))              // x is already defined
				b0.Statements = append(b0.Statements, moveInst("m", "z"))              // z is already defined
				b0.Statements = append(b0.Statements, assignInst("a", identExpr("m"))) // m is already defined
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"y": true, "_ret": true}, // Only y and _ret flow into the block
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {},
			},
		},
		{
			name: "Shadowed Definition - Branch Condition",
			setupGraph: func() *mir.Graph {
				g := newTestGraph(0)
				b0 := addBlock(g, 0, branchTerm(identExpr("cond"), 1, 1))
				addBlock(g, 1, returnTerm())

				// Define 'cond' within the block before the terminator uses it
				b0.Statements = append(b0.Statements, copyInst("cond", "inputVal"))
				return g
			},
			expectedLiveIn: map[mir.BlockID]map[string]bool{
				0: {"inputVal": true, "_ret": true},
				1: {"_ret": true},
			},
			expectedLiveOut: map[mir.BlockID]map[string]bool{
				0: {"_ret": true},
				1: {},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.setupGraph()
			res := AnalyzeLiveness(g)

			// Assert LiveIn sets
			for blockID, expectedVars := range tc.expectedLiveIn {
				actualVars, exists := res.LiveIn[blockID]
				if !exists {
					t.Errorf("FAIL [%s]: Block %d missing from LiveIn map", tc.name, blockID)
					continue
				}

				for v := range expectedVars {
					if !actualVars[v] {
						t.Errorf("FAIL [%s]: Block %d LiveIn missing expected variable %q", tc.name, blockID, v)
					}
				}

				for v := range actualVars {
					if !expectedVars[v] {
						t.Errorf("FAIL [%s]: Block %d LiveIn contains unexpected variable %q", tc.name, blockID, v)
					}
				}
			}

			// Assert LiveOut sets
			for blockID, expectedVars := range tc.expectedLiveOut {
				actualVars, exists := res.LiveOut[blockID]
				if !exists {
					t.Errorf("FAIL [%s]: Block %d missing from LiveOut map", tc.name, blockID)
					continue
				}

				for v := range expectedVars {
					if !actualVars[v] {
						t.Errorf("FAIL [%s]: Block %d LiveOut missing expected variable %q", tc.name, blockID, v)
					}
				}

				for v := range actualVars {
					if !expectedVars[v] {
						t.Errorf("FAIL [%s]: Block %d LiveOut contains unexpected variable %q", tc.name, blockID, v)
					}
				}
			}
		})
	}
}
