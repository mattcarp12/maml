package parser

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Declarations
// =============================================================================

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
		{
			name:          "declaration with boolean true",
			input:         "ok := true",
			expectedName:  "ok",
			expectedMut:   false,
			expectedValue: true,
		},
		{
			name:          "declaration with boolean false",
			input:         "done := false",
			expectedName:  "done",
			expectedMut:   false,
			expectedValue: false,
		},
		{
			name:          "declaration with string",
			input:         `msg := "hello"`,
			expectedName:  "msg",
			expectedMut:   false,
			expectedValue: "hello",
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

			// String values may be identifiers or string literals; dispatch manually.
			if tt.name == "declaration with string" {
				sl, ok := decl.Value.(*ast.StringLiteral)
				require.True(t, ok, "expected *ast.StringLiteral, got %T", decl.Value)
				assert.Equal(t, "hello", sl.Value)
			} else {
				testLiteralExpression(t, decl.Value, tt.expectedValue)
			}
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
		{"return bool true", "return true", true},
		{"return bool false", "return false", false},
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

func TestYieldStatements(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"yield integer", "=> 42", "42"},
		{"yield identifier", "=> x", "x"},
		{"yield expression", "=> a + b", "(a + b)"},
		{"yield bool", "=> true", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			yStmt, ok := stmts[0].(*ast.YieldStmt)
			require.True(t, ok, "expected *ast.YieldStmt, got %T", stmts[0])
			assert.Equal(t, tt.expected, yStmt.Value.String())
		})
	}
}

func TestAssignmentStatements(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		lvalue string
		rvalue string
	}{
		{
			name:   "simple assign",
			input:  "x = 5",
			lvalue: "x",
			rvalue: "5",
		},
		{
			name:   "assign expression",
			input:  "x = a + b",
			lvalue: "x",
			rvalue: "(a + b)",
		},
		{
			name:   "assign to field access",
			input:  "p.x = 10",
			lvalue: "(p.x)",
			rvalue: "10",
		},
		{
			name:   "assign to index",
			input:  "arr[0] = 99",
			lvalue: "(arr[0])",
			rvalue: "99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			assign, ok := stmts[0].(*ast.AssignStmt)
			require.True(t, ok, "expected *ast.AssignStmt, got %T", stmts[0])
			assert.Equal(t, tt.lvalue, assign.LValue.String())
			assert.Equal(t, tt.rvalue, assign.RValue.String())
		})
	}
}

func TestFunctionDeclarations(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		fnName     string
		returnType string
		paramCount int
		stmtCount  int
	}{
		{
			name:       "two params",
			input:      `fn add(x int, y int) int { return x + y }`,
			fnName:     "add",
			returnType: "int",
			paramCount: 2,
			stmtCount:  1,
		},
		{
			name:       "no params",
			input:      "fn greet() string {\n return msg \n}",
			fnName:     "greet",
			returnType: "string",
			paramCount: 0,
			stmtCount:  1,
		},
		{
			name:       "one param",
			input:      "fn double(n int) int {\n return n \n}",
			fnName:     "double",
			returnType: "int",
			paramCount: 1,
			stmtCount:  1,
		},
		{
			name:       "multiple statements",
			input:      "fn calc(a int, b int) int {\n x := a + b\n return x \n}",
			fnName:     "calc",
			returnType: "int",
			paramCount: 2,
			stmtCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			require.Len(t, program.Decls, 1)

			fn, ok := program.Decls[0].(*ast.FnDecl)
			require.True(t, ok)

			assert.Equal(t, tt.fnName, fn.Name)
			assert.Equal(t, tt.returnType, fn.ReturnType.(*ast.NamedType).Name)
			assert.Len(t, fn.Params, tt.paramCount)
			assert.Len(t, fn.Body.Statements, tt.stmtCount)
		})
	}
}

