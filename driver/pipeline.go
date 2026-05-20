package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mattcarp12/maml/frontend"
	"github.com/mattcarp12/maml/hir"
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

	// 2. Temp Directory for Intermediate Build Files
	tempDir, err := os.MkdirTemp("", "maml_build_*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	jsonPath := filepath.Join(tempDir, "ast.json")
	llPath := filepath.Join(tempDir, "output.ll")

	// 3. Emit JSON
	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to create json file: %w", err)
	}
	emitter := hir.NewEmitter(res.TypeMap, res.EscapeMap)
	if err := emitter.Emit(res.Program, jsonFile); err != nil {
		jsonFile.Close()
		return fmt.Errorf("failed to emit HIR JSON: %w", err)
	}
	jsonFile.Close()

	// 4. Locate and Execute the C++ Backend
	backendBin := "maml-backend"
	if execPath, err := os.Executable(); err == nil {
		localBackend := filepath.Join(filepath.Dir(execPath), "maml-backend")
		if _, err := os.Stat(localBackend); err == nil {
			backendBin = localBackend
		}
	}

	backendCmd := exec.Command(backendBin, jsonPath)
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

	// Optional: Print IR
	if p.cfg.PrintIR {
		irBytes, _ := os.ReadFile(llPath)
		fmt.Println("=== Generated LLVM IR ===")
		fmt.Println(string(irBytes))
	}

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
