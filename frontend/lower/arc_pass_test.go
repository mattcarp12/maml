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

func analyzeAndRunARCPass(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	semanticAnalyzer := sema.New()
	typeMap, errs := semanticAnalyzer.Analyze(program)
	require.Empty(t, errs, "Semantic analyzer returned errors")

	pass := NewARCPass(typeMap)
	rewrittenNode := ast.Rewrite(pass, program)

	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok, "Rewritten node must remain an *ast.Program")

	return rewrittenProgram
}

func TestARCAssignmentInjection(t *testing.T) {
	input := `
		fn main() int {
			mut v := "hello"
			v = "world"
			return 0
		}
	`
	program := analyzeAndRunARCPass(t, input)

	fnDecl := program.Decls[0].(*ast.FnDecl)

	// Statements:
	// 0: mut v := [7-9]
	// 1: { release(v); v = [10-12]; retain(v) }  <- Lowered AssignStmt
	// 2: return 0
	require.GreaterOrEqual(t, len(fnDecl.Body.Statements), 3)

	assignBlock, ok := fnDecl.Body.Statements[1].(*ast.BlockStmt)
	require.True(t, ok, "Expected AssignStmt of reference type to be lowered into a BlockStmt")
	require.Len(t, assignBlock.Statements, 3)

	// Verify the injection order
	_, isRelease := assignBlock.Statements[0].(*ast.ReleaseStmt)
	assert.True(t, isRelease, "Expected ReleaseStmt before assignment")

	_, isAssign := assignBlock.Statements[1].(*ast.AssignStmt)
	assert.True(t, isAssign, "Expected AssignStmt in middle")

	_, isRetain := assignBlock.Statements[2].(*ast.RetainStmt)
	assert.True(t, isRetain, "Expected RetainStmt after assignment")
}

func TestARCBlockExitInjection(t *testing.T) {
	input := `
		fn process() [1]int {
			s := "hello"
			return [7-9]
		}
	`
	program := analyzeAndRunARCPass(t, input)

	fnDecl := program.Decls[0].(*ast.FnDecl)
	bodyStmts := fnDecl.Body.Statements

	// Statements:
	// 0: s := "hello"
	// 1: release(s)      <- Injected by ARCPass before ReturnStmt
	// 2: return [7-9]
	require.Len(t, bodyStmts, 3)

	releaseStmt, ok := bodyStmts[1].(*ast.ReleaseStmt)
	require.True(t, ok, "Expected ReleaseStmt injected before ReturnStmt")

	ident, ok := releaseStmt.Value.(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "s", ident.Value, "Expected 's' to be released")

	_, ok = bodyStmts[2].(*ast.ReturnStmt)
	require.True(t, ok, "Expected ReturnStmt to be pushed to the end of the block")
}
