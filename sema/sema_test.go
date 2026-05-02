package sema

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/lexer"
	"github.com/mattcarp12/maml/parser"
)

// testHelper parses the input and runs the semantic analyzer.
// It explicitly fails the test if the *parser* has errors, to ensure
// we are actually testing semantic logic and not broken syntax.
func analyzeInput(t *testing.T, input string) []string {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser encountered errors, fix syntax first:\n%s", strings.Join(p.Errors(), "\n"))
	}

	analyzer := New()
	return analyzer.Analyze(program)
}

func TestValidPrograms(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "Basic declarations and math",
			input: `
			fn main() int {
				x := 5
				y := 10
				=> x + y
			}`,
		},
		{
			name: "Function calling (Pass 1 Resolution)",
			input: `
			fn helper() int {
				=> 42
			}
			fn main() int {
				// We haven't implemented function call AST nodes yet, 
				// but this ensures multiple functions parse without scope collision.
				x := 5
				=> x
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzeInput(t, tt.input)
			if len(errors) > 0 {
				t.Errorf("Expected 0 semantic errors, got %d:\n%s", len(errors), strings.Join(errors, "\n"))
			}
		})
	}
}

func TestVariableShadowing(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedError string
	}{
		{
			name: "Duplicate immutable declaration",
			input: `
			fn main() int {
				x := 5
				x := 10
			}`,
			expectedError: "variable 'x' is already declared in this block",
		},
		{
			name: "Duplicate mixed declaration",
			input: `
			fn main() int {
				y ~= 5
				y := 10
			}`,
			expectedError: "variable 'y' is already declared in this block",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzeInput(t, tt.input)
			if len(errors) == 0 {
				t.Fatalf("Expected a semantic error, but got none")
			}

			if !strings.Contains(errors[0], tt.expectedError) {
				t.Errorf("Expected error containing %q, got %q", tt.expectedError, errors[0])
			}
		})
	}
}

func TestUndefinedVariables(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedError string
	}{
		{
			name: "Returning an undeclared variable",
			input: `
			fn main() int {
				=> x
			}`,
			expectedError: "undefined variable 'x'",
		},
		{
			name: "Using undeclared variable in math",
			input: `
			fn main() int {
				y := 10
				=> x + y
			}`,
			expectedError: "undefined variable 'x'",
		},
		{
			name: "Assigning an undeclared variable to another",
			input: `
			fn main() int {
				a := b
			}`,
			expectedError: "undefined variable 'b'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzeInput(t, tt.input)
			if len(errors) == 0 {
				t.Fatalf("Expected a semantic error, but got none")
			}

			if !strings.Contains(errors[0], tt.expectedError) {
				t.Errorf("Expected error containing %q, got %q", tt.expectedError, errors[0])
			}
		})
	}
}

func TestScopeIsolation(t *testing.T) {
	// This proves that a variable declared in one function
	// does not "leak" into the global scope or another function.
	input := `
	fn first() int {
		secret := 42
		=> secret
	}

	fn second() int {
		=> secret
	}
	`

	errors := analyzeInput(t, input)
	if len(errors) == 0 {
		t.Fatalf("Expected a semantic error, but got none")
	}

	expectedError := "undefined variable 'secret'"
	found := false
	for _, err := range errors {
		if strings.Contains(err, expectedError) {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected error containing %q, got:\n%s", expectedError, strings.Join(errors, "\n"))
	}
}

// NOTE: Once the parser supports Strings or Booleans, we will add a
// TestTypeMismatch suite here to verify that `5 + "hello"` throws an error.
