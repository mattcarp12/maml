package toolchain

import (
	"fmt"
	"os"
	"os/exec"
)

type ClangOptions struct {
	OutputPath    string
	RuntimeLib    string
	Optimization  string
	StaticLinking bool
}

func CompileLLVMIR(llvmIR string, opts ClangOptions) error {
	irFile := opts.OutputPath + ".ll"

	if err := os.WriteFile(irFile, []byte(llvmIR), 0644); err != nil {
		return fmt.Errorf("failed to write llvm ir file: %w", err)
	}

	defer os.Remove(irFile)

	args := []string{
		"-O2",
		"-Wno-override-module",
		irFile,
		opts.RuntimeLib,
		"-Wl,-z,noexecstack",
		"-o",
		opts.OutputPath,
		"-lm",
	}

	if opts.StaticLinking {
		args = append(args, "-static")
	}

	cmd := exec.Command("clang", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"clang invocation failed:\n%s",
			string(output),
		)
	}

	return nil
}
