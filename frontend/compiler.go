package frontend

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattcarp12/maml/frontend/alloc"
	"github.com/mattcarp12/maml/frontend/arc"
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/liveness"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/ownership"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/prune"
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
	HIR  *hir.Program
	MIR  *mir.Program
}

// Frontend executes the canonical frontend pipeline.
func (c *Compiler) Frontend(src string) (*FrontendResult, error) {
	// -------------------------------------------------------------------------
	// Phase 1: Syntax Analysis -> AST
	// -------------------------------------------------------------------------
	l := lexer.New(src)
	p := parser.New(l)
	astProgram := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parser syntax errors:\n%s", strings.Join(p.Errors(), "\n"))
	}

	// -------------------------------------------------------------------------
	// Phase 2: Semantic Analysis -> TAST
	// -------------------------------------------------------------------------
	semaAnalyzer := sema.New()
	tastProgram, errs := semaAnalyzer.Analyze(astProgram)
	if len(errs) > 0 {
		return nil, formatErrors("Semantic", errs)
	}

	// -------------------------------------------------------------------------
	// Phase 3 & 4: HIR Translation & MIR Construction (Allocation Agnostic)
	// -------------------------------------------------------------------------
	hirProgram := hir.Translate(tastProgram)

	mirProgram := &mir.Program{
		TypeDecls: make([]*hir.TypeDecl, 0),
		Functions: make([]mir.Function, 0),
	}

	var ownErrs []ast.CompileError

	// Sort declarations and execute the Micro-Package pipeline
	for _, decl := range hirProgram.Decls {
		switch d := decl.(type) {
		case *hir.TypeDecl:
			mirProgram.TypeDecls = append(mirProgram.TypeDecls, d)

		case *hir.FnDecl:
			// Phase 4: Construct CFG
			g := mir.Build(d)

			// Dead Code Elimination
			prune.Graph(g)

			// Phase 5: Dataflow Analysis
			escapes := escape.AnalyzeEscape(g)
			liveVars := liveness.AnalyzeLiveness(g)

			// Phase 6 & 7: Transformation
			alloc.LowerAllocations(g, escapes)
			arc.InjectARC(g, liveVars)

			// Phase 5 Validation: Borrow Checker
			ownAnalyzer := ownership.New()
			ownErrs = append(ownErrs, ownAnalyzer.Analyze(g)...)

			mirProgram.Functions = append(mirProgram.Functions, mir.Function{
				Name:       d.Name,
				Params:     d.Params,
				ReturnType: d.ReturnType,
				IsAsync:    d.IsAsync,
				Graph:      g,
			})
		}
	}

	if len(ownErrs) > 0 {
		return nil, formatErrors("Ownership", ownErrs)
	}

	return &FrontendResult{
		AST:  astProgram,
		TAST: tastProgram,
		HIR:  hirProgram,
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
