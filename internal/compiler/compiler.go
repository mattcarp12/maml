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

type Compiler struct {
	lexer   *lexer.Lexer
	parser  *parser.Parser
	sema    *sema.Analyzer
	codegen *codegen.Codegen
}

func New() *Compiler {
	return &Compiler{
		sema:    sema.New(),
		codegen: codegen.New(),
	}
}

// CompileSource is now a convenience wrapper around the more detailed Compile method.
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
	// 1. Lex & Parse
	c.lexer = lexer.New(src)
	c.parser = parser.New(c.lexer)
	program := c.parser.ParseProgram()
	if errs := c.parser.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("parser errors:\n%s", strings.Join(errs, "\n"))
	}

	// 2. Semantic Analysis
	semaErrors, typeMap := c.sema.Analyze(program)
	if len(semaErrors) > 0 {
		var sb strings.Builder
		sb.WriteString("semantic errors:\n")
		for _, err := range semaErrors {
			sb.WriteString(fmt.Sprintf("%s\n", err))
		}
		return nil, errors.New(sb.String())
	}

	// 3. Code Generation
	// Note: We'll assume codegen.Generate now returns the ir.Module as part of its state
	if err := c.codegen.Generate(program, typeMap); err != nil {
		return nil, fmt.Errorf("codegen error: %w", err)
	}

	// 4. IR Verification (Sanity Check)
	if err := c.codegen.Validate(); err != nil {
		// Even if IR is invalid, we might want to see it for debugging
		return &Result{LLVMIR: c.codegen.String()}, fmt.Errorf("IR verification failed: %w", err)
	}

	return &Result{
		AST:     program,
		TypeMap: typeMap,
		Module:  c.codegen.Module(), // We'll add this getter next
		LLVMIR:  c.codegen.String(),
	}, nil
}
