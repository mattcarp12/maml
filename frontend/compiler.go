package frontend

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/cfg"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/lower"
	"github.com/mattcarp12/maml/frontend/ownership"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
)

type Compiler struct{}

func New() *Compiler {
	return &Compiler{}
}

// FrontendResult contains all semantic information produced by the frontend.
type FrontendResult struct {
	Program     *ast.Program
	TypeMap     map[ast.Node]sema.Type
	EscapeMap   map[ast.Node]escape.EscapeState
	CFG         map[*ast.FnDecl]*cfg.CFG
	CFGAnalysis map[*ast.FnDecl]*cfg.Result
}

// Frontend executes the canonical frontend pipeline.
//
// ALL frontend compilation flows should go through this function.
func (c *Compiler) Frontend(src string) (*FrontendResult, error) {
	// ---------------------------------------------------------------------
	// 1. Parse
	// ---------------------------------------------------------------------

	l := lexer.New(src)
	p := parser.New(l)

	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parser syntax errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	// ---------------------------------------------------------------------
	// 2. Pre-Sema Lowering (Inliner Pass)
	// ---------------------------------------------------------------------

	inlinePass := lower.NewInlinePass(program)
	program = ast.Rewrite(inlinePass, program).(*ast.Program)

	// ---------------------------------------------------------------------
	// 3. Semantic Analysis
	// ---------------------------------------------------------------------

	semaAnalyzer := sema.New()

	typeMap, semaErrors := semaAnalyzer.Analyze(program)
	if len(semaErrors) > 0 {
		return nil, formatErrors("Semantic", semaErrors)
	}

	// ---------------------------------------------------------------------
	// 4. CFG Analysis
	// ---------------------------------------------------------------------

	cfgAnalyzer := cfg.NewAnalyzer(typeMap)
	cfgGraphs, cfgResults, cfgErrors := cfgAnalyzer.Analyze(program)
	if len(cfgErrors) > 0 {
		return nil, formatErrors("Control Flow", cfgErrors)
	}

	// ---------------------------------------------------------------------
	// 5. Post-Sema Lowering Passes (Control Flow, Aggregates, Async)
	// ---------------------------------------------------------------------

	loweringPass := lower.NewPass(typeMap)
	program = ast.Rewrite(loweringPass, program).(*ast.Program)

	aggPass := lower.NewAggregatePass(typeMap)
	program = ast.Rewrite(aggPass, program).(*ast.Program)

	asyncPass := lower.NewAsyncPass(typeMap)
	program = ast.Rewrite(asyncPass, program).(*ast.Program)

	// ---------------------------------------------------------------------
	// 6. Escape Analysis
	// ---------------------------------------------------------------------

	escapeAnalyzer := escape.New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	// ---------------------------------------------------------------------
	// 7. Allocation Lowering Pass
	// ---------------------------------------------------------------------

	allocPass := lower.NewAllocPass(escapeMap)
	program = ast.Rewrite(allocPass, program).(*ast.Program)

	// ---------------------------------------------------------------------
	// 8. Ownership Analysis
	// ---------------------------------------------------------------------

	ownershipAnalyzer := ownership.New(typeMap)

	ownershipErrors := ownershipAnalyzer.Analyze(program)
	if len(ownershipErrors) > 0 {
		return nil, formatErrors("Ownership", ownershipErrors)
	}

	// ---------------------------------------------------------------------
	// 9. ARC Lowering Pass
	// ---------------------------------------------------------------------

	arcPass := lower.NewARCPass(typeMap)
	program = ast.Rewrite(arcPass, program).(*ast.Program)

	return &FrontendResult{
		Program:     program,
		TypeMap:     typeMap,
		EscapeMap:   escapeMap,
		CFG:         cfgGraphs,
		CFGAnalysis: cfgResults,
	}, nil
}

// CompileFile only executes the frontend pipeline.
func (c *Compiler) CompileFile(path string) (*FrontendResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", path, err)
	}

	return c.Frontend(string(content))
}

// formatErrors is a helper to aggregate a slice of ast.CompileError into a single formatted error.
func formatErrors(stage string, errs []ast.CompileError) error {
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return fmt.Errorf("%s errors:\n%s", stage, strings.Join(msgs, "\n"))
}
