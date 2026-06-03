package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mattcarp12/maml/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	exitCodeRegex    = regexp.MustCompile(`//\s*EXIT_CODE:\s*(\d+)`)
	expectedOutRegex = regexp.MustCompile(`//\s*EXPECTED_OUTPUT:\s*(.+)`)
	expectedErrRegex = regexp.MustCompile(`//\s*EXPECTED_ERROR:\s*(.+)`)
)

func TestEndToEnd(t *testing.T) {
	matches, err := filepath.Glob("../programs/*.maml") // Flat directory

	require.NoError(t, err)
	require.NotEmpty(t, matches)

	d := driver.New(driver.Config{})

	for _, srcPath := range matches {
		testName := strings.TrimSuffix(filepath.Base(srcPath), ".maml")

		t.Run(testName, func(t *testing.T) {
			content, err := os.ReadFile(srcPath)
			require.NoError(t, err)

			src := string(content)

			expectedExit := parseExitCode(src)
			expectedOut := parseExpectedOutput(src)
			expectedErr := parseExpectedError(src)

			// -----------------------------------------------------------------
			// Build executable through the driver pipeline
			// -----------------------------------------------------------------

			binPath, compileErr := d.BuildTemporary(srcPath)

			if expectedErr != "" {
				require.Error(t, compileErr)
				assert.Contains(t, compileErr.Error(), strings.TrimSpace(expectedErr))
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
				assert.Equal(t,
					strings.TrimSpace(expectedOut),
					strings.TrimSpace(actualOut),
				)
			}
		})
	}
}

func parseExitCode(src string) int {
	matches := exitCodeRegex.FindStringSubmatch(src)
	if len(matches) < 2 {
		return 0 // default
	}
	val, _ := strconv.Atoi(strings.TrimSpace(matches[1]))
	return val
}

func parseExpectedOutput(src string) string {
	matches := expectedOutRegex.FindStringSubmatch(src)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func parseExpectedError(src string) string {
	matches := expectedErrRegex.FindStringSubmatch(src)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
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
