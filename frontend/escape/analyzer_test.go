package escape

import (
	"strings"
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// analyzeEscape is a helper that runs the pipeline up through the escape analyzer.
func analyzeEscape(t *testing.T, input string) (*ast.Program, map[ast.Node]EscapeState) {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	semaAnalyzer := sema.New()
	typeMap, errors := semaAnalyzer.Analyze(program)
	if len(errors) > 0 {
		var errMsgs []string
		for _, e := range errors {
			errMsgs = append(errMsgs, e.Msg)
		}
		t.Fatalf("Sema errors:\n%s", strings.Join(errMsgs, "\n"))
	}

	escapeAnalyzer := New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	return program, escapeMap
}

// extractVarStates walks the AST to find DeclareStmts and looks up their
// root allocation's EscapeState. This allows us to assert on variable names directly.
func extractVarStates(program *ast.Program, escapeMap map[ast.Node]EscapeState) map[string]EscapeState {
	states := make(map[string]EscapeState)

	var walk func(node ast.Node)
	walk = func(node ast.Node) {
		// This untyped nil check doesn't catch typed nils hidden in interfaces
		if node == nil {
			return
		}

		switch n := node.(type) {
		case *ast.Program:
			if n == nil {
				return
			}
			for _, d := range n.Decls {
				walk(d)
			}
		case *ast.FnDecl:
			if n == nil {
				return
			}
			walk(n.Body)
		case *ast.BlockStmt:
			if n == nil {
				return
			} // Guard against typed nils
			for _, s := range n.Statements {
				walk(s)
			}
		case *ast.DeclareStmt:
			if n == nil {
				return
			}

			// Find the root allocation node (stripping field accesses / index expressions)
			var alloc ast.Node = n.Value
			for {
				if idx, ok := alloc.(*ast.IndexExpr); ok {
					alloc = idx.Left
					continue
				}
				if fa, ok := alloc.(*ast.FieldAccess); ok {
					alloc = fa.Object
					continue
				}
				if sl, ok := alloc.(*ast.SliceExpr); ok {
					alloc = sl.Left
					continue
				}
				break
			}

			// Check if the allocation escaped
			state, exists := escapeMap[alloc]
			if !exists {
				// Fallback to checking the DeclareStmt itself
				state = escapeMap[n]
			}
			states[n.Name] = state

		case *ast.IfExpr:
			if n == nil {
				return
			}
			if n.Consequence != nil {
				walk(n.Consequence)
			}
			if n.Alternative != nil { // Prevent the panic on missing 'else' blocks!
				walk(n.Alternative)
			}
		case *ast.ForStmt:
			if n == nil {
				return
			}
			if n.Init != nil {
				walk(n.Init)
			}
			if n.Body != nil {
				walk(n.Body)
			}
		case *ast.ExprStmt:
			if n == nil {
				return
			}
			walk(n.Value)
		}
	}

	walk(program)
	return states
}

func TestEscapeAnalysis(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantHeap  []string
		wantStack []string
	}{
		{
			name: "local array returning a primitive stays on stack",
			input: `
			fn main() int {
				arr := [1, 2, 3]
				return arr[0]
			}`,
			wantHeap:  []string{},
			wantStack: []string{"arr"},
		},
		{
			name: "local array returning by value stays on stack",
			input: `
			fn main() [3]int {
				arr := [1, 2, 3]
				return arr
			}`,
			wantHeap:  []string{},
			wantStack: []string{"arr"}, // Arrays are value types! Copied by value.
		},
		{
			name: "returned slice escapes its backing array",
			input: `
			fn main() []int {
				arr := [1, 2, 3]
				s := arr[:]
				return s
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "transitive escape via simple assignment",
			input: `
			fn main() []int {
				arr := [1, 2, 3]
				a := arr[:]
				b := a
				return b
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "own transfer escapes to heap",
			input: `
			fn consume(own x []int) int {
				return 0
			}
			fn main() int {
				arr := [1, 2, 3]
				s := arr[:]
				consume(own s)
				return 0
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "mut borrow does NOT escape",
			input: `
			fn update(mut x []int) int {
				return 0
			}
			fn main() int {
				arr := [1, 2, 3]
				mut s := arr[:]
				update(mut s)
				return 0
			}`,
			wantHeap:  []string{},
			wantStack: []string{"arr"},
		},
		{
			name: "struct field escapes container and contents",
			input: `
			type Box = { data []int }
			fn main() Box {
				arr := [1, 2, 3]
				b := Box{data: arr[:]}
				return b
			}`,
			// b is returned, so its internal reference types (arr) must escape!
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "assignment flow sensitivity (reassignment)",
			input: `
			fn main() []int {
				a := [1]
				b := [2]
				mut c := a[:]
				c = b[:]
				return c
			}`,
			// Both a and b conservatively escape because c holds them and is returned
			wantHeap:  []string{"a", "b"},
			wantStack: []string{},
		},
		{
			name: "branching escape",
			input: `
			fn main() []int {
				a := [1]
				b := [2]
				if (true) {
					return a[:]
				}
				c := [3]
				return c[:]
			}`,
			wantHeap:  []string{"a", "c"},
			wantStack: []string{"b"}, // b is never returned
		},
		{
			name: "string literal escapes on return",
			input: `
			fn main() string {
				s := "hello heap"
				return s
			}`,
			wantHeap:  []string{"s"},
			wantStack: []string{},
		},
		{
			name: "primitives do not escape when assigned to structs",
			input: `
			type Point = { x int, y int }
			fn main() Point {
				val := 42
				p := Point{x: val, y: 10}
				return p
			}`,
			wantHeap:  []string{},
			wantStack: []string{"val"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, escapeMap := analyzeEscape(t, tt.input)
			varStates := extractVarStates(program, escapeMap)

			for _, expectedHeap := range tt.wantHeap {
				state, exists := varStates[expectedHeap]
				require.True(t, exists, "variable '%s' not found in AST var list", expectedHeap)
				assert.Equal(t, StateHeap, state, "expected variable '%s' to be StateHeap", expectedHeap)
			}

			for _, expectedStack := range tt.wantStack {
				state, exists := varStates[expectedStack]
				require.True(t, exists, "variable '%s' not found in AST var list", expectedStack)
				assert.Equal(t, StateStack, state, "expected variable '%s' to be StateStack", expectedStack)
			}
		})
	}
}

func TestEscapeAnalysis_AdvancedCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantHeap  []string
		wantStack []string
	}{
		{
			name: "struct literal with slice field escapes",
			input: `
			type Box = { data []int }
			fn main() Box {
				arr := [1, 2, 3]
				b := Box{data: arr[:]}
				return b
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "owned parameter forces escape",
			input: `
			fn consume(own s []int) int {return 0}
			fn main() int {
				arr := [1, 2, 3]
				s := arr[:]
				consume(own s)
				return 0
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "return inside if/else forces escape",
			input: `
			fn main() []int {
				arr := [1, 2, 3]
				if true {
					return arr[:]
				} else {
					return [4, 5, 6][:]
				}
			}`,
			wantHeap:  []string{"arr"},
			wantStack: []string{},
		},
		{
			name: "for loop with slice does not escape if not returned",
			input: `
			fn main() int {
				arr := [1, 2, 3]
				for i := 0; i < 3; i = i + 1 {
					s := arr[:]
					x := s[0]
				}
				return 0
			}`,
			wantHeap:  []string{},
			wantStack: []string{"arr"},
		},
		{
			name: "reassignment of slice reference",
			input: `
			fn main() []int {
				a := [10]
				b := [20]
				mut s := a[:]
				s = b[:]
				return s
			}`,
			wantHeap:  []string{"a", "b"},
			wantStack: []string{},
		},
		{
			name: "primitive in struct does not force heap",
			input: `
			type Point = { x int, y int }
			fn main() Point {
				x := 42
				p := Point{x: x, y: 99}
				return p
			}`,
			wantHeap:  []string{},
			wantStack: []string{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, escapeMap := analyzeEscape(t, tt.input)
			varStates := extractVarStates(program, escapeMap)

			for _, name := range tt.wantHeap {
				assert.Equal(t, StateHeap, varStates[name], "expected %s on heap", name)
			}
			for _, name := range tt.wantStack {
				assert.Equal(t, StateStack, varStates[name], "expected %s on stack", name)
			}
		})
	}
}

func TestEscapeEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"nil expressions", `fn main() int {return 0}`},
		{"empty function", `fn main() int {return 0}`},
		{"return primitive", `fn main() int { return 42 }`},
		{"call with owned non-reference", `
			fn consume(own x int) {return}
			fn main() int { mut x := 10; consume(own x); return 0 }`},
		{"struct with only primitives", `
			type Point = { x int }
			fn main() int { p := Point{x: 1}; return 0 }`},
		{"nested if without else", `
			fn main() []int {
				arr := [1]
				if true { return arr[:] }
				return [2][:]
			}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = analyzeEscape(t, tt.input) // just ensure no panic + full traversal
		})
	}
}

func TestEscapeCascadeAndFixedPoint(t *testing.T) {
	input := `
	fn main() []int {
		arr := [1, 2, 3]
		s1 := arr[:]
		s2 := s1
		s3 := s2
		return s3
	}`
	program, escapeMap := analyzeEscape(t, input)
	states := extractVarStates(program, escapeMap)

	assert.Equal(t, StateHeap, states["arr"], "backing array should escape through chain")
}
