package lower

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlinePass(t *testing.T) {
	input := `
		fn add(a int, b int) int {
			return a + b
		}
		fn main() int {
			x := add(5, 10)
			return x
		}
	`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	// 1. Run the Inliner Pass BEFORE Semantic Analysis
	pass := NewInlinePass(program)
	rewrittenNode := ast.Rewrite(pass, program)

	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok, "Rewritten node must remain an *ast.Program")

	// 2. Find the main function
	var mainFn *ast.FnDecl
	for _, decl := range rewrittenProgram.Decls {
		if fn, ok := decl.(*ast.FnDecl); ok && fn.Name == "main" {
			mainFn = fn
			break
		}
	}
	require.NotNil(t, mainFn, "Could not find main function")

	// 3. Verify the first statement in main is our inlined block wrapped in the assignment to 'x'
	require.GreaterOrEqual(t, len(mainFn.Body.Statements), 1)
	declStmt, ok := mainFn.Body.Statements[0].(*ast.DeclareStmt)
	require.True(t, ok, "Expected x declaration")

	inlinedBlock, ok := declStmt.Value.(*ast.BlockStmt)
	require.True(t, ok, "Expected add() CallExpr to be replaced by an inlined BlockStmt")

	// Block should have: 'a' declaration, 'b' declaration, and a yield statement
	require.Len(t, inlinedBlock.Statements, 3)

	aDecl, ok := inlinedBlock.Statements[0].(*ast.DeclareStmt)
	require.True(t, ok)
	assert.Equal(t, "a", aDecl.Name)
	assert.Equal(t, "5", aDecl.Value.String())

	bDecl, ok := inlinedBlock.Statements[1].(*ast.DeclareStmt)
	require.True(t, ok)
	assert.Equal(t, "b", bDecl.Name)
	assert.Equal(t, "10", bDecl.Value.String())

	yieldStmt, ok := inlinedBlock.Statements[2].(*ast.YieldStmt)
	require.True(t, ok)
	assert.Contains(t, yieldStmt.Value.String(), "a + b") // Format can be a+b or (a + b)
}
