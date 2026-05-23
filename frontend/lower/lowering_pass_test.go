package lower

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to parse code into an AST
func parseProgram(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors: %v", p.Errors())
	return program
}

type mockTypeVisitor struct {
	typeMap map[ast.Node]sema.Type
}

func (v *mockTypeVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if ident, ok := node.(*ast.Identifier); ok {
		switch ident.Value {
		case "m":
			v.typeMap[ident] = &sema.MapType{}
		case "v":
			v.typeMap[ident] = &sema.VectorType{}
		}
	}
	return v
}

func TestLowerForStmt(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		hasInit      bool
		hasCondition bool
		hasPost      bool
	}{
		{
			name: "while-style loop (condition only)",
			input: `
				fn main() int {
					for (x < 10) { 
						puts("hello") 
					}
					return 0
				}
			`,
			hasInit:      false,
			hasCondition: true,
			hasPost:      false,
		},
		{
			name: "standard for loop (init, condition, post)",
			input: `
				fn main() int {
					for (mut i := 0; i < 10; i = i + 1) {
						puts("hello")
					}
					return 0
				}
			`,
			hasInit:      true,
			hasCondition: true,
			hasPost:      true,
		},
		{
			name: "infinite loop (no condition)",
			input: `
				fn main() int {
					for { 
						puts("infinite") 
					}
					return 0
				}
			`,
			hasInit:      false,
			hasCondition: false,
			hasPost:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)

			pass := NewPass(nil)
			rewrittenNode := ast.Rewrite(pass, program)
			rewrittenProgram, ok := rewrittenNode.(*ast.Program)
			require.True(t, ok)

			fnDecl := rewrittenProgram.Decls[0].(*ast.FnDecl)
			mainBody := fnDecl.Body

			require.GreaterOrEqual(t, len(mainBody.Statements), 1)
			outerBlock, ok := mainBody.Statements[0].(*ast.BlockStmt)
			require.True(t, ok, "Expected ForStmt to be lowered into a BlockStmt")

			var loopStmt *ast.LoopStmt
			if tt.hasInit {
				require.GreaterOrEqual(t, len(outerBlock.Statements), 2)
				_, ok = outerBlock.Statements[0].(*ast.DeclareStmt)
				assert.True(t, ok, "Expected Init to be a DeclareStmt")
				loopStmt, ok = outerBlock.Statements[1].(*ast.LoopStmt)
				require.True(t, ok)
			} else {
				loopStmt, ok = outerBlock.Statements[0].(*ast.LoopStmt)
				require.True(t, ok)
			}

			loopBody := loopStmt.Body
			require.GreaterOrEqual(t, len(loopBody.Statements), 1)

			exprStmt, ok := loopBody.Statements[0].(*ast.ExprStmt)
			require.True(t, ok)
			ifExpr, ok := exprStmt.Value.(*ast.IfExpr)
			require.True(t, ok)

			consequence := ifExpr.Consequence
			require.Len(t, consequence.Statements, 1)
			_, ok = consequence.Statements[0].(*ast.BreakStmt)
			require.True(t, ok)

			if tt.hasCondition {
				prefix, ok := ifExpr.Condition.(*ast.PrefixExpr)
				require.True(t, ok)
				assert.Equal(t, "!", prefix.Operator)
			} else {
				boolLit, ok := ifExpr.Condition.(*ast.BoolLiteral)
				require.True(t, ok)
				assert.False(t, boolLit.Value)
			}

			if tt.hasPost {
				lastStmt := loopBody.Statements[len(loopBody.Statements)-1]
				_, ok = lastStmt.(*ast.AssignStmt)
				assert.True(t, ok)
			}
		})
	}
}

func TestLowerNestedForStmts(t *testing.T) {
	input := `
		fn main() int {
			for (mut i := 0; i < 5; i = i + 1) {
				for (mut j := 0; j < 5; j = j + 1) {
					puts("nested")
				}
			}
			return 0
		}
	`
	program := parseProgram(t, input)
	pass := NewPass(nil)
	rewrittenProgram := ast.Rewrite(pass, program).(*ast.Program)
	ast.Walk(&NoForStmtVisitor{t: t}, rewrittenProgram)
}

type NoForStmtVisitor struct {
	t *testing.T
}

func (v *NoForStmtVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if _, isFor := node.(*ast.ForStmt); isFor {
		assert.Fail(v.t, "Found an un-lowered ForStmt in the AST!")
	}
	return v
}

// analyzeAndLower parses, type-checks, and lowers the program
func analyzeAndLower(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	analyzer := sema.New()
	typeMap, errs := analyzer.Analyze(program)
	require.Empty(t, errs, "Semantic analyzer returned errors: %v", errs)

	pass := NewPass(typeMap)
	rewrittenNode := ast.Rewrite(pass, program)
	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok)

	return rewrittenProgram
}

