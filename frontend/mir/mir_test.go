package mir

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Phase 1: Control Flow Graph & Basic Block Handling Tests
// =============================================================================

// TestNewGraph verifies that a newly constructed graph properly allocates
// its internal structural fields.
func TestNewGraph(t *testing.T) {
	g := NewGraph()

	if g == nil {
		t.Fatal("Expected NewGraph() to return a non-nil graph pointer")
	}

	if g.Blocks == nil {
		t.Fatal("Expected Graph.Blocks map to be initialized, got nil")
	}

	if len(g.Blocks) != 0 {
		t.Errorf("Expected a fresh graph to contain 0 blocks, got %d", len(g.Blocks))
	}
}

// TestBlockInsertionAndRetrieval ensures basic blocks can be correctly mapped,
// stored, and resolved by their unique identifier within the graph.
func TestBlockInsertionAndRetrieval(t *testing.T) {
	g := NewGraph()
	targetID := BlockID(42)

	// Construct a basic block shell with a dummy terminator
	block := &BasicBlock{
		ID:         targetID,
		Statements: []Instruction{},
		Terminator: &ReturnTerminator{},
	}

	// Insert into the CFG map
	g.Blocks[targetID] = block

	// Assert retrieval correctness
	retrieved, exists := g.Blocks[targetID]
	if !exists {
		t.Fatalf("Failed to retrieve block with ID %d from the graph", targetID)
	}

	if retrieved.ID != targetID {
		t.Errorf("Expected block identity to be %d, got %d", targetID, retrieved.ID)
	}

	if retrieved.Terminator == nil {
		t.Error("Expected retrieved basic block to retain its control flow terminator reference, got nil")
	}
}

// TestBlockBoundaryAndSequence verifies that basic blocks faithfully preserve
// the exact execution sequence and order of flat, desugared instructions.
func TestBlockBoundaryAndSequence(t *testing.T) {
	g := NewGraph()
	id := BlockID(0)
	g.Entry = id

	// Instantiate specific sequential instructions and a terminator
	inst1 := &CoroPrologueInst{}
	inst2 := &CoroPrologueInst{}
	term := &ReturnTerminator{}

	block := &BasicBlock{
		ID:         id,
		Statements: []Instruction{inst1, inst2},
		Terminator: term,
	}

	g.Blocks[id] = block

	// Resolve the block and assert strict ordering and boundary preservation
	res := g.Blocks[id]

	if len(res.Statements) != 2 {
		t.Fatalf("Expected basic block statements length to be 2, got %d", len(res.Statements))
	}

	if res.Statements[0] != inst1 || res.Statements[1] != inst2 {
		t.Error("Basic block failed to preserve instruction array alignment or sequential entry order")
	}

	if res.Terminator != term {
		t.Error("Basic block boundary terminator reference identity was corrupted or modified")
	}

	if g.Entry != id {
		t.Errorf("Expected Graph Entry boundary ID to be %d, got %d", id, g.Entry)
	}
}