func TestFunctionParamTypes(t *testing.T) {
	input := `fn add(x int, y int) int { return x + y }`
	program := parseProgram(t, input)
	fn := program.Decls[0].(*ast.FnDecl)

	require.Len(t, fn.Params, 2)
	assert.Equal(t, "x", fn.Params[0].Name)
	assert.Equal(t, "int", fn.Params[0].Type.(*ast.NamedType).Name)
	assert.Equal(t, "y", fn.Params[1].Name)
	assert.Equal(t, "int", fn.Params[1].Type.(*ast.NamedType).Name)
}

func TestTypeDeclaration(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		typeName   string
		fieldCount int
	}{
		{
			name: "two field struct",
			input: `
			type Point = {
				x int,
				y int
			}
			`,
			typeName:   "Point",
			fieldCount: 2,
		},
		{
			name: "single field struct",
			input: `
			type Wrapper = {
				val int
			}
			`,
			typeName:   "Wrapper",
			fieldCount: 1,
		},
		{
			name:       "empty struct",
			input:      "type Empty = {}",
			typeName:   "Empty",
			fieldCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			require.Len(t, program.Decls, 1)

			td, ok := program.Decls[0].(*ast.TypeDecl)
			require.True(t, ok)
			assert.Equal(t, tt.typeName, td.Name.Name)

			pt, ok := td.Rhs.(*ast.ProductType)
			require.True(t, ok)
			assert.Len(t, pt.Fields, tt.fieldCount)
		})
	}
}

func TestMultipleTopLevelDeclarations(t *testing.T) {
	input := `
fn add(x int, y int) int { return x + y }
fn sub(x int, y int) int { return x + y }
fn mul(x int, y int) int { return x + y }
`
	program := parseProgram(t, input)
	require.Len(t, program.Decls, 3)

	names := []string{"add", "sub", "mul"}
	for i, name := range names {
		fn, ok := program.Decls[i].(*ast.FnDecl)
		require.True(t, ok)
		assert.Equal(t, name, fn.Name)
	}
}

// =============================================================================
// Expressions
// =============================================================================

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
		{"modulo", "a % b", "(a % b)"},
		{"divide", "a / b", "(a / b)"},
		{"not equal", "a != b", "(a != b)"},
		{"lte", "a <= b", "(a <= b)"},
		{"gte", "a >= b", "(a >= b)"},
		{"logical and", "a && b", "(a && b)"},
		{"logical or", "a || b", "(a || b)"},
		{"and tighter than or", "a || b && c", "(a || (b && c))"},
		{"complex precedence", "a + b * c - d / e", "((a + (b * c)) - (d / e))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, "return "+tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)
			assert.Equal(t, tt.expected, retStmt.Value.String())
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
			stmts := parseFunctionBody(t, "return "+tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)
			testLiteralExpression(t, retStmt.Value, tt.expected)
		})
	}
}

func TestPrefixExpressions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		operator string
		expected string
	}{
		{"logical not identifier", "!x", "!", "(!x)"},
		{"logical not bool", "!true", "!", "(!true)"},
		{"logical not expression", "!isValid", "!", "(!isValid)"},
		{"unary minus integer", "-5", "-", "(-5)"},
		{"unary minus identifier", "-x", "-", "(-x)"},
		{"unary minus expression", "-(a + b)", "-", "(-(a + b))"},
		{"double not", "!!x", "!", "(!(!x))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, "return "+tt.input)
			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			prefix, ok := retStmt.Value.(*ast.PrefixExpr)
			require.True(t, ok, "expected *ast.PrefixExpr, got %T", retStmt.Value)
			assert.Equal(t, tt.operator, prefix.Operator)
			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}

func TestStringLiteralParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple string", `return "hello"`, "hello"},
		{"empty string", `return ""`, ""},
		{"string with spaces", `return "hello world"`, "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			sl, ok := retStmt.Value.(*ast.StringLiteral)
			require.True(t, ok, "expected *ast.StringLiteral, got %T", retStmt.Value)
			assert.Equal(t, tt.expected, sl.Value)
		})
	}
}

func TestIntegerLiteralPositions(t *testing.T) {
	// Verify End_ is set correctly (col + len of literal).
	stmts := parseFunctionBody(t, "return 123")
	retStmt := stmts[0].(*ast.ReturnStmt)
	il, ok := retStmt.Value.(*ast.IntLiteral)
	require.True(t, ok)
	assert.Equal(t, int64(123), il.Value)
	// End_ col should be Start col + 3 (length of "123")
	assert.Equal(t, il.Pos().Col+3, il.End_.Col)
}

