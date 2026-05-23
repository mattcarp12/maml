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

func analyzeAndLowerAggregate(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	require.Empty(t, p.Errors(), "Parser returned errors")

	analyzer := sema.New()
	typeMap, errs := analyzer.Analyze(program)
	require.Empty(t, errs, "Semantic analyzer returned errors")

	pass := NewAggregatePass(typeMap)
	rewrittenNode := ast.Rewrite(pass, program)

	rewrittenProgram, ok := rewrittenNode.(*ast.Program)
	require.True(t, ok)
	return rewrittenProgram
}

func TestLowerArrayLiteral(t *testing.T) {
	input := `
		fn main() [2]int {
			return [4, 5]
		}
	`
	program := analyzeAndLowerAggregate(t, input)

	fnDecl := program.Decls[0].(*ast.FnDecl)
	retStmt := fnDecl.Body.Statements[0].(*ast.ReturnStmt)

	// The array literal should now be a block statement
	block, ok := retStmt.Value.(*ast.BlockStmt)
	require.True(t, ok, "Expected ArrayLiteral to be lowered into a BlockStmt")

	// Stmts: 0: Decl, 1: arr[0]=4, 2: arr[1]=5, 3: Yield
	require.Len(t, block.Statements, 4)

	decl, ok := block.Statements[0].(*ast.DeclareStmt)
	require.True(t, ok)
	assert.Equal(t, "_arr_tmp_1", decl.Name)

	emptyArr, ok := decl.Value.(*ast.ArrayLiteral)
	require.True(t, ok)
	assert.Len(t, emptyArr.Elements, 0, "Base allocation must be completely empty")

	assign1, ok := block.Statements[1].(*ast.AssignStmt)
	require.True(t, ok)
	assert.Equal(t, "(_arr_tmp_1[0])", assign1.LValue.String())
	assert.Equal(t, "4", assign1.RValue.String())

	assign2, ok := block.Statements[2].(*ast.AssignStmt)
	require.True(t, ok)
	assert.Equal(t, "(_arr_tmp_1[1])", assign2.LValue.String())
	assert.Equal(t, "5", assign2.RValue.String())

	yield, ok := block.Statements[3].(*ast.YieldStmt)
	require.True(t, ok)
	assert.Equal(t, "_arr_tmp_1", yield.Value.String())
}