func TestLowerBuiltinMethods(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "lower map.put",
			input: `
				fn main() int {
					m := Map<string, int>()
					m.put("key", 42)
					return 0
				}
			`,
			expected: "maml_map_put(m, \"key\", 42)",
		},
		{
			name: "lower map.get",
			input: `
				fn main() int {
					m := Map<string, int>()
					val := m.get("key")
					return 0
				}
			`,
			expected: "maml_map_get(m, \"key\")",
		},
		{
			name: "lower vec.push",
			input: `
				fn main() int {
					v := Vec<int>()
					v.push(99)
					return 0
				}
			`,
			expected: "maml_vec_push(v, 99)",
		},
		{
			name: "lower vec.len to property access",
			input: `
				fn main() int {
					v := Vec<int>()
					l := v.len()
					return 0
				}
			`,
			expected: "(v.len)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Bypass analyzeAndLower to parse without full SEMA strictness
			l := lexer.New(tt.input)
			p := parser.New(l)
			program := p.ParseProgram()
			require.Empty(t, p.Errors(), "Parser returned errors")

			// 2. Manually inject the types for 'm' and 'v'
			typeMap := make(map[ast.Node]sema.Type)
			ast.Walk(&mockTypeVisitor{typeMap: typeMap}, program)

			pass := NewPass(typeMap)
			rewrittenNode := ast.Rewrite(pass, program)
			rewrittenProgram, ok := rewrittenNode.(*ast.Program)
			require.True(t, ok)

			fnDecl := rewrittenProgram.Decls[0].(*ast.FnDecl)
			stmts := fnDecl.Body.Statements

			require.GreaterOrEqual(t, len(stmts), 2, "Expected at least declare + builtin call")

			// 3. Find the statement containing the method call
			var targetStmt ast.Stmt
			for _, s := range stmts {
				if decl, isDecl := s.(*ast.DeclareStmt); isDecl {
					// ONLY skip the initial setup declarations, not the target declarations!
					if decl.Name == "m" || decl.Name == "v" {
						continue
					}
				}
				if _, isRet := s.(*ast.ReturnStmt); isRet {
					continue
				}
				targetStmt = s
				break
			}
			require.NotNil(t, targetStmt, "Could not find lowered builtin statement. Check sema type resolution.")

			var loweredStr string
			switch s := targetStmt.(type) {
			case *ast.ExprStmt:
				loweredStr = s.Value.String()
			case *ast.DeclareStmt:
				loweredStr = s.Value.String() // Safely captures val := m.get(...)
			default:
				t.Fatalf("Unexpected statement type: %T", targetStmt)
			}

			assert.Equal(t, tt.expected, loweredStr)
		})
	}
}

func TestLowerMatchExpr(t *testing.T) {
	input := `
		type Shape = 
			| Circle { radius int }
			| Point

		fn get_area(s Shape) int {
			return match s {
				case Circle(r) { => r * 2 }
				case Point { => 0 }
			}
		}
	`
	program := analyzeAndLower(t, input)

	require.GreaterOrEqual(t, len(program.Decls), 2)
	fnDecl := program.Decls[1].(*ast.FnDecl)

	require.GreaterOrEqual(t, len(fnDecl.Body.Statements), 1)
	retStmt, ok := fnDecl.Body.Statements[0].(*ast.ReturnStmt)
	require.True(t, ok, "Expected ReturnStmt as first body statement")

	// MatchExpr should lower to BlockStmt inside ReturnStmt.Value
	outerBlock, ok := retStmt.Value.(*ast.BlockStmt)
	require.True(t, ok, "MatchExpr should lower to BlockStmt")

	require.Len(t, outerBlock.Statements, 2)

	// Subject declaration
	declStmt, ok := outerBlock.Statements[0].(*ast.DeclareStmt)
	require.True(t, ok)
	assert.Equal(t, "_match_subj", declStmt.Name)

	// YieldStmt containing IfExpr chain
	yieldStmt, ok := outerBlock.Statements[1].(*ast.YieldStmt)
	require.True(t, ok)

	ifExpr, ok := yieldStmt.Value.(*ast.IfExpr)
	require.True(t, ok, "Expected IfExpr chain inside YieldStmt")

	// Check discriminant condition
	cond, ok := ifExpr.Condition.(*ast.InfixExpr)
	require.True(t, ok)
	assert.Equal(t, "==", cond.Operator)
	assert.Contains(t, cond.Left.String(), "_match_subj.0")
	assert.Equal(t, "0", cond.Right.String())
}
