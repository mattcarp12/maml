package frontend

import (
	"fmt"
	"os"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/cfg"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/lexer"
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
		return nil, fmt.Errorf("parser syntax error: %s", p.Errors()[0])
	}

	// ---------------------------------------------------------------------
	// 2. Lowering Passes
	// ---------------------------------------------------------------------

	// ---------------------------------------------------------------------
	// 3. Semantic Analysis
	// ---------------------------------------------------------------------

	semaAnalyzer := sema.New()

	typeMap, semaErrors := semaAnalyzer.Analyze(program)
	if len(semaErrors) > 0 {
		return nil, semaErrors[0]
	}

	// ---------------------------------------------------------------------
	// 4. CFG Analysis
	// ---------------------------------------------------------------------

	cfgGraphs := make(map[*ast.FnDecl]*cfg.CFG)
	cfgResults := make(map[*ast.FnDecl]*cfg.Result)

	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FnDecl)
		if !ok {
			continue
		}

		graph := cfg.Build(fn)
		result := cfg.Analyze(graph)

		cfgGraphs[fn] = graph
		cfgResults[fn] = result

		fnType := typeMap[fn.ReturnType]

		if fnType != nil {
			if ft, ok := fnType.(*sema.FunctionType); ok {

				if !ft.Return.Equals(sema.UnitType{}) &&
					!result.AlwaysReturns {

					return nil, fmt.Errorf(
						"function '%s' is missing a return statement",
						fn.Name,
					)
				}
			}
		}

		for _, node := range result.DeadStatements {
			return nil, fmt.Errorf(
				"%s: unreachable code",
				node.Pos(),
			)
		}
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
