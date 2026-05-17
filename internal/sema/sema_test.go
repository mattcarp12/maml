package sema

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzer_Analyze(t *testing.T) {
	tests := []struct {
		name        string
		program     *ast.Program
		wantErrors  []string
		wantTypeMap map[string]Type // simplified key for expected types (e.g., var name or expr desc)
	}{
		{
			name:       "empty program",
			program:    &ast.Program{Decls: []ast.Decl{}},
			wantErrors: nil,
		},
		{
			name: "simple function with return",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ReturnStmt{
									Value: &ast.IntLiteral{Value: 42},
								},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			name: "function missing return",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{},
						},
					},
				},
			},
			wantErrors: []string{"function 'main' is missing a return statement"},
		},
		{
			name: "type mismatch in return",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ReturnStmt{
									Value: &ast.BoolLiteral{Value: true},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"type mismatch: expected return type 'int', got 'bool'"},
		},
		{
			name: "variable declaration and use",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.DeclareStmt{
									Name:    "x",
									Mutable: false,
									Value:   &ast.IntLiteral{Value: 10},
								},
								&ast.ReturnStmt{
									Value: &ast.Identifier{Value: "x"},
								},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			name: "undefined variable",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ReturnStmt{
									Value: &ast.Identifier{Value: "undefined"},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"undefined name 'undefined'"},
		},
		{
			name: "redeclaration error",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.DeclareStmt{
									Name:  "x",
									Value: &ast.IntLiteral{Value: 1},
								},
								&ast.DeclareStmt{
									Name:  "x",
									Value: &ast.IntLiteral{Value: 2},
								},
								&ast.ReturnStmt{
									Value: &ast.Identifier{Value: "x"},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"variable 'x' is already declared"},
		},
		{
			name: "infix type mismatch",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ReturnStmt{
									Value: &ast.InfixExpr{
										Left:     &ast.IntLiteral{Value: 1},
										Operator: "+",
										Right:    &ast.BoolLiteral{Value: true},
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"type mismatch: cannot + 'int' and 'bool'"},
		},
		{
			name: "if condition must be bool",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ReturnStmt{
									Value: &ast.IfExpr{
										Condition:   &ast.IntLiteral{Value: 1},
										Consequence: &ast.BlockStmt{},
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"IF condition must be a boolean"},
		},
		{
			name: "builtin puts call",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ExprStmt{
									Value: &ast.CallExpr{
										Function: &ast.Identifier{Value: "puts"},
										Arguments: []ast.CallArg{
											{Argument: &ast.StringLiteral{Value: "hello"}},
										},
									},
								},
								&ast.ReturnStmt{Value: &ast.IntLiteral{Value: 0}},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			name: "wrong arity call",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.ExprStmt{
									Value: &ast.CallExpr{
										Function:  &ast.Identifier{Value: "puts"},
										Arguments: []ast.CallArg{},
									},
								},
								&ast.ReturnStmt{Value: &ast.IntLiteral{Value: 0}},
							},
						},
					},
				},
			},
			wantErrors: []string{"wrong number of arguments: expected 1, got 0"},
		},
		{
			name: "struct definition and literal",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.TypeDecl{
						Name: &ast.NamedType{Name: "Point"},
						Rhs: &ast.ProductType{
							Fields: []ast.Param{
								{Name: "x", Type: &ast.NamedType{Name: "int"}},
								{Name: "y", Type: &ast.NamedType{Name: "int"}},
							},
						},
					},
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.DeclareStmt{
									Name: "p",
									Value: &ast.StructLiteral{
										Type: &ast.Identifier{Value: "Point"},
										Fields: []ast.StructField{
											{Name: &ast.Identifier{Value: "x"}, Value: &ast.IntLiteral{Value: 1}},
											{Name: &ast.Identifier{Value: "y"}, Value: &ast.IntLiteral{Value: 2}},
										},
									},
								},
								&ast.ReturnStmt{Value: &ast.IntLiteral{Value: 0}},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			name: "unknown type in struct field",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.TypeDecl{
						Name: &ast.NamedType{Name: "Bad"},
						Rhs: &ast.ProductType{
							Fields: []ast.Param{
								{Name: "x", Type: &ast.NamedType{Name: "unknownType"}},
							},
						},
					},
				},
			},
			wantErrors: []string{"unknown type unknownType"},
		},
		{
			name: "field access on struct",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.TypeDecl{
						Name: &ast.NamedType{Name: "Point"},
						Rhs: &ast.ProductType{
							Fields: []ast.Param{{Name: "x", Type: &ast.NamedType{Name: "int"}}},
						},
					},
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.DeclareStmt{
									Name: "p",
									Value: &ast.StructLiteral{
										Type:   &ast.Identifier{Value: "Point"},
										Fields: []ast.StructField{{Name: &ast.Identifier{Value: "x"}, Value: &ast.IntLiteral{Value: 10}}},
									},
								},
								&ast.ReturnStmt{
									Value: &ast.FieldAccess{
										Object: &ast.Identifier{Value: "p"},
										Field:  &ast.Identifier{Value: "x"},
									},
								},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			name: "field access on non-struct",
			program: &ast.Program{
				Decls: []ast.Decl{
					&ast.FnDecl{
						Name:       "main",
						Params:     []ast.Param{},
						ReturnType: &ast.NamedType{Name: "int"},
						Body: &ast.BlockStmt{
							Statements: []ast.Stmt{
								&ast.DeclareStmt{
									Name:  "x",
									Value: &ast.IntLiteral{Value: 10},
								},
								&ast.ReturnStmt{
									Value: &ast.FieldAccess{
										Object: &ast.Identifier{Value: "x"},
										Field:  &ast.Identifier{Value: "foo"},
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []string{"cannot access field 'foo' on non-struct type 'int'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer := New()
			errors, _ := analyzer.Analyze(tt.program)

			if tt.wantErrors == nil {
				require.Empty(t, errors, "expected no errors")
			} else {
				require.Len(t, errors, len(tt.wantErrors))
				for i, want := range tt.wantErrors {
					require.Contains(t, errors[i].Msg, want)
				}
			}

			// Optional: basic check on TypeMap size or specific entries if needed
			if tt.wantTypeMap != nil {
				// Custom assertions based on expected types
			}
		})
	}
}

// Additional focused table tests for specific analyzers if needed

func TestAnalyzer_ResolveAstType(t *testing.T) {
	// Could be a separate test for helper methods
	analyzer := New()

	tests := []struct {
		name     string
		typeExpr ast.TypeExpr
		want     Type
		wantErr  bool
	}{
		{"int", &ast.NamedType{Name: "int"}, IntType{}, false},
		{"unknown", &ast.NamedType{Name: "FooBar"}, UnknownType{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.resolveAstType(tt.typeExpr) // Note: method is unexported, may need to adjust or test via Analyze
			if tt.wantErr {
				// Check errors
			} else {
				require.True(t, got.Equals(tt.want))
			}
		})
	}
}

// analyzeInput parses the input and runs semantic analysis.
// It now returns both the errors and the resolved TypeMap.
func analyzeInput(t *testing.T, input string) ([]ast.CompileError, map[ast.Node]Type) {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser errors:\n%s", strings.Join(p.Errors(), "\n"))
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
			name: "basic declarations and return",
			input: `
			fn main() int {
				x := 5
				y := 10
				return x + y
			}`,
		},
		{
			name: "function with parameters and call",
			input: `
			fn add(a int, b int) int {
				return a + b
			}

			fn main() int {
				return add(3, 4)
			}`,
		},
		{
			name: "if expression",
			input: `
			fn main() int {
				return if (true) { => 10 } else { => 20 }
			}`,
		},
		{
			name: "struct declaration and usage",
			input: `
			type Point = { x int, y int }

			fn main() int {
				p := Point{x: 10, y: 20}
				return p.x + p.y
			}`,
		},
		{
			name: "built-in puts",
			input: `
			fn main() int {
				puts("hello world")
				return 0
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, typeMap := analyzeInput(t, tt.input)
			assert.Empty(t, errors, "expected no semantic errors")
			assert.NotEmpty(t, typeMap, "TypeMap should be populated for valid programs")
		})
	}
}

func TestReturnPathEnforcement(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "missing return statement",
			input: `
			fn main() int {
				x := 5
			}`,
			expectedErr: "missing a return statement",
		},
		{
			name: "wrong return type",
			input: `
			fn main() int {
				return "hello"
			}`,
			expectedErr: "expected return type 'int', got 'string'",
		},
		{
			name: "function returning a struct",
			input: `
			type Point = { x int, y int }
			fn makePoint() Point {
				return Point{x: 1, y: 2}
			}
			fn main() int { return 0 }
			`,
			expectedErr: "", // Should be valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}

func TestStructAndFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "access unknown field",
			input: `
			type Point = { x int }
			fn main() int {
				p := Point{x: 10}
				return p.y
			}`,
			expectedErr: "field 'y' does not exist on struct 'Point'",
		},
		{
			name: "field access on non-struct",
			input: `
			fn main() int {
				x := 5
				return x.y
			}`,
			expectedErr: "cannot access field 'y' on non-struct type 'int'",
		},
		{
			name: "duplicate struct definition",
			input: `
			type Point = { x int }
			type Point = { y int }
			fn main() int { return 0 }
			`,
			expectedErr: "type 'Point' already defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0].Msg, tt.expectedErr)
		})
	}
}

func TestVariableShadowing(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "duplicate immutable declaration",
			input: `
			fn main() int {
				x := 5
				x := 10
				return x
			}`,
			expectedErr: "variable 'x' is already declared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0].Msg, tt.expectedErr)
		})
	}
}

func TestUndefinedVariables(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name:        "undefined name in return",
			input:       `fn main() int { return x }`,
			expectedErr: "undefined name 'x'",
		},
		{
			name: "undefined function",
			input: `
			fn main() int {
				return missingFunc()
			}`,
			expectedErr: "undefined name 'missingFunc'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0].Msg, tt.expectedErr)
		})
	}
}

func TestTypeMismatch(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name:        "bool used in arithmetic",
			input:       `fn main() int { return true + 5 }`,
			expectedErr: "type mismatch",
		},
		{
			name:        "non-boolean if condition",
			input:       `fn main() int { return if (5) { => 10 } }`,
			expectedErr: "IF condition must be a boolean",
		},
		{
			name: "wrong argument type in function call",
			input: `
			fn add(a int, b int) int { return a + b }
			fn main() int {
				return add(5, "hello")
			}`,
			expectedErr: "argument 1 type mismatch",
		},
		{
			name: "wrong number of arguments",
			input: `
			fn add(a int, b int) int { return a + b }
			fn main() int {
				return add(5)
			}`,
			expectedErr: "wrong number of arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0].Msg, tt.expectedErr)
		})
	}
}

func TestImmutableAssignment(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			require.NotEmpty(t, errors)
			assert.Contains(t, errors[0].Msg, tt.expectedErr)
		})
	}
}

func TestArrayLiteralSemanticAnalysis(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedErr  string
		expectedType Type
	}{
		{
			name: "valid int array",
			input: `
			fn main() int {
				x := [1, 2, 3]
				return x[0]
			}`,
			expectedErr:  "",
			expectedType: ArrayType{Base: IntType{}, Size: 3},
		},
		{
			name: "valid bool array",
			input: `
			fn main() int {
				flags := [true, false, true]
				return 0
			}`,
			expectedErr:  "",
			expectedType: ArrayType{Base: BoolType{}, Size: 3},
		},
		{
			name: "empty array literal",
			input: `
			fn main() int {
				x := []
				return 0
			}`,
			expectedErr: "cannot infer type of empty array literal",
		},
		{
			name: "mixed array element types",
			input: `
			fn main() int {
				x := [1, true, 3]
				return 0
			}`,
			expectedErr: "array element 1 type mismatch: expected 'int', got 'bool'",
		},
		{
			name: "nested array literals",
			input: `
			fn main() int {
				x := [[1, 2], [3, 4]]
				return 0
			}`,
			expectedErr: "",
			expectedType: ArrayType{
				Base: ArrayType{
					Base: IntType{},
					Size: 2,
				},
				Size: 2,
			},
		},
		{
			name: "array with expression elements",
			input: `
			fn main() int {
				x := [1 + 2, 3 * 4]
				return 0
			}`,
			expectedErr:  "",
			expectedType: ArrayType{Base: IntType{}, Size: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, typeMap := analyzeInput(t, tt.input)

			if tt.expectedErr != "" {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
				return
			}

			require.Empty(t, errors)

			var found bool

			for _, typ := range typeMap {
				if typ.Equals(tt.expectedType) {
					found = true
					break
				}
			}

			assert.True(
				t,
				found,
				"expected array type %s to exist in TypeMap",
				tt.expectedType.String(),
			)
		})
	}
}

func TestIndexExpressionSemanticAnalysis(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedErr  string
		expectedType Type
	}{
		{
			name: "valid int array indexing",
			input: `
			fn main() int {
				x := [1, 2, 3]
				return x[1]
			}`,
			expectedErr:  "",
			expectedType: IntType{},
		},
		{
			name: "valid bool array indexing",
			input: `
			fn main() int {
				flags := [true, false]
				if (flags[0]) {
					return 1
				}
				return 0
			}`,
			expectedErr:  "",
			expectedType: BoolType{},
		},
		{
			name: "indexing non-array type",
			input: `
			fn main() int {
				x := 5
				return x[0]
			}`,
			expectedErr: "cannot index non-array/slice type 'int'",
		},
		{
			name: "non-integer index",
			input: `
			fn main() int {
				x := [1, 2, 3]
				return x[true]
			}`,
			expectedErr: "index must be an integer, got 'bool'",
		},
		{
			name: "nested array indexing",
			input: `
			fn main() int {
				matrix := [[1, 2], [3, 4]]
				return matrix[0][1]
			}`,
			expectedErr:  "",
			expectedType: IntType{},
		},
		{
			name: "index expression with computed index",
			input: `
			fn main() int {
				x := [10, 20, 30]
				return x[1 + 1]
			}`,
			expectedErr:  "",
			expectedType: IntType{},
		},
		{
			name: "indexing result used in arithmetic",
			input: `
			fn main() int {
				x := [1, 2, 3]
				return x[0] + x[1]
			}`,
			expectedErr:  "",
			expectedType: IntType{},
		},
		{
			name: "indexing unknown identifier",
			input: `
			fn main() int {
				return arr[0]
			}`,
			expectedErr: "undefined name 'arr'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, typeMap := analyzeInput(t, tt.input)

			if tt.expectedErr != "" {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
				return
			}

			require.Empty(t, errors)

			var found bool

			for _, typ := range typeMap {
				if typ.Equals(tt.expectedType) {
					found = true
					break
				}
			}

			assert.True(
				t,
				found,
				"expected type %s to exist in TypeMap",
				tt.expectedType.String(),
			)
		})
	}
}

func TestArrayAndIndexIntegration(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "array passed through variable",
			input: `
			fn main() int {
				values := [1, 2, 3]
				x := values[2]
				return x
			}`,
		},
		{
			name: "array indexing inside function call",
			input: `
			fn identity(x int) int {
				return x
			}

			fn main() int {
				values := [1, 2, 3]
				return identity(values[0])
			}`,
		},
		{
			name: "array indexing in conditional",
			input: `
			fn main() int {
				flags := [true, false]

				if (flags[0]) {
					return 1
				}

				return 0
			}`,
		},
		{
			name: "nested indexing arithmetic",
			input: `
			fn main() int {
				matrix := [[1, 2], [3, 4]]
				return matrix[0][0] + matrix[1][1]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, typeMap := analyzeInput(t, tt.input)

			assert.Empty(t, errors)
			assert.NotEmpty(t, typeMap)
		})
	}
}

func TestForStatements(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "for loop with bool condition",
			input: `
			fn main() int {
				for (true) { }
				return 0
			}`,
		},
		{
			name: "for loop with variable condition",
			input: `
			fn main() int {
				x := true
				for (x) { }
				return 0
			}`,
		},
		{
			name: "for loop with init and post",
			input: `
			fn main() int {
				for (mut i := 0; i < 10; i = i + 1) { }
				return 0
			}`,
		},
		{
			name: "infinite for loop that always returns",
			input: `
			fn main() int {
				for (true) {
					return 42
				}
			}`,
		},
		{
			name: "condition must be bool",
			input: `
			fn main() int {
				for (42) { }
				return 0
			}`,
			expectedErr: "condition must be of type 'bool'",
		},
		{
			name: "unreachable code after return in for",
			input: `
			fn main() int {
				for (true) {
					return 1
					x := 2  // unreachable
				}
				return 0
			}`,
			expectedErr: "unreachable code after return statement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}

func TestSliceExpressions(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "slice entire array",
			input: `
			fn main() int {
				arr := [1, 2, 3, 4]
				s := arr[:]
				return 0
			}`,
		},
		{
			name: "slice with low and high",
			input: `
			fn main() int {
				arr := [1, 2, 3, 4, 5]
				s := arr[1:4]
				return 0
			}`,
		},
		{
			name: "slice a slice",
			input: `
			fn main() int {
				arr := [1, 2, 3, 4]
				s1 := arr[1:3]
				s2 := s1[0:1]
				return 0
			}`,
		},
		{
			name: "index into slice",
			input: `
			fn main() int {
				arr := [10, 20, 30]
				s := arr[:]
				return s[1]
			}`,
		},
		{
			name: "slice non-array/slice type",
			input: `
			fn main() int {
				x := 5
				s := x[1:3]
				return 0
			}`,
			expectedErr: "cannot slice non-array/slice type",
		},
		{
			name: "slice low/high must be int",
			input: `
			fn main() int {
				arr := [1, 2, 3]
				s := arr[true:5]
				return 0
			}`,
			expectedErr: "slice low index must be an integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
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
		{
			name: "immutable variable cannot be reassigned",
			input: `
			fn main() int {
				x := 5
				x = 10
				return x
			}`,
			expectedErr: "cannot mutate immutable variable 'x'",
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
			name: "immutable array cannot be mutated via index",
			input: `
			fn main() int {
				arr := [1, 2, 3]
				arr[1] = 99
				return 0
			}`,
			expectedErr: "cannot mutate immutable variable 'arr'",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}

func TestReturnPathAnalysis(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{
			name: "return in if consequence only",
			input: `
			fn main() int {
				if (true) { return 1 }
				return 0
			}`,
		},
		{
			name: "return in else only",
			input: `
			fn main() int {
				if (false) { 
					return 1 
				} else { 
					return 0 
				}
			}`,
		},
		{
			name: "missing return when only one branch returns",
			input: `
			fn main() int {
				if (true) {
					return 1
				}
				// no return here
			}`,
			expectedErr: "function 'main' is missing a return statement",
		},
		{
			name: "nested if with guaranteed return",
			input: `
			fn main() int {
				if (true) {
					if (true) {
						return 1
					} else {
						return 2
					}
				} else {
					return 3
				}
			}`,
		},
		{
			name: "if without else does not guarantee return",
			input: `
			fn main() int {
				if (true) { return 42 }
				// falls through → error
			}`,
			expectedErr: "function 'main' is missing a return statement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)
			if tt.expectedErr == "" {
				assert.Empty(t, errors)
			} else {
				require.NotEmpty(t, errors)
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}

func TestAssignment(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		// === Scoping Rules (should already work) ===
		{
			name: "variable declared only inside if branch is not visible outside",
			input: `
			fn main() int {
				if (true) {
					x := 5
				} else {
					x := 10
				}
				return x
			}`,
			expectedErr: "undefined name 'x'",
		},
		{
			name: "variable declared in one branch only",
			input: `
			fn main() int {
				if (true) {
					x := 5
				}
				return x
			}`,
			expectedErr: "undefined name 'x'",
		},
		{
			name: "variable declared outside and assigned on all paths",
			input: `
			fn main() int {
				mut x := 0
				if (true) {
					x = 5
				} else {
					x = 10
				}
				return x
			}`,
		},
		{
			name: "variable may not be assigned on all paths",
			input: `
			fn main() int {
				mut x := 0
				if (true) {
					x = 5
				}
				// else branch does not assign x
				return x
			}`,
		},
		{
			name: "assigned before use in straight line code",
			input: `
			fn main() int {
				mut x := 42
				x = 100
				return x
			}`,
		},
		{
			name: "nested if - assigned in all paths",
			input: `
			fn main() int {
				mut x := 0
				if (true) {
					if (false) {
						x = 1
					} else {
						x = 2
					}
				} else {
					x = 3
				}
				return x
			}`,
		},
		{
			name: "assigned inside for loop does not count as definite",
			input: `
			fn main() int {
				mut x := 0
				for (true) {
					x = 5
				}
				return x  
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, _ := analyzeInput(t, tt.input)

			if tt.expectedErr == "" {
				assert.Empty(t, errors, "expected no errors for valid case")
			} else {
				require.NotEmpty(t, errors, "expected an error")
				assert.Contains(t, errors[0].Msg, tt.expectedErr)
			}
		})
	}
}
