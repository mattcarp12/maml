package main

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
var DefaultRuntimeLib = "./build/runtime/lib/libmamlrt.a"

func init() {
	if root := os.Getenv("MAML_ROOT"); root != "" {
		DefaultRuntimeLib = filepath.Join(root, "build/runtime/lib/libmamlrt.a")
	}
}

type Config struct {
	OutputPath string
	PrintIR    bool
	RuntimeLib string
	Sanitize   bool
	Target     mir.Target
}

type Pipeline struct {
	cfg Config
}

func NewPipeline(cfg Config) *Pipeline {
	if cfg.RuntimeLib == "" {
		cfg.RuntimeLib = DefaultRuntimeLib
	}
	cfg.Target = *mir.DefaultTarget
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
	mirBytes, err := mir.MarshalProgram(res.MIR, &p.cfg.Target)
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
		// "-O2",
		"-Os", // optimize for size instead of speed
		"--target=x86_64-linux-musl",
		"-static",
		"-ffunction-sections",
		"-fdata-sections",
		"-fno-rtti",
		"-fno-exceptions",
		"-flto",             // enable link-time optimization
		"-fuse-ld=lld",      // use LLVM's linker for better cross-platform support
		"-Wl,--gc-sections", // remove unused functions
		"-Wl,-s",            // strip symbol table
		"-Wno-override-module",
		llPath,
		p.cfg.RuntimeLib,
		"-o", p.cfg.OutputPath,
	}

	if p.cfg.Sanitize {
		args = append(args, "-fsanitize=address", "-g", "-fno-omit-frame-pointer")
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

// dumpJSON reads a file, compiles it with fn, and returns the JSON-marshalled result.
func (p *Pipeline) dumpJSON(srcPath string, fn func(string) (any, error)) ([]byte, error) {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source target %s: %w", srcPath, err)
	}

	prog, err := fn(string(content))
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(prog)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize to JSON: %w", err)
	}
	return jsonBytes, nil
}

// DumpAST parses the target file and returns its JSON AST representation.
func (p *Pipeline) DumpAST(srcPath string) ([]byte, error) {
	return p.dumpJSON(srcPath, func(src string) (any, error) {
		return frontend.New().CompileAST(src)
	})
}

// DumpTAST parses the target file and returns its JSON TAST representation.
func (p *Pipeline) DumpTAST(srcPath string) ([]byte, error) {
	return p.dumpJSON(srcPath, func(src string) (any, error) {
		return frontend.New().CompileTAST(src)
	})
}

// DumpHIR parses the target file and returns its JSON HIR representation.
func (p *Pipeline) DumpHIR(srcPath string) ([]byte, error) {
	return p.dumpJSON(srcPath, func(src string) (any, error) {
		tast, err := frontend.New().CompileTAST(src)
		if err != nil {
			return nil, err
		}
		return hir.NewTASTLowerer().LowerProgram(tast), nil
	})
}

// DumpMIR translates the target file and returns its pretty-printed JSON MIR representation.
func (p *Pipeline) DumpMIR(srcPath string) ([]byte, error) {
	comp := frontend.New()
	res, err := comp.CompileFile(srcPath)
	if res == nil || res.MIR == nil {
		return nil, err
	}
	return mir.MarshalProgram(res.MIR, &p.cfg.Target)
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
	mirBytes, err := mir.MarshalProgram(res.MIR, &p.cfg.Target)
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

// phaseSpec describes a dumpable compiler phase.
type phaseSpec struct {
	key   string
	title string
	fn    func(*Pipeline, string) ([]byte, error)
}

// allPhases defines the order and metadata for every dumpable phase.
var allPhases = []phaseSpec{
	{"source", "PHASE 0: SOURCE CODE", func(_ *Pipeline, src string) ([]byte, error) { return os.ReadFile(src) }},
	{"ast", "PHASE 1: ABSTRACT SYNTAX TREE (AST)", (*Pipeline).DumpAST},
	{"tast", "PHASE 2: TYPED ABSTRACT SYNTAX TREE (TAST)", (*Pipeline).DumpTAST},
	{"hir", "PHASE 2.5: DESUGARED TAST (DTAST)", (*Pipeline).DumpHIR},
	{"mir", "PHASE 3: MID-LEVEL IR (MIR)", (*Pipeline).DumpMIR},
	{"llvm", "PHASE 4: LLVM IR", (*Pipeline).DumpLLVM},
}

// DumpPhases emits the requested compiler phases.
// When multiple phases are requested, each is separated by a header banner.
func (p *Pipeline) DumpPhases(srcPath string, requested []string) ([]byte, error) {
	phaseMap := make(map[string]phaseSpec, len(allPhases))
	for _, ph := range allPhases {
		phaseMap[ph.key] = ph
	}

	var buf bytes.Buffer
	multi := len(requested) > 1

	for i, key := range requested {
		ph, ok := phaseMap[key]
		if !ok {
			valid := make([]string, 0, len(phaseMap))
			for k := range phaseMap {
				valid = append(valid, k)
			}
			return nil, fmt.Errorf("unknown phase %q (valid: %s)", key, strings.Join(valid, ", "))
		}

		content, err := ph.fn(p, srcPath)
		if err != nil {
			return nil, fmt.Errorf("failed to dump %s: %w", key, err)
		}

		if multi {
			title := ph.title
			if key == "source" {
				title = fmt.Sprintf("PHASE 0: SOURCE CODE (%s)", srcPath)
			}
			fmt.Fprintf(&buf, "// %s\n", strings.Repeat("=", 76))
			fmt.Fprintf(&buf, "// %s\n", title)
			fmt.Fprintf(&buf, "// %s\n\n", strings.Repeat("=", 76))
		}

		buf.Write(content)

		if multi && i < len(requested)-1 {
			buf.WriteString("\n\n")
		}
	}

	return buf.Bytes(), nil
}

// DumpAll generates all compiler phases and writes them to a single file
// segregated by clear header comments.
func (p *Pipeline) DumpAll(srcPath string, outPath string) error {
	keys := make([]string, 0, len(allPhases))
	for _, ph := range allPhases {
		keys = append(keys, ph.key)
	}

	out, err := p.DumpPhases(srcPath, keys)
	if err != nil {
		return err
	}

	return os.WriteFile(outPath, out, 0644)
}
