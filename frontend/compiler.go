package frontend

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/codegen"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/lexer"
	"github.com/mattcarp12/maml/frontend/ownership"
	"github.com/mattcarp12/maml/frontend/parser"
	"github.com/mattcarp12/maml/frontend/sema"
)

// NEW: A placeholder for your async lowering pass
type AsyncLowerer struct{}

func (a *AsyncLowerer) Mutate(node ast.Node) ast.Node {
	// For now, it just returns the node untouched.
	// When we implement await, this is where the AST gets rewritten into state machines!
	return node
}

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

	// 2. THE REFACTOR: Run the Mutator over the AST before Semantic Analysis
	lowerer := &AsyncLowerer{}
	program = ast.Rewrite(lowerer, program).(*ast.Program)

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

	cmd := exec.Command("clang", "-v",
		"-O2",
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

// In internal/compiler/compiler.go
func (c *Compiler) CompileToJSON(src string, jsonOut io.Writer) error {
	// 1. Lex & Parse
	program := parser.New(lexer.New(src)).ParseProgram()

	// 2. Semantic Analysis
	analyzer := sema.New()
	errs, typeMap := analyzer.Analyze(program)
	if len(errs) > 0 {
		return errs[0]
	}

	// 3. Escape Analysis
	escapeAnalyzer := escape.New(typeMap)
	escapeMap := escapeAnalyzer.Analyze(program)

	// 4. NEW: Emit Structured JSON for C++ Backend!
	emitter := NewEmitter(typeMap, escapeMap)
	return emitter.Emit(program, jsonOut)
}

type Emitter struct {
	typeMap   map[ast.Node]sema.Type
	escapeMap map[ast.Node]escape.EscapeState
}

func NewEmitter(typeMap map[ast.Node]sema.Type, escapeMap map[ast.Node]escape.EscapeState) *Emitter {
	return &Emitter{typeMap: typeMap, escapeMap: escapeMap}
}

func (e *Emitter) Emit(program *ast.Program, w io.Writer) error {
	jsonAST := e.marshalNode(program)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonAST)
}

func (e *Emitter) marshalNode(node ast.Node) map[string]interface{} {
	if node == nil {
		return nil
	}

	res := make(map[string]interface{})

	// Inject Type and Escape Metadata if available
	if t, ok := e.typeMap[node]; ok && t != nil {
		res["maml_type"] = e.marshalType(t)
	}
	if esc, ok := e.escapeMap[node]; ok {
		res["is_heap"] = esc == escape.StateHeap
	}

	// Dynamic dispatch based on AST node type
	switch n := node.(type) {
	case *ast.Program:
		res["node_type"] = "Program"
		var decls []interface{}
		for _, d := range n.Decls {
			decls = append(decls, e.marshalNode(d))
		}
		res["decls"] = decls

	case *ast.FnDecl:
		res["node_type"] = "FnDecl"
		res["name"] = n.Name
		res["is_async"] = n.IsAsync
		var params []interface{}
		for _, p := range n.Params {
			params = append(params, map[string]interface{}{
				"name": p.Name,
				"type": e.marshalType(e.typeMap[p.Type]),
				"own":  p.Own,
				"mut":  p.Mut,
			})
		}
		res["params"] = params
		res["body"] = e.marshalNode(n.Body)

	case *ast.BlockStmt:
		res["node_type"] = "BlockStmt"
		var stmts []interface{}
		for _, s := range n.Statements {
			stmts = append(stmts, e.marshalNode(s))
		}
		res["statements"] = stmts

	case *ast.DeclareStmt:
		res["node_type"] = "DeclareStmt"
		res["name"] = n.Name
		res["mutable"] = n.Mutable
		res["value"] = e.marshalNode(n.Value)

	case *ast.AssignStmt:
		res["node_type"] = "AssignStmt"
		res["lvalue"] = e.marshalNode(n.LValue)
		res["rvalue"] = e.marshalNode(n.RValue)

	case *ast.ReturnStmt:
		res["node_type"] = "ReturnStmt"
		res["value"] = e.marshalNode(n.Value)

	case *ast.AwaitExpr:
		res["node_type"] = "AwaitExpr"
		res["value"] = e.marshalNode(n.Value)

	case *ast.CallExpr:
		res["node_type"] = "CallExpr"
		res["function"] = e.marshalNode(n.Function)
		var args []interface{}
		for _, arg := range n.Arguments {
			args = append(args, map[string]interface{}{
				"value": e.marshalNode(arg.Argument),
				"own":   arg.Own,
				"mut":   arg.Mut,
			})
		}
		res["arguments"] = args

	case *ast.InfixExpr:
		res["node_type"] = "InfixExpr"
		res["operator"] = n.Operator
		res["left"] = e.marshalNode(n.Left)
		res["right"] = e.marshalNode(n.Right)

	case *ast.Identifier:
		res["node_type"] = "Identifier"
		res["value"] = n.Value

	case *ast.IntLiteral:
		res["node_type"] = "IntLiteral"
		res["value"] = n.Value

	case *ast.BoolLiteral:
		res["node_type"] = "BoolLiteral"
		res["value"] = n.Value

	case *ast.StringLiteral:
		res["node_type"] = "StringLiteral"
		res["value"] = n.Value
	}

	return res
}

func (e *Emitter) marshalType(t sema.Type) map[string]interface{} {
	if t == nil {
		return nil
	}
	res := make(map[string]interface{})

	switch v := t.(type) {
	case sema.IntType:
		res["kind"] = "Int"
	case sema.BoolType:
		res["kind"] = "Bool"
	case sema.StringType:
		res["kind"] = "String"
	case sema.TaskType:
		res["kind"] = "Task"
		res["result"] = e.marshalType(v.Base)
	case sema.VectorType:
		res["kind"] = "Vector"
		res["base"] = e.marshalType(v.Base)
	case *sema.StructType:
		res["kind"] = "Struct"
		res["name"] = v.Name
	case *sema.FunctionType:
		res["kind"] = "Function"
		res["return"] = e.marshalType(v.Return)
	default:
		res["kind"] = "Unknown"
	}
	return res
}
