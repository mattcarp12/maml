// internal/compiler/compiler.go
package compiler

import (
	"fmt"
	"os"

	"github.com/mattcarp12/maml/internal/codegen"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/parser"
	"github.com/mattcarp12/maml/internal/sema"
)

// Compiler orchestrates the entire compilation pipeline:
// Lexer → Parser → Semantic Analysis → Code Generation
type Compiler struct {
	lexer   *lexer.Lexer
	parser  *parser.Parser
	sema    *sema.Analyzer
	codegen *codegen.Codegen
}

// New creates a new Compiler with fresh instances of each stage.
func New() *Compiler {
	return &Compiler{
		sema:    sema.New(),
		codegen: codegen.New(),
	}
}

// CompileSource compiles MAML source code and returns the generated LLVM IR.
func (c *Compiler) CompileSource(src string) (string, error) {
	// 1. Lexical Analysis
	c.lexer = lexer.New(src)
	c.parser = parser.New(c.lexer)

	// 2. Parsing
	program := c.parser.ParseProgram()
	if errs := c.parser.Errors(); len(errs) > 0 {
		return "", fmt.Errorf("parser errors:\n%s", formatErrors(errs))
	}

	// 3. Semantic Analysis
	// NEW: We now capture the typeMap!
	semaErrors, typeMap := c.sema.Analyze(program)
	if len(semaErrors) > 0 {
		return "", fmt.Errorf("semantic errors:\n%s", formatErrors(semaErrors))
	}

	// 4. Code Generation
	if err := c.codegen.Generate(program, typeMap); err != nil {
		return "", fmt.Errorf("code generation error: %w", err)
	}

	return c.codegen.String(), nil
}

// CompileFile reads a .maml file and compiles it.
func (c *Compiler) CompileFile(filename string) (string, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filename, err)
	}
	return c.CompileSource(string(src))
}

// Helper to format multiple errors nicely
func formatErrors(errs []string) string {
	var out string
	for _, e := range errs {
		out += "  - " + e + "\n"
	}
	return out
}

// Diagnostics returns any errors from the last compilation (for future IDE use)
func (c *Compiler) Diagnostics() []string {
	// TODO: Aggregate parser + sema errors with positions in the future
	return nil
}