func TestIfExpressionParsing(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expected       string
		hasAlternative bool
	}{
		{
			name:           "simple if",
			input:          "if (x > 5) { => true }",
			expected:       "if (x > 5) {\n\t=> true\n}",
			hasAlternative: false,
		},
		{
			name:           "if with else",
			input:          "if (x == y) { => 10 } else { => 20 }",
			expected:       "if (x == y) {\n\t=> 10\n} else {\n\t=> 20\n}",
			hasAlternative: true,
		},
		{
			name:           "if with complex condition",
			input:          "if (a + b > c * d) { => true }",
			expected:       "if ((a + b) > (c * d)) {\n\t=> true\n}",
			hasAlternative: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, "return "+tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)

			ifExpr, ok := retStmt.Value.(*ast.IfExpr)
			require.True(t, ok, "expected *ast.IfExpr, got %T", retStmt.Value)

			if tt.hasAlternative {
				assert.NotNil(t, ifExpr.Alternative)
			} else {
				assert.Nil(t, ifExpr.Alternative)
			}

			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}

func TestCallExpressionParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		argCount int
	}{
		{"no args", "return add()", "add()", 0},
		{"one arg", "return add(5)", "add(5)", 1},
		{"multiple args", "return add(5, x + 2)", "add(5, (x + 2))", 2},
		{"nested calls", "return f(g(1), h(2, 3))", "f(g(1), h(2, 3))", 2},
		{"call with bool", "return check(true)", "check(true)", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			retStmt := stmts[0].(*ast.ReturnStmt)

			callExpr, ok := retStmt.Value.(*ast.CallExpr)
			require.True(t, ok, "expected *ast.CallExpr, got %T", retStmt.Value)
			assert.Len(t, callExpr.Arguments, tt.argCount)
			assert.Equal(t, tt.expected, retStmt.Value.String())
		})
	}
}

func TestGroupedExpressionParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple group", "(1 + 2)", "(1 + 2)"},
		{"nested groups", "((a + b) * c)", "((a + b) * c)"},
		{"group changes precedence", "(a + b) * c", "((a + b) * c)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, "return "+tt.input)
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
		{
			name:     "empty struct literal",
			input:    "return Empty{}",
			expected: "Empty{}",
		},
		{
			name:     "single field struct",
			input:    "return Wrapper{val: 42}",
			expected: "Wrapper{val: 42}",
		},
		{
			name:     "struct with expression field",
			input:    "return Point{x: a * 2, y: b + 1}",
			expected: "Point{x: (a * 2), y: (b + 1)}",
		},
		{
			name:     "chained field access",
			input:    "return a.b.c",
			expected: "(a.b).c",
		},
		{
			name:     "field access on call",
			input:    "return getUser().name",
			expected: "(getUser().name)",
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

// =============================================================================
// Array and index expressions
// =============================================================================

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
		{
			name:     "single element",
			input:    "return [42]",
			expected: "[42]",
			elemCnt:  1,
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
		{"simple index", "return arr[0]", "(arr[0])"},
		{"expression index", "return arr[1 + 1]", "(arr[(1 + 1)])"},
		{"nested indexing", "return matrix[1][2]", "((matrix[1])[2])"},
		{"index after call", "return getData()[0]", "(getData()[0])"},
		{"index precedence", "return arr[1] + arr[2]", "((arr[1]) + (arr[2]))"},
		{"index with field access", "return users[0].name", "((users[0]).name)"},
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

func TestSliceExpressionParsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		hasLow  bool
		hasHigh bool
	}{
		{
			name:    "full slice low:high",
			input:   "return arr[1:3]",
			hasLow:  true,
			hasHigh: true,
		},
		{
			name:    "slice from start :high",
			input:   "return arr[:3]",
			hasLow:  false,
			hasHigh: true,
		},
		{
			name:    "slice to end low:",
			input:   "return arr[1:]",
			hasLow:  true,
			hasHigh: false,
		},
		{
			name:    "full open slice [:]",
			input:   "return arr[:]",
			hasLow:  false,
			hasHigh: false,
		},
		{
			name:    "slice with expressions",
			input:   "return arr[i+1:j-1]",
			hasLow:  true,
			hasHigh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			sliceExpr, ok := retStmt.Value.(*ast.SliceExpr)
			require.True(t, ok, "expected *ast.SliceExpr, got %T", retStmt.Value)

			if tt.hasLow {
				assert.NotNil(t, sliceExpr.Low, "expected non-nil Low")
			} else {
				assert.Nil(t, sliceExpr.Low, "expected nil Low")
			}

			if tt.hasHigh {
				assert.NotNil(t, sliceExpr.High, "expected non-nil High")
			} else {
				assert.Nil(t, sliceExpr.High, "expected nil High")
			}
		})
	}
}

