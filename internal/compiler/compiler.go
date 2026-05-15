package compiler

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/codegen"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/parser"
	"github.com/mattcarp12/maml/internal/sema"
)

// Result holds all artifacts produced during the compilation process.
type Result struct {
	AST     *ast.Program
	TypeMap map[ast.Node]sema.Type
	Module  *ir.Module
	LLVMIR  string
}

// Compiler is completely stateless.
// It acts as a namespace for compilation methods.
type Compiler struct{}

func New() *Compiler {
	return &Compiler{}
}

func (c *Compiler) CompileSource(src string) (string, error) {
	res, err := c.Compile(src)
	if err != nil {
		return "", err
	}
	return res.LLVMIR, nil
}

func (c *Compiler) CompileFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return c.CompileSource(string(content))
}

// Compile executes the full pipeline and returns all intermediate artifacts.
func (c *Compiler) Compile(src string) (*Result, error) {
	// 1. Lex & Parse (Local instances)
	l := lexer.New(src)
	p := parser.New(l)
	program := p.ParseProgram()

	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("parser errors:\n%s", strings.Join(errs, "\n"))
	}

	// 2. Semantic Analysis
	analyzer := sema.New()
	semaErrors, typeMap := analyzer.Analyze(program)
	if len(semaErrors) > 0 {
		var sb strings.Builder
		sb.WriteString("semantic errors:\n")
		for _, err := range semaErrors {
			sb.WriteString(fmt.Sprintf("%s\n", err))
		}
		return nil, errors.New(sb.String())
	}

	// 3. Code Generation
	cg := codegen.New()
	if err := cg.Generate(program, typeMap); err != nil {
		return nil, fmt.Errorf("codegen error: %w", err)
	}

	// 4. IR Verification (Sanity Check)
	if err := cg.Validate(); err != nil {
		return &Result{LLVMIR: cg.String()}, fmt.Errorf("IR verification failed: %w", err)
	}

	return &Result{
		AST:     program,
		TypeMap: typeMap,
		Module:  cg.Module(),
		LLVMIR:  cg.String(),
	}, nil
}
