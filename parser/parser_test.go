package parser

import (
	"fmt"
	"testing"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
)

// -----------------------------------------------------------------------------
// Core Helpers
// -----------------------------------------------------------------------------

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

// parseFunctionBody is a critical helper. Since MAML requires statements to be
// inside a function, this wraps our test inputs in a dummy function and extracts
// the body so we can test statements directly.
func parseFunctionBody(t *testing.T, input string) []ast.Stmt {
	fullInput := "fn test() int {\n" + input + "\n}"
	l := lexer.New(fullInput)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Decls) != 1 {
		t.Fatalf("program.Decls does not contain 1 declaration. got=%d", len(program.Decls))
	}

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("decl is not FnDecl. got=%T", program.Decls[0])
	}

	return fn.Body.Statements
}

// -----------------------------------------------------------------------------
// Statement Tests
// -----------------------------------------------------------------------------

func TestDeclareStatements(t *testing.T) {
	tests := []struct {
		input         string
		expectedName  string
		expectedMut   bool
		expectedValue interface{}
	}{
		{"x := 5", "x", false, 5},
		{"y ~= 10", "y", true, 10},
		{"foobar := y", "foobar", false, "y"},
	}

	for _, tt := range tests {
		stmts := parseFunctionBody(t, tt.input)
		if len(stmts) != 1 {
			t.Fatalf("expected 1 statement, got %d", len(stmts))
		}

		stmt := stmts[0]
		declStmt, ok := stmt.(*ast.DeclareStmt)
		if !ok {
			t.Fatalf("stmt is not ast.DeclareStmt. got=%T", stmt)
		}

		if declStmt.Name != tt.expectedName {
			t.Errorf("declStmt.Name not '%s'. got=%s", tt.expectedName, declStmt.Name)
		}
		if declStmt.Mutable != tt.expectedMut {
			t.Errorf("declStmt.Mutable not '%t'. got=%t", tt.expectedMut, declStmt.Mutable)
		}

		testLiteralExpression(t, declStmt.Value, tt.expectedValue)
	}
}

func TestReturnStatements(t *testing.T) {
	tests := []struct {
		input         string
		expectedValue interface{}
	}{
		{"=> 5", 5},
		{"=> x", "x"},
	}

	for _, tt := range tests {
		stmts := parseFunctionBody(t, tt.input)
		if len(stmts) != 1 {
			t.Fatalf("expected 1 statement, got %d", len(stmts))
		}

		returnStmt, ok := stmts[0].(*ast.ReturnStmt)
		if !ok {
			t.Fatalf("stmt is not ast.ReturnStmt. got=%T", stmts[0])
		}

		testLiteralExpression(t, returnStmt.Value, tt.expectedValue)
	}
}

// -----------------------------------------------------------------------------
// Top-Level Declaration Tests
// -----------------------------------------------------------------------------

func TestFunctionDeclarations(t *testing.T) {
	input := `fn add() int { => 5 }`

	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Decls) != 1 {
		t.Fatalf("program.Decls does not contain 1 declaration. got=%d", len(program.Decls))
	}

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("decl is not FnDecl. got=%T", program.Decls[0])
	}

	if fn.Name != "add" {
		t.Errorf("fn.Name not 'add'. got=%s", fn.Name)
	}

	returnType, ok := fn.ReturnType.(*ast.NamedType)
	if !ok {
		t.Fatalf("fn.ReturnType is not NamedType. got=%T", fn.ReturnType)
	}
	if returnType.Name != "int" {
		t.Errorf("returnType.Name not 'int'. got=%s", returnType.Name)
	}

	if len(fn.Body.Statements) != 1 {
		t.Fatalf("fn.Body.Statements has wrong length. got=%d", len(fn.Body.Statements))
	}
}

// -----------------------------------------------------------------------------
// Expression & Math Tests (Pratt Parser Validation)
// -----------------------------------------------------------------------------

func TestOperatorPrecedenceParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			"a + b",
			"(a + b)",
		},
		{
			"a + b + c",
			"((a + b) + c)",
		},
		{
			"a + b - c",
			"((a + b) - c)",
		},
		// NOTE: Once you add multiplication (*), division (/), and parenthesis ()
		// to your infix/prefix maps, you can uncomment these to test them!
		// {
		// 	"a + b * c",
		// 	"(a + (b * c))",
		// },
		// {
		// 	"a * b + c",
		// 	"((a * b) + c)",
		// },
		// {
		// 	"5 + (2 * 3)",
		// 	"(5 + (2 * 3))",
		// },
	}

	for _, tt := range tests {
		// stmts := parseFunctionBody(t, tt.input)
		// We expect the math to be parsed as a floating expression (which currently causes an error
		// in our strict stmt block because we only allow := and =>).
		// For exhaustive testing, you usually create a dummy "ExpressionStatement" in the AST
		// just so floating math can exist, OR we wrap it in a return statement for this test:

		inputWrapped := fmt.Sprintf("=> %s", tt.input)
		stmtsWrapped := parseFunctionBody(t, inputWrapped)

		retStmt := stmtsWrapped[0].(*ast.ReturnStmt)
		actual := retStmt.Value.String()

		if actual != tt.expected {
			t.Errorf("expected=%q, got=%q", tt.expected, actual)
		}
	}
}

// -----------------------------------------------------------------------------
// Expression Type Assertions (The "SOTA" Generic Checkers)
// -----------------------------------------------------------------------------

func testLiteralExpression(t *testing.T, exp ast.Expr, expected interface{}) bool {
	switch v := expected.(type) {
	case int:
		return testIntegerLiteral(t, exp, int64(v))
	case int64:
		return testIntegerLiteral(t, exp, v)
	case string:
		return testIdentifier(t, exp, v)
	}
	t.Errorf("type of exp not handled. got=%T", exp)
	return false
}

func testIntegerLiteral(t *testing.T, il ast.Expr, value int64) bool {
	integ, ok := il.(*ast.IntLiteral)
	if !ok {
		t.Errorf("il not *ast.IntLiteral. got=%T", il)
		return false
	}
	if integ.Value != value {
		t.Errorf("integ.Value not %d. got=%d", value, integ.Value)
		return false
	}
	return true
}

func testIdentifier(t *testing.T, exp ast.Expr, value string) bool {
	ident, ok := exp.(*ast.Identifier)
	if !ok {
		t.Errorf("exp not *ast.Identifier. got=%T", exp)
		return false
	}
	if ident.Value != value {
		t.Errorf("ident.Value not %s. got=%s", value, ident.Value)
		return false
	}
	return true
}