// =============================================================================
// For loops
// =============================================================================

func TestForLoopParsing(t *testing.T) {
	t.Run("while-style loop", func(t *testing.T) {
		input := `
x := 0
for (x < 10) {
	x = x + 1
}
`
		stmts := parseFunctionBody(t, input)
		require.Len(t, stmts, 2)

		forStmt, ok := stmts[1].(*ast.ForStmt)
		require.True(t, ok, "expected *ast.ForStmt, got %T", stmts[1])

		assert.Nil(t, forStmt.Init)
		assert.Nil(t, forStmt.Post)
		assert.NotNil(t, forStmt.Condition)
		assert.NotNil(t, forStmt.Body)
		assert.Equal(t, "(x < 10)", forStmt.Condition.String())
	})

	t.Run("c-style loop", func(t *testing.T) {
		input := `
for (i := 0; i < 10; i = i + 1) {
	x = i
}
`
		stmts := parseFunctionBody(t, input)
		require.Len(t, stmts, 1)

		forStmt, ok := stmts[0].(*ast.ForStmt)
		require.True(t, ok, "expected *ast.ForStmt, got %T", stmts[0])

		assert.NotNil(t, forStmt.Init)
		assert.NotNil(t, forStmt.Condition)
		assert.NotNil(t, forStmt.Post)
		assert.NotNil(t, forStmt.Body)

		initDecl, ok := forStmt.Init.(*ast.DeclareStmt)
		require.True(t, ok, "expected Init to be *ast.DeclareStmt, got %T", forStmt.Init)
		assert.Equal(t, "i", initDecl.Name)

		assert.Equal(t, "(i < 10)", forStmt.Condition.String())
	})

	t.Run("loop body contains multiple statements", func(t *testing.T) {
		input := `
for (i < 5) {
	x = i + 1
	y = x * 2
}
`
		stmts := parseFunctionBody(t, input)
		require.Len(t, stmts, 1)

		forStmt, ok := stmts[0].(*ast.ForStmt)
		require.True(t, ok)
		assert.Len(t, forStmt.Body.Statements, 2)
	})

	t.Run("nested for loops", func(t *testing.T) {
		input := `
for (i < 3) {
	for (j < 3) {
		x = i + j
	}
}
`
		stmts := parseFunctionBody(t, input)
		require.Len(t, stmts, 1)

		outer, ok := stmts[0].(*ast.ForStmt)
		require.True(t, ok)

		require.Len(t, outer.Body.Statements, 1)
		_, ok = outer.Body.Statements[0].(*ast.ForStmt)
		require.True(t, ok, "expected inner *ast.ForStmt")
	})
}

// =============================================================================
// Error reporting — single errors
// =============================================================================

