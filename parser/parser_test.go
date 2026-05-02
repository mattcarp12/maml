package parser

import (
	"testing"

	"github.com/mattcarp12/maml/ast"
	"github.com/mattcarp12/maml/lexer"
)

// Helper to catch and print parser errors
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

func TestDeclareStatements(t *testing.T) {
	input := `
	x := 20
	y ~= 22
	`

	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if program == nil {
		t.Fatalf("ParseProgram() returned nil")
	}
	if len(program.Statements) != 2 {
		t.Fatalf("program.Statements does not contain 2 statements. got=%d", len(program.Statements))
	}

	tests := []struct {
		expectedIdentifier string
		expectedMutable    bool
	}{
		{"x", false},
		{"y", true},
	}

	for i, tt := range tests {
		stmt := program.Statements[i]
		testDeclareStatement(t, stmt, tt.expectedIdentifier, tt.expectedMutable)
	}
}

func testDeclareStatement(t *testing.T, s ast.Statement, name string, mutable bool) bool {
	declStmt, ok := s.(*ast.DeclareStatement)
	if !ok {
		t.Errorf("s not *ast.DeclareStatement. got=%T", s)
		return false
	}

	if declStmt.Name != name {
		t.Errorf("declStmt.Name not '%s'. got=%s", name, declStmt.Name)
		return false
	}

	if declStmt.Mutable != mutable {
		t.Errorf("declStmt.Mutable not '%t'. got=%t", mutable, declStmt.Mutable)
		return false
	}

	return true
}

func TestReturnStatements(t *testing.T) {
	input := `
	=> 5
	=> x
	`

	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Statements) != 2 {
		t.Fatalf("program.Statements does not contain 2 statements. got=%d", len(program.Statements))
	}

	for _, stmt := range program.Statements {
		returnStmt, ok := stmt.(*ast.ReturnStatement)
		if !ok {
			t.Errorf("stmt not *ast.ReturnStatement. got=%T", stmt)
			continue
		}
		if returnStmt.TokenLiteral() != "return" {
			t.Errorf("returnStmt.TokenLiteral not 'return', got %q", returnStmt.TokenLiteral())
		}
	}
}

func TestParsingInfixExpressions(t *testing.T) {
	infixTests := []struct {
		input      string
		leftValue  string
		operator   string
		rightValue string
	}{
		{"x + y", "x", "+", "y"},
		{"5 + 5", "5", "+", "5"},
	}

	for _, tt := range infixTests {
		l := lexer.New(tt.input)
		p := New(l)
		expression := p.parseExpression(LOWEST)
		checkParserErrors(t, p)

		if expression == nil {
			t.Fatalf("Failed to parse expression: %s", tt.input)
		}

		infixExp, ok := expression.(*ast.InfixExpression)
		if !ok {
			t.Fatalf("expression is not *ast.InfixExpression. got=%T", expression)
		}

		if infixExp.Operator != tt.operator {
			t.Errorf("Operator is not '%s'. got=%s", tt.operator, infixExp.Operator)
		}

	}
}

// The Grand Finale: Testing the Tracer Bullet
func TestTracerBulletProgram(t *testing.T) {
	input := `
	fn main() int {
		x := 20
		y := 22
		=> x + y
	}
	`

	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Statements) != 1 {
		t.Fatalf("program.Statements does not contain 1 statements. got=%d", len(program.Statements))
	}

	funcDecl, ok := program.Statements[0].(*ast.FunctionDecl)
	if !ok {
		t.Fatalf("program.Statements[0] is not ast.FunctionDecl. got=%T", program.Statements[0])
	}

	if funcDecl.Name != "main" {
		t.Errorf("funcDecl.Name is not 'main'. got=%s", funcDecl.Name)
	}

	if funcDecl.ReturnType != "int" {
		t.Errorf("funcDecl.ReturnType is not 'int'. got=%s", funcDecl.ReturnType)
	}

	if len(funcDecl.Body.Statements) != 3 {
		t.Fatalf("funcDecl.Body does not contain 3 statements. got=%d", len(funcDecl.Body.Statements))
	}

	// 1. x := 20
	testDeclareStatement(t, funcDecl.Body.Statements[0], "x", false)

	// 2. y := 22
	testDeclareStatement(t, funcDecl.Body.Statements[1], "y", false)

	// 3. => x + y
	retStmt, ok := funcDecl.Body.Statements[2].(*ast.ReturnStatement)
	if !ok {
		t.Fatalf("statements[2] is not ReturnStatement. got=%T", funcDecl.Body.Statements[2])
	}

	infix, ok := retStmt.Value.(*ast.InfixExpression)
	if !ok {
		t.Fatalf("retStmt.Value is not InfixExpression. got=%T", retStmt.Value)
	}

	if infix.Operator != "+" {
		t.Errorf("infix.Operator is not '+'. got=%s", infix.Operator)
	}
}
