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
	expectedOutStart = regexp.MustCompile(`//\s*EXPECTED_OUTPUT:`)
	expectedErrRegex = regexp.MustCompile(`//\s*EXPECTED_ERROR:\s*(.+)`)
)

func TestEndToEnd(t *testing.T) {
	matches, err := filepath.Glob("../programs/*.maml")
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

			binPath, compileErr := d.BuildTemporary(srcPath)

			if expectedErr != "" {
				require.Error(t, compileErr)
				assert.Contains(t, compileErr.Error(), strings.TrimSpace(expectedErr))
				return
			}

			require.NoError(t, compileErr)
			defer os.Remove(binPath)

			actualExit, actualOut := runBinary(t, binPath)

			assert.Equal(t, expectedExit, actualExit)

			if expectedOut != "" {
				// Normalize both sides: trim + collapse whitespace
				normalizedExpected := normalizeOutput(expectedOut)
				normalizedActual := normalizeOutput(actualOut)

				assert.Equal(t, normalizedExpected, normalizedActual)
			}
		})
	}
}

// parseExpectedOutput supports multi-line output
func parseExpectedOutput(src string) string {
	lines := strings.Split(src, "\n")
	var outputLines []string
	inOutput := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if expectedOutStart.MatchString(line) {
			inOutput = true
			// Capture anything after EXPECTED_OUTPUT: on the same line
			if idx := strings.Index(line, "EXPECTED_OUTPUT:"); idx != -1 {
				rest := strings.TrimSpace(line[idx+len("EXPECTED_OUTPUT:"):])
				if rest != "" {
					outputLines = append(outputLines, rest)
				}
			}
			continue
		}

		if inOutput {
			if strings.HasPrefix(line, "//") {
				// Continue collecting indented or continued comment lines
				clean := strings.TrimPrefix(line, "//")
				clean = strings.TrimSpace(clean)
				if clean != "" {
					outputLines = append(outputLines, clean)
				}
			} else {
				// Stop when we hit non-comment line
				break
			}
		}
	}

	return strings.Join(outputLines, "\n")
}

func parseExitCode(src string) int {
	m := exitCodeRegex.FindStringSubmatch(src)
	if len(m) < 2 {
		return 0
	}
	val, _ := strconv.Atoi(strings.TrimSpace(m[1]))
	return val
}

func parseExpectedError(src string) string {
	m := expectedErrRegex.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func normalizeOutput(s string) string {
	s = strings.TrimSpace(s)
	// Replace all whitespace sequences with single space, or keep newlines if you prefer exact match
	// For now, let's do exact line-by-line match after trimming
	return s
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