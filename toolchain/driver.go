package toolchain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mattcarp12/maml/frontend"
)

const runtimeLibPath = "./runtime/zig-out/lib/libmamlrt.a"

type Driver struct{}

func New() *Driver {
	return &Driver{}
}

func (d *Driver) Check(path string) error {
	c := frontend.New()

	_, err := c.CompileFile(path)
	return err
}

func (d *Driver) Build(path string, out string, printIR bool) error {
	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	llvmIR, err := d.generateLLVMIR(string(srcBytes), printIR)
	if err != nil {
		return err
	}

	return CompileLLVMIR(
		llvmIR,
		ClangOptions{
			OutputPath: out,
			RuntimeLib: runtimeLibPath,
		},
	)
}

func (d *Driver) Run(path string, printIR bool) error {
	outName := filepath.Join(os.TempDir(), "maml_run_tmp")

	if err := d.Build(path, outName, printIR); err != nil {
		return err
	}

	defer os.Remove(outName)

	cmd := exec.Command(outName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (d *Driver) generateLLVMIR(src string, printIR bool) (string, error) {
	c := frontend.New()

	// ---------------------------------------------------------------------
	// Emit HIR JSON
	// ---------------------------------------------------------------------

	tmpJSON, err := os.CreateTemp("", "maml_hir_*.json")
	if err != nil {
		return "", err
	}

	defer os.Remove(tmpJSON.Name())

	if err := c.EmitHIR(src, tmpJSON); err != nil {
		return "", err
	}

	tmpJSON.Close()

	// ---------------------------------------------------------------------
	// Locate backend executable
	// ---------------------------------------------------------------------

	backendBin := "maml-backend"

	execPath, err := os.Executable()
	if err == nil {
		localBackend := filepath.Join(filepath.Dir(execPath), "maml-backend")

		if _, err := os.Stat(localBackend); err == nil {
			backendBin = localBackend
		}
	}

	// ---------------------------------------------------------------------
	// Invoke C++ backend
	// ---------------------------------------------------------------------

	cmd := exec.Command(backendBin, tmpJSON.Name())

	llvmIRBytes, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf(
				"backend generation failure:\n%s",
				string(exitErr.Stderr),
			)
		}

		return "", fmt.Errorf(
			"failed to execute backend (%s): %w",
			backendBin,
			err,
		)
	}

	llvmIR := string(llvmIRBytes)

	if printIR {
		fmt.Println("=== Generated LLVM IR ===")
		fmt.Println(llvmIR)
	}

	return llvmIR, nil
}

func (d *Driver) BuildTemporary(srcPath string) (string, error) {
	outPath := filepath.Join(
		os.TempDir(),
		filepath.Base(srcPath)+".exe",
	)

	if err := d.Build(srcPath, outPath, false); err != nil {
		return "", err
	}

	return outPath, nil
}
