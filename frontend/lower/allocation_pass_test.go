package lower

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func analyzeEscapeAndLower(t *testing.T, input string) *ast.Program {
	t.Helper()
	// 1. Parse
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	// 2. Semantic Analysis
	semanticAnalyzer := sema.New()
	typeMap, errs := semanticAnalyzer.Analyze(program)
	require.Empty(t, errs, "Semantic analyzer returned errors")

	// 3. Escape Analysis
	escapeAnalyzer := escape.New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	// 4. Allocation Lowering
	pass := NewAllocPass(escapeMap)
	rewrittenNode := ast.Rewrite(pass, program)

	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok, "Rewritten node must remain an *ast.Program")

	return rewrittenProgram
}

func TestExplicitAllocationNodes(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectHeap bool
	}{
		{
			name: "local array returned by value stays on stack",
			input: `
				fn main() [2]int {   // Return a statically sized array (value type)
					arr := [3, 5]    // Comma-separated array literal
					return arr       // Copied by value, so the local allocation does not escape
				}
			`,
			expectHeap: false,
		},
		{
			name: "returned slice escapes backing array to heap",
			input: `
				fn main() []int {
					arr := [3,5]
					s := arr[:]
					return s
				}
			`,
			expectHeap: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := analyzeEscapeAndLower(t, tt.input)

			require.GreaterOrEqual(t, len(program.Decls), 1)
			fnDecl, ok := program.Decls[0].(*ast.FnDecl)
			require.True(t, ok)

			require.GreaterOrEqual(t, len(fnDecl.Body.Statements), 1)
			declStmt, ok := fnDecl.Body.Statements[0].(*ast.DeclareStmt)
			require.True(t, ok)

			assert.Equal(t, "arr", declStmt.Name)

			// Verify that the literal has been correctly wrapped
			if tt.expectHeap {
				_, isHeap := declStmt.Value.(*ast.HeapAllocExpr)
				assert.True(t, isHeap, "Expected ArrayLiteral to be wrapped in a HeapAllocExpr")
			} else {
				_, isStack := declStmt.Value.(*ast.StackAllocExpr)
				assert.True(t, isStack, "Expected ArrayLiteral to be wrapped in a StackAllocExpr")
			}
		})
	}
}
