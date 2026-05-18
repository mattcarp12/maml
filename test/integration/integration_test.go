package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mattcarp12/maml/internal/compiler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRuntimeLibPath = "../../runtime/zig-out/lib/libmamlrt.a"

func TestEndToEnd(t *testing.T) {
	matches, err := filepath.Glob("../programs/*/*.maml")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "No .maml test files found!")

	for _, srcPath := range matches {
		testName := strings.TrimSuffix(filepath.Base(srcPath), ".maml")
		dir := filepath.Dir(srcPath)

		t.Run(testName, func(t *testing.T) {
			expectedExit := readIntFile(filepath.Join(dir, testName+".exitcode.txt"), 0)
			expectedOut := readFile(filepath.Join(dir, testName+".expected.txt"))
			expectedErr := readFile(filepath.Join(dir, testName+".expected_err.txt"))

			c := compiler.New()
			// FIXED: Compiles directly via file tracking pipeline, running type and borrow checking safely
			llvmIR, compileErr := c.CompileFile(srcPath)

			if expectedErr != "" {
				require.Error(t, compileErr, "Expected a compilation error but got none")
				assert.Contains(t, compileErr.Error(), strings.TrimSpace(expectedErr), "Compilation error did not match expected")
				return
			}

			require.NoError(t, compileErr, "Compilation failed unexpectedly")

			actualExit, actualOut := buildAndRun(t, llvmIR, testName)

			assert.Equal(t, expectedExit, actualExit, "Exit code mismatch")

			if expectedOut != "" {
				assert.Equal(t, strings.TrimSpace(expectedOut), strings.TrimSpace(actualOut), "Stdout mismatch")
			}
		})
	}
}

func buildAndRun(t *testing.T, llvmIR string, testName string) (int, string) {
	binPath := filepath.Join(os.TempDir(), testName+".exe")

	// Utilizes the shared compilation utility to prevent duplicate shell logic orchestration
	err := compiler.InvokeClang(llvmIR, binPath, testRuntimeLibPath)
	require.NoError(t, err, "Clang assembly step failed")
	defer os.Remove(binPath)

	var stdout bytes.Buffer
	cmd := exec.Command(binPath)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run binary: %v", err)
	}

	return exitCode, stdout.String()
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readIntFile(path string, defaultVal int) int {
	str := readFile(path)
	if str == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(strings.TrimSpace(str))
	if err != nil {
		return defaultVal
	}
	return val
}
