package ownership

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/parser"
	"github.com/mattcarp12/maml/internal/sema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func analyzeOwnership(t *testing.T, input string) []ast.CompileError {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	// Generate the prerequisite type tracking map cleanly first
	semaAnalyzer := sema.New()
	_, typeMap := semaAnalyzer.Analyze(program)

	ownershipAnalyzer := New(typeMap)
	return ownershipAnalyzer.Analyze(program)
}

// TestOwnershipAndBorrowing covers the full ownership model implemented in phases 2-4.
func TestOwnershipAndBorrowing(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		// -------------------------------------------------------------------------
		// Immutability by default
		// -------------------------------------------------------------------------
		{
			name: "immutable variable cannot be reassigned",
			input: `
			fn main() int {
				x := 10
				x = 20
				return x
			}`,
			expectedErr: "cannot mutate immutable variable 'x'",
		},
		{
			name: "mutable variable can be reassigned",
			input: `
			fn main() int {
				mut x := 10
				x = 20
				return x
			}`,
		},

		// -------------------------------------------------------------------------
		// Mutable acquisition requires mutable source
		// -------------------------------------------------------------------------
		{
			name: "cannot acquire mutable ownership of immutable variable",
			input: `
			fn main() int {
				x := 10
				mut y := x
				return y
			}`,
			expectedErr: "cannot acquire mutable ownership of immutable variable 'x'",
		},
		{
			name: "mutable acquisition from mutable source transfers ownership",
			input: `
			fn main() int {
				mut x := 10
				mut y := x
				return y
			}`,
		},
		{
			name: "use of moved variable after mutable acquisition",
			input: `
			fn main() int {
				mut x := 10
				mut y := x
				return x
			}`,
			expectedErr: "use of moved variable 'x'",
		},
		{
			name: "moved variable cannot be reassigned",
			input: `
			fn main() int {
				mut x := 10
				mut y := x
				x = 5
				return y
			}`,
			expectedErr: "use of moved variable 'x'",
		},
		{
			name: "multiple mutable borrows",
			input: `
			fn f(mut x int) int { return x }
			fn main() int {
				mut x := 10

				f(mut x)
				f(mut x)
				return x
			}`,
		},

		// -------------------------------------------------------------------------
		// Immutable aliasing
		// -------------------------------------------------------------------------
		{
			name: "immutable value may be freely aliased",
			input: `
			fn main() int {
				x := 10
				a := x
				b := x
				return a
			}`,
		},
		{
			name: "cannot acquire mutable ownership of aliased immutable value",
			input: `
			fn main() int {
				x := 10
				a := x
				mut c := x
				return c
			}`,
			expectedErr: "cannot acquire mutable ownership of immutable variable 'x'",
		},

		// -------------------------------------------------------------------------
		// Use-after-move in expressions
		// -------------------------------------------------------------------------
		{
			name: "use of moved variable in infix expression",
			input: `
			fn main() int {
				mut x := 10
				mut y := x
				return x + 1
			}`,
			expectedErr: "use of moved variable 'x'",
		},

		// -------------------------------------------------------------------------
		// fn f(x T) — immutable borrow parameter
		// -------------------------------------------------------------------------
		{
			name: "immutable param accepts immutable variable",
			input: `
			fn read(x int) int {
				return x
			}
			fn main() int {
				val := 42
				return read(val)
			}`,
		},
		{
			name: "immutable param accepts mutable variable",
			input: `
			fn read(x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return read(val)
			}`,
		},
		{
			name: "immutable param rejects mut at call site",
			input: `
			fn read(x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return read(mut val)
			}`,
			expectedErr: "parameter is an immutable borrow, remove 'mut' at call site",
		},
		{
			name: "immutable param rejects own at call site",
			input: `
			fn read(x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return read(own val)
			}`,
			expectedErr: "parameter is an immutable borrow, remove 'own' at call site",
		},
		{
			name: "immutable borrow does not invalidate caller binding",
			input: `
			fn read(x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				read(val)
				return val
			}`,
		},

		// -------------------------------------------------------------------------
		// fn f(mut x T) — mutable borrow parameter
		// -------------------------------------------------------------------------
		{
			name: "mut param requires mut at call site",
			input: `
			fn update(mut x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return update(val)
			}`,
			expectedErr: "call site must pass with 'mut'",
		},
		{
			name: "mut param rejects own at call site",
			input: `
			fn update(mut x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return update(own val)
			}`,
			expectedErr: "parameter is a mutable borrow, use 'mut' not 'own' at call site",
		},
		{
			name: "mut param rejects immutable source",
			input: `
			fn update(mut x int) int {
				return x
			}
			fn main() int {
				val := 42
				return update(mut val)
			}`,
			expectedErr: "cannot mutably borrow immutable variable 'val'",
		},
		{
			name: "mut borrow does not transfer ownership",
			input: `
			fn update(mut x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				update(mut val)
				return val
			}`,
		},
		{
			name: "valid mut borrow of mutable variable",
			input: `
			fn update(mut x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return update(mut val)
			}`,
		},

		// -------------------------------------------------------------------------
		// fn f(own x T) — ownership transfer parameter
		// -------------------------------------------------------------------------
		{
			name: "own param requires own at call site",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return consume(val)
			}`,
			expectedErr: "parameter is declared 'own', call site must pass with 'own'",
		},
		{
			name: "own param rejects mut at call site",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return consume(mut val)
			}`,
			expectedErr: "parameter requires ownership transfer, use 'own' not 'mut' at call site",
		},
		{
			name: "own transfer of mutable source invalidates caller binding",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				consume(own val)
				return val
			}`,
			expectedErr: "use of moved variable 'val'",
		},
		{
			name: "own transfer of immutable source invalidates caller binding",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				val := 42
				consume(own val)
				return val
			}`,
			expectedErr: "use of moved variable 'val'",
		},
		{
			name: "own transfer of non-identifier is an error",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				return consume(own 42)
			}`,
			expectedErr: "'own' can only transfer ownership of a named variable",
		},
		{
			name: "valid own transfer of mutable variable",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				return consume(own val)
			}`,
		},
		{
			name: "valid own transfer of immutable variable",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				val := 42
				return consume(own val)
			}`,
		},
		{
			name: "cannot transfer already-moved variable",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				mut val := 42
				consume(own val)
				return consume(own val)
			}`,
			expectedErr: "use of moved variable 'val'",
		},

		// -------------------------------------------------------------------------
		// Alias cleanup on scope exit
		// -------------------------------------------------------------------------
		{
			name: "alias going out of scope allows mutable acquisition in outer scope",
			input: `
			fn main() int {
				mut x := 10
				if (true) {
					a := x
				}
				mut y := x
				return y
			}`,
		},
		{
			name: "own transfer of unique immutable variable is allowed",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				x := 42
				return consume(own x)
			}`,
		},
		{
			name: "own transfer of shared immutable variable is illegal",
			input: `
			fn consume(own x int) int {
				return x
			}
			fn main() int {
				x := 42
				a := x
				return consume(own x)
			}`,
			expectedErr: "cannot transfer ownership of 'x': value has active immutable aliases",
		},
		{
			name: "sum type variable is immutable by default",
			input: `
			type Toggle =
				| On
				| Off

			fn main() int {
				t := On
				t = Off
				return 0
			}`,
			expectedErr: "cannot mutate immutable variable 't'",
		},
		{
			name: "sum type passed to own parameter invalidates source",
			input: `
			type Toggle =
				| On
				| Off

			fn consume(own t Toggle) int {
				return 0
			}

			fn main() int {
				mut t := On
				consume(own t)
				return match t {
					case On { => 1 }
					case Off { => 0 }
				}
			}`,
			expectedErr: "use of moved variable 't'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzeOwnership(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}

func TestMutabilityEnforcement(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "mutable variable can be reassigned",
			input: `
			fn main() int {
				mut x := 5
				x = 10
				return x
			}`,
		},
		// TODO - uncomment this after implementing char data types
		// {
		// 	name: "string index assignment forbidden",
		// 	input: `
		// 	fn main() int {
		// 		s := "hello"
		// 		s[0] = 'a'
		// 		return 0
		// 	}`,
		// 	expectedErr: "strings are immutable and cannot be modified by index",
		// },
		{
			name: "mutable array element assignment",
			input: `
			fn main() int {
				mut arr := [1, 2, 3]
				arr[1] = 99
				return 0
			}`,
		},
		{
			name: "mutable struct field assignment",
			input: `
			type Point = { x int, y int }
			fn main() int {
				mut p := Point{x: 1, y: 2}
				p.x = 10
				return 0
			}`,
		},
		{
			name: "assign to literal should fail",
			input: `
			fn main() int {
				5 = 10
				return 0
			}`,
			expectedErr: "cannot assign to non-variable expression",
		},
		{
			name: "reassigning immutable variable",
			input: `
			fn main() int {
				x := 5
				x = 10
				return x
			}`,
			expectedErr: "cannot mutate immutable variable 'x'",
		},
		{
			name: "immutable array cannot be mutated via index",
			input: `
			fn main() int {
				arr := [1, 2, 3]
				arr[1] = 99
				return 0
			}`,
			expectedErr: "cannot mutate immutable variable 'arr'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzeOwnership(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}
