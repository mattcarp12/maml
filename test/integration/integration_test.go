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

func TestEndToEnd(t *testing.T) {
	// Automatically find all .maml files in the programs directory
	matches, err := filepath.Glob("../programs/*/*.maml")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "No .maml test files found!")

	for _, srcPath := range matches {
		testName := strings.TrimSuffix(filepath.Base(srcPath), ".maml")
		dir := filepath.Dir(srcPath)

		t.Run(testName, func(t *testing.T) {
			// 1. Read expected outcome files
			expectedExit := readIntFile(filepath.Join(dir, testName+".exitcode.txt"), 0)
			expectedOut := readFile(filepath.Join(dir, testName+".expected.txt"))
			expectedErr := readFile(filepath.Join(dir, testName+".expected_err.txt"))

			// 2. Compile
			c := compiler.New()
			llvmIR, compileErr := c.CompileFile(srcPath)

			// 3. Handle Expected Compilation Errors (Negative Tests)
			if expectedErr != "" {
				require.Error(t, compileErr, "Expected a compilation error but got none")
				assert.Contains(t, compileErr.Error(), strings.TrimSpace(expectedErr), "Compilation error did not match expected")
				return // Test passes successfully!
			}

			// 4. Ensure successful compilation
			require.NoError(t, compileErr, "Compilation failed unexpectedly")

			// 5. Build and Run
			actualExit, actualOut := buildAndRun(t, llvmIR, testName)

			// 6. Assertions
			assert.Equal(t, expectedExit, actualExit, "Exit code mismatch")

			if expectedOut != "" {
				assert.Equal(t, strings.TrimSpace(expectedOut), strings.TrimSpace(actualOut), "Stdout mismatch")
			}
		})
	}
}

// buildAndRun writes IR, compiles with clang, runs the binary, and returns (exitCode, stdout)
func buildAndRun(t *testing.T, llvmIR string, testName string) (int, string) {
	irPath := filepath.Join(os.TempDir(), testName+".ll")
	err := os.WriteFile(irPath, []byte(llvmIR), 0644)
	require.NoError(t, err)
	defer os.Remove(irPath)
	runtimeLibPath := "../../runtime/zig-out/lib/libmamlrt.a"
	binPath := filepath.Join(os.TempDir(), testName+".exe")
	cmd := exec.Command("clang",
		"-Wno-override-module",
		irPath,
		runtimeLibPath,
		"-Wl,-z,noexecstack", // Silences the GNU-stack linker warning
		"-o", binPath,
		"-lm",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "clang failed:\n%s", string(output))
	defer os.Remove(binPath)

	var stdout bytes.Buffer
	cmd = exec.Command(binPath)
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

// Helpers
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
