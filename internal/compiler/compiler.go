package compiler

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/codegen"
	"github.com/mattcarp12/maml/internal/escape"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/ownership"
	"github.com/mattcarp12/maml/internal/parser"
	"github.com/mattcarp12/maml/internal/sema"
)

type Compiler struct{}

type Result struct {
	AST     *ast.Program
	TypeMap map[ast.Node]sema.Type
	LLVMIR  string
}

func New() *Compiler {
	return &Compiler{}
}

// compile executes the full compiler pipeline layout sequentially
func (c *Compiler) compile(src string) (*Result, error) {
	// 1. Lexical & Syntactic Analysis
	l := lexer.New(src)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parser syntax error: %s", p.Errors()[0])
	}

	// 2. Pure Semantic Analysis (Type-checking)
	semaAnalyzer := sema.New()
	semaErrors, typeMap := semaAnalyzer.Analyze(program)
	if len(semaErrors) > 0 {
		return nil, semaErrors[0]
	}

	// 3. Ownership & Borrow Lifetime Analysis
	ownershipAnalyzer := ownership.New(typeMap)
	ownershipErrors := ownershipAnalyzer.Analyze(program)
	if len(ownershipErrors) > 0 {
		return nil, ownershipErrors[0]
	}

	// 4. Escape Analysis
	escapeAnalyzer := escape.New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	// 5. LLVM IR Code Generation
	cg := codegen.New()
	if err := cg.Generate(program, typeMap, escapeMap); err != nil {
		return nil, fmt.Errorf("codegen generation failed: %w", err)
	}
	generatedLLVMIR := cg.String()

	return &Result{
		AST:     program,
		TypeMap: typeMap,
		LLVMIR:  generatedLLVMIR,
	}, nil
}

// compileSource compiles a raw string of source code text and returns the generated LLVM IR string
func (c *Compiler) compileSource(src string) (string, error) {
	res, err := c.compile(src)
	if err != nil {
		return "", err
	}
	return res.LLVMIR, nil
}

// CompileFile reads a target file path from disk and feeds its raw text contents into CompileSource
func (c *Compiler) CompileFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to open source target %s: %w", path, err)
	}
	return c.compileSource(string(content))
}

// InvokeClang acts as a shared compiler-tier utility to link against the MAML runtime library
func InvokeClang(llvmIR, outName, runtimeLibPath string) error {
	irFile := outName + ".ll"
	if err := os.WriteFile(irFile, []byte(llvmIR), 0644); err != nil {
		return fmt.Errorf("failed to write intermediate assembly target: %w", err)
	}
	defer os.Remove(irFile)

	cmd := exec.Command("clang",
		"-Wno-override-module",
		irFile,
		runtimeLibPath,
		"-Wl,-z,noexecstack",
		"-o", outName,
		"-lm",
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("linker invocation failed:\n%s", string(output))
	}
	return nil
}
