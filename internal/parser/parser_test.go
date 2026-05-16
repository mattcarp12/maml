package parser

import (
	"testing"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeclareStatements(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedName  string
		expectedMut   bool
		expectedValue interface{}
	}{
		{
			name:          "immutable declaration",
			input:         "x := 5",
			expectedName:  "x",
			expectedMut:   false,
			expectedValue: 5,
		},
		{
			name:          "mutable declaration",
			input:         "mut y := 10",
			expectedName:  "y",
			expectedMut:   true,
			expectedValue: 10,
		},
		{
			name:          "declaration with identifier value",
			input:         "foobar := y",
			expectedName:  "foobar",
			expectedMut:   false,
			expectedValue: "y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			decl, ok := stmts[0].(*ast.DeclareStmt)
			require.True(t, ok, "expected DeclareStmt, got %T", stmts[0])

			assert.Equal(t, tt.expectedName, decl.Name)
			assert.Equal(t, tt.expectedMut, decl.Mutable)
			testLiteralExpression(t, decl.Value, tt.expectedValue)
		})
	}
}

func TestReturnStatements(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedValue interface{}
	}{
		{"return integer", "return 5", 5},
		{"return identifier", "return x", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			testLiteralExpression(t, retStmt.Value, tt.expectedValue)
		})
	}
}

func TestFunctionDeclarations(t *testing.T) {
	input := `fn add(x int, y int) int { return x + y }`

	program := parseProgram(t, input)

	require.Len(t, program.Decls, 1)

	fn, ok := program.Decls[0].(*ast.FnDecl)
	require.True(t, ok)

	assert.Equal(t, "add", fn.Name)
	assert.Equal(t, "int", fn.ReturnType.(*ast.NamedType).Name)
	assert.Len(t, fn.Params, 2)
	assert.Len(t, fn.Body.Statements, 1)
}

func TestTypeDeclaration(t *testing.T) {
	input := `
	type Point = {
		x int,
		y int
	}
	`

	program := parseProgram(t, input)
	require.Len(t, program.Decls, 1)

	td, ok := program.Decls[0].(*ast.TypeDecl)
	require.True(t, ok)
	assert.Equal(t, "Point", td.Name.Name)

	pt, ok := td.Rhs.(*ast.ProductType)
	require.True(t, ok)
	assert.Len(t, pt.Fields, 2)
}

func TestOperatorPrecedenceParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple add", "a + b", "(a + b)"},
		{"left associative", "a + b + c", "((a + b) + c)"},
		{"precedence math", "a + b * c", "(a + (b * c))"},
		{"parentheses", "5 + (2 * 3)", "(5 + (2 * 3))"},
		{"comparison", "a + b == c * d", "((a + b) == (c * d))"},
		{"relational tighter than equality", "x > 5 == true", "((x > 5) == true)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputWrapped := "return " + tt.input
			stmts := parseFunctionBody(t, inputWrapped)

			retStmt := stmts[0].(*ast.ReturnStmt)
			actual := retStmt.Value.String()

			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestBooleanParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"true", "true", true},
		{"false", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputWrapped := "return " + tt.input
			stmts := parseFunctionBody(t, inputWrapped)

			retStmt := stmts[0].(*ast.ReturnStmt)
			testLiteralExpression(t, retStmt.Value, tt.expected)
		})
	}
}

func TestIfExpressionParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple if",
			input:    "if (x > 5) { => true }",
			expected: "if (x > 5) {\n\t=> true\n}",
		},
		{
			name:     "if with else",
			input:    "if (x == y) { => 10 } else { => 20 }",
			expected: "if (x == y) {\n\t=> 10\n} else {\n\t=> 20\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputWrapped := "return " + tt.input
			stmts := parseFunctionBody(t, inputWrapped)

			retStmt := stmts[0].(*ast.ReturnStmt)
			actual := retStmt.Value.String()

			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestCallExpressionParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no args", "return add()", "add()"},
		{"one arg", "return add(5)", "add(5)"},
		{"multiple args", "return add(5, x + 2)", "add(5, (x + 2))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)
			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}

func TestStructLiteralAndFieldAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "struct literal",
			input:    "return Point{x: 10, y: 20 + 5}",
			expected: "Point{x: 10, y: (20 + 5)}",
		},
		{
			name:     "field access",
			input:    "return p.x + user.address.zip",
			expected: "((p.x) + (user.address).zip)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)
			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}

