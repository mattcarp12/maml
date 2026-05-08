package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mattcarp12/maml/internal/compiler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPrograms = []struct {
	name         string
	file         string
	expectedExit int
}{
	{"ret42", "../programs/ret42.maml", 42},
	{"fncall", "../programs/fncall.maml", 42},
	{"struct1", "../programs/struct1.maml", 30},
	{"ifelse", "../programs/ifelse.maml", 10},
	{"puts", "../programs/puts.maml", 0},
}

func TestEndToEnd(t *testing.T) {
	for _, tc := range testPrograms {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Compile using the full compiler pipeline
			c := compiler.New()
			llvmIR, err := c.CompileFile(tc.file)
			require.NoError(t, err, "compilation failed")

			// 2. Build and run the binary
			exitCode := buildAndRun(t, llvmIR, tc.name)
			assert.Equal(t, tc.expectedExit, exitCode,
				"program exited with wrong status code")
		})
	}
}

// buildAndRun writes IR, compiles with clang, runs the binary, and returns exit code
func buildAndRun(t *testing.T, llvmIR string, testName string) int {
	// Write LLVM IR
	irPath := filepath.Join(os.TempDir(), testName+".ll")
	err := os.WriteFile(irPath, []byte(llvmIR), 0644)
	require.NoError(t, err)
	defer os.Remove(irPath)

	// Compile with clang
	binPath := filepath.Join(os.TempDir(), testName+".exe")
	cmd := exec.Command("clang", "-Wno-override-module", irPath, "-o", binPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clang failed:\n%s", string(output))
	}
	defer os.Remove(binPath)

	// Run the binary
	cmd = exec.Command(binPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		t.Fatalf("failed to run binary: %v", err)
	}
	return 0
}