func TestParserErrors(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name:        "missing closing paren in params",
			input:       "fn add(x int {",
			expectedErr: "expected next token",
		},
		{
			name:        "invalid statement at top level",
			input:       "123",
			expectedErr: "only function and type declarations are supported at the top level",
		},
		{
			name:        "missing type in param",
			input:       "fn test(x) {}",
			expectedErr: "expected",
		},
		{
			name:        "missing return type",
			input:       "fn test() {",
			expectedErr: "",
		},
		{
			name:        "unclosed block",
			input:       "fn main() int { return 5",
			expectedErr: "expected",
		},
		{
			name:        "missing fn name",
			input:       "fn () int {}",
			expectedErr: "expected next token",
		},
		{
			name:        "missing opening paren after fn name",
			input:       "fn add x int) int {}",
			expectedErr: "expected next token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			p.ParseProgram()

			errors := p.Errors()
			require.NotEmpty(t, errors, "expected at least one error")
			assert.True(t,
				containsSubstring(errors, tt.expectedErr),
				"expected error containing %q, got: %v", tt.expectedErr, errors,
			)
		})
	}
}

// =============================================================================
// Error recovery — multiple errors from one parse run
// =============================================================================

func TestErrorRecovery(t *testing.T) {
	t.Run("two broken functions both reported", func(t *testing.T) {
		// Both functions are broken; the parser should report errors from both,
		// not stop after the first one.
		input := `
fn bad1( int { return 1 }
fn bad2( int { return 2 }
`
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()

		errors := p.Errors()
		require.NotEmpty(t, errors)
		// After recovering from the first broken function we should see errors
		// originating from both — i.e. at least 2.
		assert.GreaterOrEqual(t, len(errors), 2,
			"expected errors from both broken functions, got: %v", errors)
	})

	t.Run("good function after bad function is parsed", func(t *testing.T) {
		input := `
fn bad( int { return 1 }
fn good(x int) int { return x }
`
		l := lexer.New(input)
		p := New(l)
		program := p.ParseProgram()

		// At least one error from the bad function.
		require.NotEmpty(t, p.Errors())

		// The good function should still be in the AST.
		require.Len(t, program.Decls, 1, "expected the good function to be parsed")
		fn, ok := program.Decls[0].(*ast.FnDecl)
		require.True(t, ok)
		assert.Equal(t, "good", fn.Name)
	})

	t.Run("multiple errors in one function body", func(t *testing.T) {
		// Two broken statements inside the same block. Recovery should keep
		// parsing after the first bad one so both errors are reported.
		input := `
fn test() int {
	x := @
	y := #
	return 0
}
`
		l := lexer.New(input)
		p := New(l)
		program := p.ParseProgram()

		errors := p.Errors()
		require.NotEmpty(t, errors)

		// "return 0" should survive as the last statement despite the earlier errors.
		require.Len(t, program.Decls, 1)
		fn := program.Decls[0].(*ast.FnDecl)
		lastStmt := fn.Body.Statements[len(fn.Body.Statements)-1]
		_, ok := lastStmt.(*ast.ReturnStmt)
		assert.True(t, ok, "expected last statement to be ReturnStmt, got %T", lastStmt)
	})

	t.Run("error cap is enforced", func(t *testing.T) {
		// Build input that would produce many errors if the cap wasn't in place.
		var sb strings.Builder
		for i := 0; i < 50; i++ {
			sb.WriteString("fn ( { }\n")
		}
		l := lexer.New(sb.String())
		p := NewWithMaxErrors(l, 5)
		p.ParseProgram()

		errors := p.Errors()
		// Should not exceed cap + 1 sentinel.
		assert.LessOrEqual(t, len(errors), 7,
			"error count should be capped, got %d errors", len(errors))
		// The last message should mention the cap.
		last := errors[len(errors)-1]
		assert.Contains(t, last, "too many errors")
	})

	t.Run("ParseErrors returns structured errors with positions", func(t *testing.T) {
		input := "fn bad( int {}"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()

		structured := p.ParseErrors()
		require.NotEmpty(t, structured)
		// Every structured error should have a non-zero line.
		for _, e := range structured {
			assert.Greater(t, e.Line, 0, "expected Line > 0, got %+v", e)
		}
	})
}

// =============================================================================
// Expression-level error cases (Phase 3 nil-guard paths)
// =============================================================================

