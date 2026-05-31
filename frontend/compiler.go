package frontend

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
	"github.com/mattcarp12/maml/frontend/tast"
)

type Compiler struct{}

func New() *Compiler {
	return &Compiler{}
}

// FrontendResult contains all semantic information produced by the frontend.
type FrontendResult struct {
	AST  *ast.Program
	TAST *tast.Program
	MIR  *mir.Program
}

// Frontend executes the canonical frontend pipeline.
func (c *Compiler) Frontend(src string) (*FrontendResult, error) {
	// -------------------------------------------------------------------------
	// Syntax Analysis -> AST
	// -------------------------------------------------------------------------
	l := lexer.New(src)
	p := parser.New(l)
	astProgram := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parser syntax errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	// -------------------------------------------------------------------------
	// Semantic Analysis -> TAST
	// -------------------------------------------------------------------------
	semaAnalyzer := sema.New()
	tastProgram, errs := semaAnalyzer.Analyze(astProgram)
	if len(errs) > 0 {
		return nil, formatErrors("Semantic", errs)
	}

	// --------------------------------------------------------------------------
	// MIR Lowering -> MIR
	// --------------------------------------------------------------------------

	mirProgram := mir.BuildProgram(tastProgram)

	// ==========================================================================
	// COMMENT OUT UNTIL CODEGEN IS WORKING
	// ==========================================================================*/

	// // Phase 5-8: Sequential Graph Passes
	// // Iterate over each function and apply the passes sequentially to its CFG.
	// for i := range mirProgram.Functions {
	// 	fn := &mirProgram.Functions[i]

	// 	// Phase 5: Liveness & Escape Analysis
	// 	livenessRes := liveness.AnalyzeLiveness(fn.Graph)
	// 	escapes := escape.AnalyzeEscape(fn.Graph)

	// 	// Phase 6: Allocation Lowering
	// 	alloc.LowerAllocations(fn.Graph, escapes)

	// 	// Phase 7: ARC Injection
	// 	arc.InjectARC(fn.Graph, livenessRes)

	// 	// Phase 8: Ownership Analysis
	// 	ownErrs := ownership.New().Analyze(fn.Graph)
	// 	if len(ownErrs) > 0 {
	// 		return nil, formatErrors("OWNERSHIP", ownErrs)
	// 	}
	// }

	// Return the fully populated pipeline state
	return &FrontendResult{
		AST:  astProgram,
		TAST: tastProgram,
		MIR:  mirProgram,
	}, nil
}

// CompileFile executes the frontend pipeline on a source file.
func (c *Compiler) CompileFile(path string) (*FrontendResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", path, err)
	}
	return c.Frontend(string(content))
}

// formatErrors aggregates compiler errors into a single string.
func formatErrors(stage string, errs []ast.CompileError) error {
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return fmt.Errorf("%s errors:\n%s", stage, strings.Join(msgs, "\n"))
}

// CompileAST executes only Phase 1 (Syntax Analysis) and returns the raw AST.
func (c *Compiler) CompileAST(src string) (*ast.Program, error) {
	l := lexer.New(src)
	p := parser.New(l)
	astProgram := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("[PARSER] errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	return astProgram, nil
}

// CompileAST executes Phase 1 (Syntax Analysis) and Phase 2 (Semantic Analysis)
// and returns the typed AST.
func (c *Compiler) CompileTAST(src string) (*tast.Program, error) {
	l := lexer.New(src)
	p := parser.New(l)
	astProgram := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("[PARSER] errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	semaChecker := sema.New()
	tastProgram, semaErrors := semaChecker.Analyze(astProgram)
	if len(semaErrors) > 0 {
		return nil, formatErrors("[SEMANTIC]", semaErrors)
	}

	return tastProgram, nil
}
