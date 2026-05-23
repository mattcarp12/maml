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

func analyzeAndLowerAsync(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	analyzer := sema.New()
	typeMap, errs := analyzer.Analyze(program)
	require.Empty(t, errs, "Semantic analyzer returned errors")

	pass := NewAsyncPass(typeMap)
	rewrittenNode := ast.Rewrite(pass, program)

	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok)
	return rewrittenProgram
}

func TestLowerAsyncPrologue(t *testing.T) {
	input := `
		async fn fetch_data() int {
			return 42
		}
	`
	program := analyzeAndLowerAsync(t, input)

	require.GreaterOrEqual(t, len(program.Decls), 1)
	fnDecl, ok := program.Decls[0].(*ast.FnDecl)
	require.True(t, ok, "Expected top-level declaration to be a function")

	require.NotNil(t, fnDecl.Body)

	// The body should now have TWO statements: the injected prologue, and the return
	require.GreaterOrEqual(t, len(fnDecl.Body.Statements), 2)

	// 1. Verify the first statement is the prologue
	exprStmt, ok := fnDecl.Body.Statements[0].(*ast.ExprStmt)
	require.True(t, ok, "Expected first statement to be an ExprStmt")

	_, isPrologue := exprStmt.Value.(*ast.AsyncPrologueExpr)
	assert.True(t, isPrologue, "Expected AsyncPrologueExpr as first statement")

	// 2. Verify the second statement is the original return
	_, isReturn := fnDecl.Body.Statements[1].(*ast.ReturnStmt)
	assert.True(t, isReturn, "Expected ReturnStmt as second statement")
}

func TestStandardFunctionsUnchanged(t *testing.T) {
	input := `
		fn sync_func() int {
			return 42
		}
	`
	program := analyzeAndLowerAsync(t, input)
	fnDecl := program.Decls[0].(*ast.FnDecl)

	// Body should only have ONE statement because it's not async
	require.Len(t, fnDecl.Body.Statements, 1)
	_, isReturn := fnDecl.Body.Statements[0].(*ast.ReturnStmt)
	assert.True(t, isReturn, "Expected ReturnStmt to remain the first and only statement")
}
