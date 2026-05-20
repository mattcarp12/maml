package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mattcarp12/maml/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndToEnd(t *testing.T) {
	matches, err := filepath.Glob("../programs/*/*.maml")

	require.NoError(t, err)
	require.NotEmpty(t, matches)

	d := driver.New(driver.Config{})

	for _, srcPath := range matches {
		testName := strings.TrimSuffix(
			filepath.Base(srcPath),
			".maml",
		)

		dir := filepath.Dir(srcPath)

		t.Run(testName, func(t *testing.T) {
			expectedExit := readIntFile(
				filepath.Join(dir, testName+".exitcode.txt"),
				0,
			)

			expectedOut := readFile(
				filepath.Join(dir, testName+".expected.txt"),
			)

			expectedErr := readFile(
				filepath.Join(dir, testName+".expected_err.txt"),
			)

			// -----------------------------------------------------------------
			// Build executable through the driver pipeline
			// -----------------------------------------------------------------

			binPath, compileErr := d.BuildTemporary(srcPath)

			if expectedErr != "" {
				require.Error(t, compileErr)

				assert.Contains(
					t,
					compileErr.Error(),
					strings.TrimSpace(expectedErr),
				)

				return
			}

			require.NoError(t, compileErr)

			defer os.Remove(binPath)

			// -----------------------------------------------------------------
			// Execute binary
			// -----------------------------------------------------------------

			actualExit, actualOut := runBinary(t, binPath)

			assert.Equal(t, expectedExit, actualExit)

			if expectedOut != "" {
				assert.Equal(
					t,
					strings.TrimSpace(expectedOut),
					strings.TrimSpace(actualOut),
				)
			}
		})
	}
}

func runBinary(t *testing.T, binPath string) (int, string) {
	var stdout bytes.Buffer

	cmd := exec.Command(binPath)

	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

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