// TestInstructionInterfaceAndStringification runs a comprehensive table-driven
// validation matrix across all Mid-Level IR instruction types to enforce
// interface compliance and precise debugging string generation.
func TestInstructionInterfaceAndStringification(t *testing.T) {
	tests := []struct {
		name           string
		instruction    Instruction
		expectedTokens []string
	}{
		{
			name: "Temporary Declaration Instruction",
			instruction: &TempDeclInst{
				Name: "t_var",
				Type: types.UnknownType{}, // Allocates an agnostic unknown type fallback
			},
			expectedTokens: []string{"let t_var:", "unknown"}, // Matches "let %s: %s"
		},
		{
			name: "Assignment Instruction",
			instruction: &AssignInst{
				Dst:    "dest_reg",
				RValue: &tast.Identifier{Value: "source_val"}, // Expects a flattened right-hand side
			},
			expectedTokens: []string{"dest_reg", "=", "source_val"}, // Matches "%s = %s"
		},
		{
			name: "Explicit Copy Instruction",
			instruction: &CopyInst{
				Dst: "copy_dst",
				Src: "copy_src",
			},
			expectedTokens: []string{"copy_dst", "=", "copy", "copy_src"}, // Matches "%s = copy %s"
		},
		{
			name: "Explicit Move Instruction",
			instruction: &MoveInst{
				Dst: "move_dst",
				Src: "move_src",
			},
			expectedTokens: []string{"move_dst", "=", "move", "move_src"}, // Matches "%s = move %s"
		},
		{
			name: "Reference Allocation Instruction",
			instruction: &RefAllocInst{
				Dst:  "heap_ptr",
				Type: types.UnknownType{},
			},
			expectedTokens: []string{"heap_ptr", "=", "ref_alloc", "unknown"}, // Matches "%s = ref_alloc %s"
		},
		{
			name: "Reference Increment Instruction",
			instruction: &RefIncInst{
				Src: "rc_handle",
			},
			expectedTokens: []string{"ref_inc", "rc_handle"}, // Matches "ref_inc %s"
		},
		{
			name: "Reference Decrement Instruction",
			instruction: &RefDecInst{
				Src: "rc_handle",
			},
			expectedTokens: []string{"ref_dec", "rc_handle"}, // Matches "ref_dec %s"
		},
		{
			name: "Mutable Borrow Exclusivity Instruction",
			instruction: &MutBorrowInst{
				Src: "borrowed_var",
			},
			expectedTokens: []string{"mut_borrow", "borrowed_var"}, // Matches "mut_borrow %s"
		},
		{
			name:           "Coroutine Prologue Instruction",
			instruction:    &CoroPrologueInst{},
			expectedTokens: []string{"coro_prologue"}, // Matches "coro_prologue" static literal
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Assert Interface Compliance
			// This explicitly verifies the type can be successfully bound to the
			// mir.Instruction interface at runtime.
			var inst Instruction = tc.instruction
			inst.isInstruction()

			// 2. Assert String Representation Validity
			strOutput := inst.String()
			if strOutput == "" {
				t.Errorf("Instruction %T returned an unexpected empty string", tc.instruction)
			}

			// Validate format compliance using substring token extraction
			for _, token := range tc.expectedTokens {
				// Convert to lower case to absorb standard/unknown naming variations safely
				if !strings.Contains(strings.ToLower(strOutput), strings.ToLower(token)) {
					t.Errorf("Expected instruction string output %q to contain token token %q", strOutput, token)
				}
			}
		})
	}
}

func TestCoreBuilderAndFlattening(t *testing.T) {
	tests := []struct {
		name     string
		fnDecl   *tast.FnDecl
		validate func(t *testing.T, g *Graph)
	}{
		{
			name: "Empty Synchronous Function Lowering",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)

				// Verify entry block configuration
				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists, "Graph entry block must be initialized and cached")
				assert.Empty(t, entryBlock.Statements, "Synchronous empty functions should not emit initial instructions")
			},
		},
		{
			name: "Async Function Coroutine Prologue Allocation",
			fnDecl: &tast.FnDecl{
				IsAsync: true,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)

				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)

				// Enforce state-machine frame initialization layout invariant
				require.NotEmpty(t, entryBlock.Statements, "Async functions must emit configuration instructions")
				assert.IsType(t, &CoroPrologueInst{}, entryBlock.Statements[0], "The absolute first instruction of an async entry block must be CoroPrologueInst")
			},
		},
		{
			name: "Variable Declaration and Memory Transfer lowering",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.DeclareStmt{
							Symbol: &types.Symbol{
								Name: "local_var",
								Type: types.IntType{},
							},
							Value: &tast.IntLiteral{Value: 100},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)

				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)

				// Ensure memory transfer primitives are instantiated
				var foundTempDecl bool
				for _, stmt := range entryBlock.Statements {
					if decl, ok := stmt.(*TempDeclInst); ok {
						if decl.Name == "local_var" {
							foundTempDecl = true
							assert.Equal(t, types.IntType{}, decl.Type, "Type context must propagate to the allocation instruction")
						}
					}
				}
				assert.True(t, foundTempDecl, "Builder failed to emit a TempDeclInst for the declared variable")
			},
		},
		{
			name: "Complex Expression Flattening and Temporary Registration",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.DeclareStmt{
							Symbol: &types.Symbol{
								Name: "result",
								Type: types.IntType{},
							},
							Value: &tast.InfixExpr{
								Left:     &tast.Identifier{Value: "lhs", Type: types.IntType{}},
								Operator: "+",
								Right:    &tast.Identifier{Value: "rhs", Type: types.IntType{}},
								Type:     types.IntType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)

				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)

				// Expression flattening decomposes expressions into continuous temporary SSA registers
				var registers []string
				for _, stmt := range entryBlock.Statements {
					if decl, ok := stmt.(*TempDeclInst); ok {
						registers = append(registers, decl.Name)
					}
				}

				// Assert that both the expression result temporary register (_t1) and the final target variable are allocated
				assert.Contains(t, registers, "_t1", "Flattening logic must allocate a unique temporary register for sub-expressions")
				assert.Contains(t, registers, "result", "Builder must allocate space for the top-level declared symbol variable")
			},
		},
		{
			name: "Variable Assignment Flow Mapping",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.AssignStmt{
							LValue: &tast.Identifier{Value: "target_ptr", Type: types.IntType{}},
							RValue: &tast.Identifier{Value: "source_val", Type: types.IntType{}},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)

				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)
				require.NotEmpty(t, entryBlock.Statements)

				// Verify assignment statements track and preserve variable identification metadata
				var matchedAssignment bool
				for _, stmt := range entryBlock.Statements {
					if decl, ok := stmt.(*TempDeclInst); ok && decl.Name == "target_ptr" {
						matchedAssignment = true
					}
				}
				assert.True(t, matchedAssignment, "Reassignment pass failed to lower target variable register space allocations")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Build(tc.fnDecl)
			tc.validate(t, g)
		})
	}
}

