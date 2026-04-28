package parser

import (
	"testing"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
)

func TestDeclareStatements(t *testing.T) {
	tests := []struct {
		input       string
		expectedId  string
		expectedMut bool
	}{
		{"x := 5", "x", false},
		{"y ~= 10", "y", true},
		{"foobar := 838383", "foobar", false},
	}

	for _, tt := range tests {
		l := lexer.New(tt.input)
		p := New(l)
		program := p.ParseProgram()
		checkParserErrors(t, p)

		if program == nil {
			t.Fatalf("ParseProgram() returned nil")
		}
		if len(program.Declarations) != 1 {
			t.Fatalf("program.Declarations does not contain 1 statements. got=%d",
				len(program.Declarations))
		}

		stmt := program.Declarations[0]
		if !testDeclareStatement(t, stmt, tt.expectedId, tt.expectedMut) {
			return
		}
	}
}

// Helper to assert the fields of a DeclareStmt
func testDeclareStatement(t *testing.T, s ast.Declaration, name string, isMut bool) bool {
	decl, ok := s.(*ast.DeclareStmt)
	if !ok {
		t.Errorf("s not *ast.DeclareStmt. got=%T", s)
		return false
	}

	if decl.Name != name {
		t.Errorf("decl.Name not '%s'. got=%s", name, decl.Name)
		return false
	}

	if decl.Mutable != isMut {
		t.Errorf("decl.Mutable not '%t'. got=%t", isMut, decl.Mutable)
		return false
	}

	// We also want to ensure TokenLiteral() returns the identifier name
	if decl.TokenLiteral() != name {
		t.Errorf("decl.TokenLiteral() not '%s'. got=%s", name, decl.TokenLiteral())
		return false
	}

	return true
}

// Helper to catch syntax errors
func checkParserErrors(t *testing.T, p *Parser) {
	errors := p.errors
	if len(errors) == 0 {
		return
	}

	t.Errorf("parser has %d errors", len(errors))
	for _, msg := range errors {
		t.Errorf("parser error: %q", msg)
	}
	t.FailNow()
}

func TestIllegalTopLevelStatements(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"x = 5"}, // Update statement at root
		{"5 + 5"}, // Expression at root
		{"x"},     // Bare identifier at root
	}

	for _, tt := range tests {
		l := lexer.New(tt.input)
		p := New(l)
		
		p.ParseProgram()

		if len(p.errors) == 0 {
			t.Errorf("expected parser to have errors for input %q, but got none", tt.input)
		}
	}
}

func TestDeclareStatementsWithValue(t *testing.T) {
	input := `
		x := 5
		y ~= 10
	`
	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Declarations) != 2 {
		t.Fatalf("expected 2 declarations, got %d", len(program.Declarations))
	}

	// Test x := 5
	stmt1 := program.Declarations[0].(*ast.DeclareStmt)
	if stmt1.Name != "x" || stmt1.Mutable != false {
		t.Errorf("stmt1 incorrect. got=%+v", stmt1)
	}
	val1, ok := stmt1.Value.(*ast.IntLiteral)
	if !ok || val1.Value != 5 {
		t.Errorf("stmt1.Value not IntLiteral with value 5. got=%T", stmt1.Value)
	}

	// Test y ~= 10
	stmt2 := program.Declarations[1].(*ast.DeclareStmt)
	if stmt2.Name != "y" || stmt2.Mutable != true {
		t.Errorf("stmt2 incorrect. got=%+v", stmt2)
	}
	val2, ok := stmt2.Value.(*ast.IntLiteral)
	if !ok || val2.Value != 10 {
		t.Errorf("stmt2.Value not IntLiteral with value 10. got=%T", stmt2.Value)
	}
}