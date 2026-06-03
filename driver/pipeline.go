package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattcarp12/maml/frontend"
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
)

// DefaultRuntimeLib gracefully handles being run from the CLI or inside tests
var DefaultRuntimeLib = "./runtime/zig-out/lib/libmamlrt.a"

func init() {
	if root := os.Getenv("MAML_ROOT"); root != "" {
		DefaultRuntimeLib = filepath.Join(root, "runtime/zig-out/lib/libmamlrt.a")
	}
}

type Config struct {
	OutputPath string
	PrintIR    bool
	RuntimeLib string
}

type Pipeline struct {
	cfg Config
}

func New(cfg Config) *Pipeline {
	if cfg.RuntimeLib == "" {
		cfg.RuntimeLib = DefaultRuntimeLib
	}
	return &Pipeline{cfg: cfg}
}

// Check merely runs the frontend to validate syntax and semantics.
func (p *Pipeline) Check(srcPath string) error {
	comp := frontend.New()
	_, err := comp.CompileFile(srcPath)
	return err
}

// Build compiles the source code into a native executable.
func (p *Pipeline) Build(srcPath string) error {
	// 1. Frontend
	comp := frontend.New()
	res, err := comp.CompileFile(srcPath)
	if err != nil {
		return err
	}

	// 2. Serialize MIR to JSON bytes in-memory
	mirBytes, err := mir.MarshalProgram(res.MIR)
	if err != nil {
		return fmt.Errorf("failed to serialize MIR: %w", err)
	}

	// 3. Temp Directory (Only needed for the intermediate LLVM IR file now)
	tempDir, err := os.MkdirTemp("", "maml_build_*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	llPath := filepath.Join(tempDir, "output.ll")

	// 4. Locate and Execute the C++ Backend
	backendBin := "maml-backend"
	if execPath, err := os.Executable(); err == nil {
		localBackend := filepath.Join(filepath.Dir(execPath), "maml-backend")
		if _, err := os.Stat(localBackend); err == nil {
			backendBin = localBackend
		}
	}

	// Do NOT pass a file path argument; the C++ backend will read from stdin
	backendCmd := exec.Command(backendBin)

	// Pipe the in-memory JSON bytes directly to the backend's stdin
	backendCmd.Stdin = bytes.NewReader(mirBytes)

	llFile, err := os.Create(llPath)
	if err != nil {
		return fmt.Errorf("failed to create LLVM IR file: %w", err)
	}
	backendCmd.Stdout = llFile

	// Capture stderr for backend compilation errors
	backendCmd.Stderr = os.Stderr
	if err := backendCmd.Run(); err != nil {
		llFile.Close()
		return fmt.Errorf("backend generation failure: %w", err)
	}
	llFile.Close()

	// 5. Clang Compilation & Linking
	args := []string{
		"-O2",
		"-Wno-override-module",
		llPath,
		p.cfg.RuntimeLib,
		"-Wl,-z,noexecstack",
		"-o", p.cfg.OutputPath,
		"-lpthread", "-ldl", "-lm",
	}

	clangCmd := exec.Command("clang++", args...)
	output, err := clangCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clang invocation failed:\n%s", string(output))
	}

	return nil
}

// BuildTemporary builds the binary to a temp directory and returns its path (Used for testing/running)
func (p *Pipeline) BuildTemporary(srcPath string) (string, error) {
	outName := filepath.Join(os.TempDir(), "maml_bin_tmp")

	// Temporarily override the output path
	originalOut := p.cfg.OutputPath
	p.cfg.OutputPath = outName
	err := p.Build(srcPath)
	p.cfg.OutputPath = originalOut

	return outName, err
}

// Run compiles and immediately executes the binary
func (p *Pipeline) Run(srcPath string) error {
	binPath, err := p.BuildTemporary(srcPath)
	if err != nil {
		return err
	}
	defer os.Remove(binPath)

	cmd := exec.Command(binPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// DumpAST parses the target file and returns its pretty-printed JSON AST representation.
func (p *Pipeline) DumpAST(srcPath string) ([]byte, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", srcPath, err)
	}

	comp := frontend.New()
	astProgram, err := comp.CompileAST(string(content))
	if err != nil {
		return nil, err
	}

	// MarshalIndent formats the JSON neatly for readable snapshot files.
	// Struct fields marked with `json:"-"` (like Pos_ and End_) are automatically skipped.
	jsonBytes, err := json.Marshal(astProgram)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize AST to JSON: %w", err)
	}

	return jsonBytes, nil
}

// DumpAST parses the target file and returns its pretty-printed JSON AST representation.
func (p *Pipeline) DumpTAST(srcPath string) ([]byte, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", srcPath, err)
	}

	comp := frontend.New()
	tastProgram, err := comp.CompileTAST(string(content))
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(tastProgram)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize AST to JSON: %w", err)
	}

	return jsonBytes, nil
}