func TestAdvancedTypingsAndControlFlow(t *testing.T) {
	tests := []struct {
		name     string
		fnDecl   *tast.FnDecl
		validate func(t *testing.T, g *Graph)
	}{
		{
			name: "Prefix Expression Lowering",
			fnDecl: &tast.FnDecl{
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ExprStmt{
							Value: &tast.PrefixExpr{
								Operator: "-",
								Right:    &tast.Identifier{Value: "val", Type: types.IntType{}},
								Type:     types.IntType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock := g.Blocks[g.Entry]

				// Should find an assignment carrying a PrefixExpr assignment
				var foundPrefix bool
				for _, inst := range entryBlock.Statements {
					if assign, ok := inst.(*AssignInst); ok {
						if _, isPrefix := assign.RValue.(*tast.PrefixExpr); isPrefix {
							foundPrefix = true
							assert.Contains(t, assign.Dst, "_t")
						}
					}
				}
				assert.True(t, foundPrefix, "Failed to materialize PrefixExpr assignment into temporary register")
			},
		},
		{
			name: "Field Access and Index Expressions",
			fnDecl: &tast.FnDecl{
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ExprStmt{
							Value: &tast.IndexExpr{
								Left: &tast.FieldAccess{
									Object: &tast.Identifier{Value: "struct_var", Type: types.UnknownType{}},
									Field:  &tast.Identifier{Value: "array_field", Type: types.ArrayType{}},
									Type:   types.UnknownType{},
								},
								Index: &tast.IntLiteral{Value: 2},
								Type:  types.IntType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock := g.Blocks[g.Entry]

				var foundFieldAccess, foundIndexExpr bool
				for _, inst := range entryBlock.Statements {
					if assign, ok := inst.(*AssignInst); ok {
						switch assign.RValue.(type) {
						case *tast.FieldAccess:
							foundFieldAccess = true
						case *tast.IndexExpr:
							foundIndexExpr = true
						}
					}
				}
				assert.True(t, foundFieldAccess, "Failed to flatten nested FieldAccess expression")
				assert.True(t, foundIndexExpr, "Failed to flatten wrapping IndexExpr expression")
			},
		},
		{
			name: "If Expression Without Alternative (Else)",
			fnDecl: &tast.FnDecl{
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ExprStmt{
							Value: &tast.IfExpr{
								Condition: &tast.BoolLiteral{Value: true},
								Consequence: &tast.BlockStmt{
									Statements: []tast.Stmt{},
								},
								Alternative: nil,
								Type:        types.UnitType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				// Entry block (0), Then block (1), Merge block (2)
				require.Len(t, g.Blocks, 3)

				entryBlock := g.Blocks[g.Entry]
				branch, ok := entryBlock.Terminator.(*BranchTerminator)
				require.True(t, ok)

				// Without alternative, the false target maps directly to the merge block
				assert.Equal(t, branch.FalseTarget, BlockID(2), "False target must fall straight through to the merge block")
			},
		},
		{
			name: "Call Argument ABI Lowering (Own, Mut, Primitive, Reference)",
			fnDecl: &tast.FnDecl{
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ExprStmt{
							Value: &tast.CallExpr{
								Function: &tast.Identifier{Value: "target_func", Type: types.UnknownType{}},
								Arguments: []tast.CallArg{
									{Argument: &tast.Identifier{Value: "owned_var", Type: types.StringType{}}, Own: true},
									{Argument: &tast.Identifier{Value: "mut_var", Type: types.IntType{}}, Mut: true},
									{Argument: &tast.Identifier{Value: "prim_var", Type: types.IntType{}}},
									{Argument: &tast.Identifier{Value: "ref_var", Type: types.StringType{}}}, // Non-primitive borrow
								},
								Type: types.UnitType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock := g.Blocks[g.Entry]

				var hasMove, hasMutBorrow, hasCopy, hasAssign bool
				for _, inst := range entryBlock.Statements {
					switch inst.(type) {
					case *MoveInst:
						hasMove = true
					case *MutBorrowInst:
						hasMutBorrow = true
					case *CopyInst:
						hasCopy = true
					case *AssignInst:
						hasAssign = true
					}
				}

				assert.True(t, hasMove, "Missing MoveInst for 'own' capability argument lowering")
				assert.True(t, hasMutBorrow, "Missing MutBorrowInst for 'mut' capability exclusivity locking")
				assert.True(t, hasCopy, "Missing CopyInst for value-typed primitive standard borrow copy")
				assert.True(t, hasAssign, "Missing AssignInst for reference-typed standard immutable borrow redirection")
			},
		},
		// {
		// 	name: "Loop Escape Paths (Break & Continue Statements)",
		// 	fnDecl: &tast.FnDecl{
		// 		Body: &tast.BlockStmt{
		// 			Statements: []tast.Stmt{
		// 				&tast.LoopStmt{
		// 					Body: &tast.BlockStmt{
		// 						Statements: []tast.Stmt{
		// 							&tast.ContinueStmt{}, // Dead codes everything following it
		// 							&tast.BreakStmt{},    // Unreachable structural test
		// 						},
		// 					},
		// 				},
		// 			},
		// 		},
		// 	},
		// 	validate: func(t *testing.T, g *Graph) {
		// 		require.NotNil(t, g)
		// 		// Entry block (0), Loop Header (1), Loop Exit (2)
		// 		require.Len(t, g.Blocks, 3)

		// 		headerBlock, exists := g.Blocks[BlockID(1)]
		// 		require.True(t, exists)

		// 		// The continue statement should emit a jump straight back to the loop header
		// 		jump, ok := headerBlock.Terminator.(*JumpTerminator)
		// 		require.True(t, ok, "Loop continue statement must resolve to a JumpTerminator")
		// 		assert.Equal(t, BlockID(1), jump.Target, "Continue statement target must resolve to the loop header ID")
		// 	},
		// },
		{
			name: "Return Statement Lowering With Complex Return Expression",
			fnDecl: &tast.FnDecl{
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ReturnStmt{
							Value: &tast.InfixExpr{
								Left:     &tast.Identifier{Value: "a", Type: types.IntType{}},
								Operator: "+",
								Right:    &tast.Identifier{Value: "b", Type: types.IntType{}},
								Type:     types.IntType{},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock := g.Blocks[g.Entry]

				var foundRetAssign bool
				for _, inst := range entryBlock.Statements {
					if assign, ok := inst.(*AssignInst); ok && assign.Dst == "_ret" {
						foundRetAssign = true
					}
				}
				assert.True(t, foundRetAssign, "Complex return statement failed to materialize and bind its evaluation into the explicit '_ret' register slot")
				assert.IsType(t, &ReturnTerminator{}, entryBlock.Terminator, "Block containing explicit return must resolve to a ReturnTerminator")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Build(tc.fnDecl)
			tc.validate(t, g)
		})
	}
}

func TestTypeDrivenMemoryAndLifecycleIntegration(t *testing.T) {
	tests := []struct {
		name     string
		fnDecl   *tast.FnDecl
		validate func(t *testing.T, g *Graph)
	}{
		{
			name: "Memory Transfer - Reference Type Move vs Primitive Copy",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.DeclareStmt{
							Symbol: &types.Symbol{
								Name: "ref_var",
								Type: types.StringType{}, // IsReferenceType() returns true
							},
							Value: &tast.Identifier{Value: "source_ref", Type: types.StringType{}},
						},
						&tast.DeclareStmt{
							Symbol: &types.Symbol{
								Name: "prim_var",
								Type: types.IntType{}, // IsReferenceType() returns false
							},
							Value: &tast.Identifier{Value: "source_prim", Type: types.IntType{}},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)

				var foundMove, foundCopy bool
				for _, inst := range entryBlock.Statements {
					switch move := inst.(type) {
					case *MoveInst:
						if move.Dst == "ref_var" && move.Src == "source_ref" {
							foundMove = true
						}
					case *CopyInst:
						if move.Dst == "prim_var" && move.Src == "source_prim" {
							foundCopy = true
						}
					}
				}
				assert.True(t, foundMove, "Reference-allocated types must emit an explicit MoveInst during memory transfer lowered bounds")
				assert.True(t, foundCopy, "Value-allocated primitive types must emit an explicit CopyInst during memory transfer lowered bounds")
			},
		},
		{
			name: "Dead Code Elimination Post-Terminator Guardrail",
			fnDecl: &tast.FnDecl{
				IsAsync: false,
				Body: &tast.BlockStmt{
					Statements: []tast.Stmt{
						&tast.ReturnStmt{}, // Structural layout terminator shifts block target sequence to nil
						&tast.DeclareStmt{ // This declaration statement constitutes dead code
							Symbol: &types.Symbol{Name: "dead_var", Type: types.IntType{}},
							Value:  &tast.IntLiteral{Value: 42},
						},
					},
				},
			},
			validate: func(t *testing.T, g *Graph) {
				require.NotNil(t, g)
				entryBlock, exists := g.Blocks[g.Entry]
				require.True(t, exists)

				// Confirm that the builder cleanly drops operations listed after a block terminator sequence
				for _, inst := range entryBlock.Statements {
					if decl, ok := inst.(*TempDeclInst); ok {
						assert.NotEqual(t, "dead_var", decl.Name, "Dead code existing past an isolated return statement boundary must be discarded")
					}
				}
			},
		},
		// {
		// 	name: "Deeply Nested Integration - Conditionals Inside Loops",
		// 	fnDecl: &tast.FnDecl{
		// 		IsAsync: false,
		// 		Body: &tast.BlockStmt{
		// 			Statements: []tast.Stmt{
		// 				&tast.LoopStmt{
		// 					Body: &tast.BlockStmt{
		// 						Statements: []tast.Stmt{
		// 							&tast.ExprStmt{
		// 								Value: &tast.IfExpr{
		// 									Condition: &tast.Identifier{Value: "escape_flag", Type: types.BoolType{}},
		// 									Consequence: &tast.BlockStmt{
		// 										Statements: []tast.Stmt{
		// 											&tast.BreakStmt{},
		// 										},
		// 									},
		// 									Alternative: &tast.BlockStmt{
		// 										Statements: []tast.Stmt{
		// 											&tast.ContinueStmt{},
		// 										},
		// 									},
		// 									Type: types.UnitType{},
		// 								},
		// 							},
		// 						},
		// 					},
		// 				},
		// 			},
		// 		},
		// 	},
		// 	validate: func(t *testing.T, g *Graph) {
		// 		require.NotNil(t, g)

		// 		// Ensure loop tracking states and lexical stack closures correctly popped without leaking blocks
		// 		assert.GreaterOrEqual(t, len(g.Blocks), 4, "Nested loops combined with conditional choices must correctly distribute across basic block boundaries")
		// 	},
		// },
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Build(tc.fnDecl)
			tc.validate(t, g)
		})
	}
}
