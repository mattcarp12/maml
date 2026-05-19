package frontend

import (
	"fmt"
	"io"
	"os"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/codegen"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/ownership"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
)

// Temporary until the C++ backend fully replaces the Go LLVM backend.
// After migration this can disappear entirely.
//
// KEEP THIS IMPORT FOR NOW.

type Compiler struct{}

func New() *Compiler {
	return &Compiler{}
}

// FrontendResult contains all semantic information produced by the frontend.
type FrontendResult struct {
	Program   *ast.Program
	TypeMap   map[ast.Node]sema.Type
	EscapeMap map[ast.Node]escape.EscapeState
}

// AsyncLowerer is currently a placeholder lowering pass.
type AsyncLowerer struct{}

func (a *AsyncLowerer) Mutate(node ast.Node) ast.Node {
	return node
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
		return nil, fmt.Errorf("parser syntax error: %s", p.Errors()[0])
	}

	// ---------------------------------------------------------------------
	// 2. Lowering Passes
	// ---------------------------------------------------------------------

	lowerer := &AsyncLowerer{}
	program = ast.Rewrite(lowerer, program).(*ast.Program)

	// ---------------------------------------------------------------------
	// 3. Semantic Analysis
	// ---------------------------------------------------------------------

	semaAnalyzer := sema.New()

	semaErrors, typeMap := semaAnalyzer.Analyze(program)
	if len(semaErrors) > 0 {
		return nil, semaErrors[0]
	}

	// ---------------------------------------------------------------------
	// 4. Ownership Analysis
	// ---------------------------------------------------------------------

	ownershipAnalyzer := ownership.New(typeMap)

	ownershipErrors := ownershipAnalyzer.Analyze(program)
	if len(ownershipErrors) > 0 {
		return nil, ownershipErrors[0]
	}

	// ---------------------------------------------------------------------
	// 5. Escape Analysis
	// ---------------------------------------------------------------------

	escapeAnalyzer := escape.New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	return &FrontendResult{
		Program:   program,
		TypeMap:   typeMap,
		EscapeMap: escapeMap,
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

// -----------------------------------------------------------------------------
// TEMPORARY LEGACY GO LLVM BACKEND
// -----------------------------------------------------------------------------
//
// This entire section disappears after the C++ backend is complete.
//
// Keeping it isolated makes removal easy later.
// -----------------------------------------------------------------------------

func (c *Compiler) GenerateLLVMIR(src string) (string, error) {
	frontendResult, err := c.Frontend(src)
	if err != nil {
		return "", err
	}

	cg := codegen.New()

	if err := cg.Generate(
		frontendResult.Program,
		frontendResult.TypeMap,
		frontendResult.EscapeMap,
	); err != nil {
		return "", fmt.Errorf("codegen generation failed: %w", err)
	}

	return cg.String(), nil
}

// EmitHIR serializes the typed frontend representation into JSON.
func (c *Compiler) EmitHIR(src string, out io.Writer) error {
	frontendResult, err := c.Frontend(src)
	if err != nil {
		return err
	}

	emitter := NewEmitter(
		frontendResult.TypeMap,
		frontendResult.EscapeMap,
	)

	return emitter.Emit(frontendResult.Program, out)
}