func TestParserErrors(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{"missing closing paren", "fn add(x int {", "expected next token"},
		{"invalid statement", "123", "only function declarations are supported at the top level"},
		{"missing type in param", "fn test(x) {}", "expected"},
		// {"unclosed block", "fn main() int { return 5", "Should NOT be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			p.ParseProgram()

			errors := p.Errors()
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0], tt.expectedErr)
		})
	}
}

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

func parseProgram(t *testing.T, input string) *ast.Program {
	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()

	checkParserErrors(t, p)
	return program
}

func parseFunctionBody(t *testing.T, input string) []ast.Stmt {
	fullInput := "fn test() int {\n" + input + "\n}"
	program := parseProgram(t, fullInput)

	require.Len(t, program.Decls, 1)

	fn, ok := program.Decls[0].(*ast.FnDecl)
	require.True(t, ok, "expected FnDecl")

	return fn.Body.Statements
}

func checkParserErrors(t *testing.T, p *Parser) {
	errors := p.Errors()
	if len(errors) == 0 {
		return
	}

	t.Errorf("parser has %d errors", len(errors))
	for _, msg := range errors {
		t.Errorf("parser error: %q", msg)
	}
	t.FailNow()
}

// testLiteralExpression dispatches to the correct literal tester
func testLiteralExpression(t *testing.T, exp ast.Expr, expected interface{}) {
	switch v := expected.(type) {
	case int64:
		testIntegerLiteral(t, exp, v)
	case int:
		testIntegerLiteral(t, exp, int64(v))
	case string:
		testIdentifier(t, exp, v)
	case bool:
		testBooleanLiteral(t, exp, v)
	default:
		t.Errorf("testLiteralExpression: type %T not supported", expected)
	}
}

func testIntegerLiteral(t *testing.T, il ast.Expr, value int64) {
	integ, ok := il.(*ast.IntLiteral)
	require.True(t, ok, "expected *ast.IntLiteral, got %T", il)
	assert.Equal(t, value, integ.Value)
}

func testBooleanLiteral(t *testing.T, exp ast.Expr, value bool) {
	bo, ok := exp.(*ast.BoolLiteral)
	require.True(t, ok, "expected *ast.BoolLiteral, got %T", exp)
	assert.Equal(t, value, bo.Value)
}

func testIdentifier(t *testing.T, exp ast.Expr, value string) {
	ident, ok := exp.(*ast.Identifier)
	require.True(t, ok, "expected *ast.Identifier, got %T", exp)
	assert.Equal(t, value, ident.Value)
}

func TestArrayLiteralParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		elemCnt  int
	}{
		{
			name:     "empty array",
			input:    "return []",
			expected: "[]",
			elemCnt:  0,
		},
		{
			name:     "integer array",
			input:    "return [1, 2, 3]",
			expected: "[1, 2, 3]",
			elemCnt:  3,
		},
		{
			name:     "mixed expressions",
			input:    "return [1, x, 2 + 3]",
			expected: "[1, x, (2 + 3)]",
			elemCnt:  3,
		},
		{
			name:     "nested arrays",
			input:    "return [[1, 2], [3, 4]]",
			expected: "[[1, 2], [3, 4]]",
			elemCnt:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)

			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			arr, ok := retStmt.Value.(*ast.ArrayLiteral)
			require.True(t, ok, "expected *ast.ArrayLiteral, got %T", retStmt.Value)

			assert.Len(t, arr.Elements, tt.elemCnt)
			assert.Equal(t, tt.expected, arr.String())
		})
	}
}

func TestIndexExpressionParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple index",
			input:    "return arr[0]",
			expected: "(arr[0])",
		},
		{
			name:     "expression index",
			input:    "return arr[1 + 1]",
			expected: "(arr[(1 + 1)])",
		},
		{
			name:     "nested indexing",
			input:    "return matrix[1][2]",
			expected: "((matrix[1])[2])",
		},
		{
			name:     "index after call",
			input:    "return getData()[0]",
			expected: "(getData()[0])",
		},
		{
			name:     "index precedence",
			input:    "return arr[1] + arr[2]",
			expected: "((arr[1]) + (arr[2]))",
		},
		{
			name:     "index with field access",
			input:    "return users[0].name",
			expected: "((users[0]).name)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)

			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}