// DumpAST parses the target file and returns its pretty-printed JSON AST representation.
func (p *Pipeline) DumpHIR(srcPath string) ([]byte, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", srcPath, err)
	}

	comp := frontend.New()
	tastProgram, err := comp.CompileTAST(string(content))
	if err != nil {
		return nil, err
	}

	hirLowerer := hir.NewLowerer()
	hirProgram := hirLowerer.LowerProgram(tastProgram)

	jsonBytes, err := json.Marshal(hirProgram)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize AST to JSON: %w", err)
	}

	return jsonBytes, nil
}

// DumpMIR translates the target file and returns its pretty-printed JSON MIR representation.
func (p *Pipeline) DumpMIR(srcPath string) ([]byte, error) {
	comp := frontend.New()
	res, err := comp.CompileFile(srcPath)
	if res == nil || res.MIR == nil {
		return nil, err
	}

	return mir.MarshalProgram(res.MIR)
}

// DumpLLVM translates the target file, invokes the C++ backend,
// and returns the generated LLVM IR as a byte slice.
func (p *Pipeline) DumpLLVM(srcPath string) ([]byte, error) {
	// 1. Frontend: Generate MIR
	comp := frontend.New()
	res, err := comp.CompileFile(srcPath)
	if err != nil {
		return nil, err
	}
	if res == nil || res.MIR == nil {
		return nil, fmt.Errorf("frontend failed to generate MIR")
	}

	// 2. Serialize MIR to JSON bytes in-memory
	mirBytes, err := mir.MarshalProgram(res.MIR)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize MIR: %w", err)
	}

	// 3. Locate and Execute the C++ Backend
	backendBin := "maml-backend"
	if execPath, err := os.Executable(); err == nil {
		localBackend := filepath.Join(filepath.Dir(execPath), "maml-backend")
		if _, err := os.Stat(localBackend); err == nil {
			backendBin = localBackend
		}
	}

	backendCmd := exec.Command(backendBin)
	backendCmd.Stdin = bytes.NewReader(mirBytes)

	// Capture both stdout (the LLVM IR) and stderr (compiler errors) in-memory
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	backendCmd.Stdout = &stdoutBuf
	backendCmd.Stderr = &stderrBuf

	if err := backendCmd.Run(); err != nil {
		return nil, fmt.Errorf("backend generation failure:\n%s\n%w", stderrBuf.String(), err)
	}

	return stdoutBuf.Bytes(), nil
}

// DumpAll generates all compiler phases and writes them to a single file
// segregated by clear header comments.
func (p *Pipeline) DumpAll(srcPath string, outPath string) error {
	// 1. Fetch all representations
	srcBytes, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	astBytes, err := p.DumpAST(srcPath)
	if err != nil {
		return fmt.Errorf("failed to dump AST: %w", err)
	}

	tastBytes, err := p.DumpTAST(srcPath)
	if err != nil {
		return fmt.Errorf("failed to dump TAST: %w", err)
	}

	dtastBytes, err := p.DumpHIR(srcPath)
	if err != nil {
		return fmt.Errorf("failed to dump DTAST: %w", err)
	}

	mirBytes, err := p.DumpMIR(srcPath)
	if err != nil {
		return fmt.Errorf("failed to dump MIR: %w", err)
	}

	llvmBytes, err := p.DumpLLVM(srcPath)
	if err != nil {
		return fmt.Errorf("failed to dump LLVM IR: %w", err)
	}

	// 2. Format the output with segregation headers
	var buf bytes.Buffer

	writeSection := func(title string, content []byte) {
		fmt.Fprintf(&buf, "// %s\n", strings.Repeat("=", 76))
		fmt.Fprintf(&buf, "// %s\n", title)
		fmt.Fprintf(&buf, "// %s\n\n", strings.Repeat("=", 76))
		buf.Write(content)
		buf.WriteString("\n\n")
	}

	writeSection(fmt.Sprintf("PHASE 0: SOURCE CODE (%s)", srcPath), srcBytes)
	writeSection("PHASE 1: ABSTRACT SYNTAX TREE (AST)", astBytes)
	writeSection("PHASE 2: TYPED ABSTRACT SYNTAX TREE (TAST)", tastBytes)
	writeSection("PHASE 2.5: DESUGARED TAST (DTAST)", dtastBytes)
	writeSection("PHASE 3: MID-LEVEL IR (MIR)", mirBytes)
	writeSection("PHASE 4: LLVM IR", llvmBytes)

	// 3. Write to the target file
	return os.WriteFile(outPath, buf.Bytes(), 0644)
}