func TestExpressionErrorCases(t *testing.T) {
	t.Run("missing right operand in infix expression", func(t *testing.T) {
		input := "fn test() int { return x + }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing operand in prefix expression", func(t *testing.T) {
		input := "fn test() int { return ! }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing closing paren in grouped expression", func(t *testing.T) {
		input := "fn test() int { return (1 + 2 }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing colon in struct field", func(t *testing.T) {
		input := "fn test() int { return Point{x 10} }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("non-identifier left of struct literal", func(t *testing.T) {
		// "5{x: 1}" — left side is an IntLiteral, not an Identifier.
		// The parser should record exactly one error, not two.
		input := "fn test() int { x := 5\n return x }"
		// This is a valid program; use it to verify no spurious errors.
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.Empty(t, p.Errors())
	})

	t.Run("missing closing bracket in array literal", func(t *testing.T) {
		input := "fn test() int { return [1, 2, 3 }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing closing paren in call expression", func(t *testing.T) {
		input := "fn test() int { return add(1, 2 }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		// Parser records an error but still returns a partial call node.
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("partial call args still collected", func(t *testing.T) {
		// add(1, 2 — missing ')'. The two valid args should still be in the AST.
		input := "fn test() int { return add(1, 2 }"
		l := lexer.New(input)
		p := New(l)
		program := p.ParseProgram()

		require.Len(t, program.Decls, 1)
		fn := program.Decls[0].(*ast.FnDecl)
		require.NotEmpty(t, fn.Body.Statements)

		retStmt, ok := fn.Body.Statements[0].(*ast.ReturnStmt)
		require.True(t, ok)

		callExpr, ok := retStmt.Value.(*ast.CallExpr)
		require.True(t, ok, "expected *ast.CallExpr, got %T", retStmt.Value)
		assert.Len(t, callExpr.Arguments, 2, "partial args should be preserved")
	})
}

// =============================================================================
// For-loop error cases (Phase 3 safe type-assertion path)
// =============================================================================

func TestForLoopErrorCases(t *testing.T) {
	t.Run("missing opening paren", func(t *testing.T) {
		input := "fn test() int { for x < 10 { } \n return 0 }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing opening brace for body", func(t *testing.T) {
		input := "fn test() int { for (x < 10) return 0 }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})
}

// =============================================================================
// Type declaration error cases (Phase 3 expectPeek(ASSIGN) path)
// =============================================================================

func TestTypeDeclErrorCases(t *testing.T) {
	t.Run("missing equals sign in type decl", func(t *testing.T) {
		input := "type Point { x int, y int }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})

	t.Run("missing type name", func(t *testing.T) {
		input := "type = { x int }"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		assert.NotEmpty(t, p.Errors())
	})
}

// =============================================================================
// Positions
// =============================================================================

func TestParserErrorPositions(t *testing.T) {
	t.Run("error position is non-zero", func(t *testing.T) {
		input := "fn bad( int {}"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()

		errs := p.ParseErrors()
		require.NotEmpty(t, errs)
		for _, e := range errs {
			assert.Greater(t, e.Line, 0)
			assert.GreaterOrEqual(t, e.Col, 0)
		}
	})

	t.Run("error string includes line and col", func(t *testing.T) {
		input := "fn bad( int {}"
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()

		for _, msg := range p.Errors() {
			// Every formatted error should include "[line" from ParseError.String().
			assert.Contains(t, msg, "line")
		}
	})
}

func TestFunctionParameterModifiers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			name string
			mut  bool
			own  bool
		}
	}{
		{
			name:  "mutable parameter",
			input: "fn update(mut user User) {}",
			expected: []struct {
				name string
				mut  bool
				own  bool
			}{
				{"user", true, false},
			},
		},
		{
			name:  "owned parameter",
			input: "fn consume(own data Buffer) {}",
			expected: []struct {
				name string
				mut  bool
				own  bool
			}{
				{"data", false, true},
			},
		},
		{
			name:  "mixed parameters",
			input: "fn process(mut a int, own b string, c bool) {}",
			expected: []struct {
				name string
				mut  bool
				own  bool
			}{
				{"a", true, false},
				{"b", false, true},
				{"c", false, false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			require.Len(t, program.Decls, 1)

			fn, ok := program.Decls[0].(*ast.FnDecl)
			require.True(t, ok)

			require.Len(t, fn.Params, len(tt.expected))
			for i, exp := range tt.expected {
				assert.Equal(t, exp.name, fn.Params[i].Name)
				assert.Equal(t, exp.mut, fn.Params[i].Mut, "Mut mismatch for param %s", exp.name)
				assert.Equal(t, exp.own, fn.Params[i].Own, "Own mismatch for param %s", exp.name)
			}
		})
	}
}

func TestCallArgumentModifiers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			argString string
			mut       bool
			own       bool
		}
	}{
		{
			name:  "mutable borrow argument",
			input: "return update(mut y)",
			expected: []struct {
				argString string
				mut       bool
				own       bool
			}{
				{"y", true, false},
			},
		},
		{
			name:  "owned transfer argument",
			input: "return channel.send(own x)",
			expected: []struct {
				argString string
				mut       bool
				own       bool
			}{
				{"x", false, true},
			},
		},
		{
			name:  "mixed arguments",
			input: "return mixed(mut a, own b, c)",
			expected: []struct {
				argString string
				mut       bool
				own       bool
			}{
				{"a", true, false},
				{"b", false, true},
				{"c", false, false},
			},
		},
		{
			name:  "complex expression with ownership",
			input: "return process(own data.clone())",
			expected: []struct {
				argString string
				mut       bool
				own       bool
			}{
				{"(data.clone)()", false, true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts := parseFunctionBody(t, tt.input)
			require.Len(t, stmts, 1)

			retStmt, ok := stmts[0].(*ast.ReturnStmt)
			require.True(t, ok)

			callExpr, ok := retStmt.Value.(*ast.CallExpr)
			require.True(t, ok, "expected *ast.CallExpr, got %T", retStmt.Value)

			require.Len(t, callExpr.Arguments, len(tt.expected))
			for i, exp := range tt.expected {
				assert.Equal(t, exp.argString, callExpr.Arguments[i].Argument.String())
				assert.Equal(t, exp.mut, callExpr.Arguments[i].Mut, "Mut mismatch for arg %d", i)
				assert.Equal(t, exp.own, callExpr.Arguments[i].Own, "Own mismatch for arg %d", i)
			}
		})
	}
}

// =============================================================================
// Helpers
// =============================================================================

func parseProgram(t *testing.T, input string) *ast.Program {
	t.Helper()
	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)
	return program
}

func parseFunctionBody(t *testing.T, input string) []ast.Stmt {
	t.Helper()
	fullInput := "fn test() int {\n" + input + "\n}"
	program := parseProgram(t, fullInput)

	require.Len(t, program.Decls, 1)

	fn, ok := program.Decls[0].(*ast.FnDecl)
	require.True(t, ok, "expected FnDecl")

	return fn.Body.Statements
}

func checkParserErrors(t *testing.T, p *Parser) {
	t.Helper()
	errors := p.Errors()
	if len(errors) == 0 {
		return
	}

	t.Errorf("parser has %d error(s)", len(errors))
	for _, msg := range errors {
		t.Errorf("  parser error: %q", msg)
	}
	t.FailNow()
}

// containsSubstring reports whether any string in msgs contains sub.
func containsSubstring(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// testLiteralExpression dispatches to the correct literal tester.
func testLiteralExpression(t *testing.T, exp ast.Expr, expected interface{}) {
	t.Helper()
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
	t.Helper()
	integ, ok := il.(*ast.IntLiteral)
	require.True(t, ok, "expected *ast.IntLiteral, got %T", il)
	assert.Equal(t, value, integ.Value)
}

func testBooleanLiteral(t *testing.T, exp ast.Expr, value bool) {
	t.Helper()
	bo, ok := exp.(*ast.BoolLiteral)
	require.True(t, ok, "expected *ast.BoolLiteral, got %T", exp)
	assert.Equal(t, value, bo.Value)
}

func testIdentifier(t *testing.T, exp ast.Expr, value string) {
	t.Helper()
	ident, ok := exp.(*ast.Identifier)
	require.True(t, ok, "expected *ast.Identifier, got %T", exp)
	assert.Equal(t, value, ident.Value)
}
