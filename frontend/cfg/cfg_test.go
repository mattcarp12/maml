package cfg_test

import (
	"testing"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/cfg"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/parser"
)

func buildCFG(t *testing.T, input string) (*cfg.CFG, *ast.FnDecl) {
	t.Helper()

	l := lexer.New(input)
	p := parser.New(l)

	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	if len(program.Decls) != 1 {
		t.Fatalf("expected exactly one decl")
	}

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected function declaration")
	}

	graph := cfg.Build(fn)

	return graph, fn
}

func TestAlwaysReturns_SimpleReturn(t *testing.T) {
	input := `
fn f() int {
	return 5
}
`

	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected function to always return")
	}
}

func TestAlwaysReturns_MissingReturn(t *testing.T) {
	input := `
fn f() int {
	x := 5
}
`

	graph, _ := buildCFG(t, input)

	if cfg.AlwaysReturns(graph) {
		t.Fatalf("expected function to not always return")
	}
}

func TestAlwaysReturns_IfElseBothReturn(t *testing.T) {
	input := `
fn f(x int) int {
	if x > 0 {
		return 1
	} else {
		return 2
	}
}
`

	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected function to always return")
	}
}

func TestAlwaysReturns_IfPartialReturn(t *testing.T) {
	input := `
fn f(x int) int {
	if x > 0 {
		return 1
	}

	y := 5
}
`

	graph, _ := buildCFG(t, input)

	if cfg.AlwaysReturns(graph) {
		t.Fatalf("expected partial-return function to fail")
	}
}

func TestAlwaysReturns_InfiniteLoopWithReturn(t *testing.T) {
	input := `
fn f() int {
	for {
		return 5
	}
}
`

	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected loop-with-return to always return")
	}
}

func TestBreakAllowsFallthrough(t *testing.T) {
	input := `
fn f() int {
	for {
		break
	}
}
`

	graph, _ := buildCFG(t, input)

	if cfg.AlwaysReturns(graph) {
		t.Fatalf("expected break to allow fallthrough")
	}
}

func TestContinuePreservesInfiniteLoop(t *testing.T) {
	input := `
fn f() int {
	for {
		continue
	}
}
`

	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected continue loop to remain infinite")
	}
}

func TestAlwaysReturns_ExhaustiveMatch(t *testing.T) {
	input := `
fn f(x int) int {
	match x {
		case 1 {
			return 1
		}
		case _ {
			return 2
		}
	}
}
`

	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected exhaustive match to return")
	}
}

func TestAlwaysReturns_NonExhaustiveMatch(t *testing.T) {
	input := `
fn f(x int) int {
	match x {
		case 1 {
			return 1
		}
	}
}
`

	graph, _ := buildCFG(t, input)

	if cfg.AlwaysReturns(graph) {
		t.Fatalf("expected non-exhaustive match to fall through")
	}
}

func TestReachability(t *testing.T) {
	input := `
fn f() {
	if false {
		puts("dead")
	}

	puts("live")
}
`

	graph, _ := buildCFG(t, input)

	reachable := cfg.Reachable(graph)

	foundDead := false

	for id, block := range graph.Blocks {
		if !reachable[id] && len(block.Statements) > 0 {
			foundDead = true
		}
	}

	if !foundDead {
		t.Fatalf("expected unreachable block")
	}
}

func TestDumpCFG(t *testing.T) {
	input := `
fn f(x int) int {
	if x > 0 {
		return 1
	}

	return 2
}
`

	graph, _ := buildCFG(t, input)

	cfg.Dump(graph)
}

func TestDeadBranch_IfFalse(t *testing.T) {
	input := `
fn f() {
	if false {
		puts("dead code")
	}
	puts("live")
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead statements inside if false")
	}
}

func TestDeadBranch_IfTrueElse(t *testing.T) {
	input := `
fn f() {
	if true {
		puts("live")
	} else {
		puts("dead")
	}
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead else branch")
	}
}

func TestDeadBranch_ComplexConst(t *testing.T) {
	input := `
fn f() {
	if !false || false {
		puts("live")
	} else {
		puts("dead")
	}

	if false && true {
		puts("also dead")
	}
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) != 2 {
		t.Fatalf("expected 2 dead statements, got %d", len(result.DeadStatements))
	}
}

func TestDeadCode_AfterReturn(t *testing.T) {
	input := `
fn f() int {
	return 42
	puts("dead")
	x := 10
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead code after return")
	}
}

func TestDeadCode_AfterBreak(t *testing.T) {
	input := `
fn f() {
	for {
		break
		puts("dead after break")
	}
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead code after break")
	}
}

func TestDeadCode_AfterContinue(t *testing.T) {
	input := `
fn f() {
	for {
		continue
		puts("dead after continue")
	}
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead code after continue")
	}
}

func TestAlwaysReturns_InfiniteLoop(t *testing.T) {
	input := `
fn f() int {
	for {
		puts("infinite")
	}
}
`
	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("infinite loop should count as always returns")
	}
}

func TestAlwaysReturns_BreakOutOfLoop(t *testing.T) {
	input := `
fn f() int {
	for {
		if true {
			break
		}
	}
	// fallthrough here
}
`
	graph, _ := buildCFG(t, input)

	if cfg.AlwaysReturns(graph) {
		t.Fatalf("function with break should NOT always return")
	}
}

func TestReachability_NestedDeadCode(t *testing.T) {
	input := `
fn f() {
	if false {
		puts("dead1")
		if true {
			puts("dead2")
		}
		puts("dead3")
	}
	puts("live")
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) != 3 {
		t.Fatalf("expected 3 dead statements, got %d", len(result.DeadStatements))
	}
}

func TestMatch_ExhaustiveVsNonExhaustive(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		shouldReturn bool
	}{
		{
			name:         "exhaustive with wildcard",
			input:        `fn f(x int) int { match x { case 1 { return 1 } case _ { return 2 } } }`,
			shouldReturn: true,
		},
		{
			name:         "non-exhaustive",
			input:        `fn f(x int) int { match x { case 1 { return 1 } } }`,
			shouldReturn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, _ := buildCFG(t, tt.input)
			if cfg.AlwaysReturns(graph) != tt.shouldReturn {
				t.Fatalf("expected AlwaysReturns = %v", tt.shouldReturn)
			}
		})
	}
}

func TestDeadCode_InForFalse(t *testing.T) {
	input := `
fn f() {
	for false {
		puts("dead loop body")
	}
	puts("live after")
}
`
	graph, _ := buildCFG(t, input)
	result := cfg.Analyze(graph)

	if len(result.DeadStatements) == 0 {
		t.Fatalf("expected dead for false body")
	}
}

func TestAlwaysReturns_ReturnInAllPaths(t *testing.T) {
	input := `
fn f(x int) int {
	if x > 0 {
		return 1
	} else if x < 0 {
		return 2
	} else {
		return 3
	}
}
`
	graph, _ := buildCFG(t, input)

	if !cfg.AlwaysReturns(graph) {
		t.Fatalf("expected all paths return")
	}
}
