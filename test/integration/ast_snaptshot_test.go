package integration

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattcarp12/maml/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateSnapshots = flag.Bool("update", false, "update snapshot files markers")

func TestASTSnapshots(t *testing.T) {
	// Scans all test program directories cleanly
	matches, err := filepath.Glob("../programs/*/*.maml")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	d := driver.New(driver.Config{})

	for _, srcPath := range matches {
		testName := strings.TrimSuffix(
			filepath.Base(srcPath),
			".maml",
		) //

		dir := filepath.Dir(srcPath)
		parserErrPath := filepath.Join(dir, testName+".parser_err.txt")
		snapshotPath := filepath.Join(dir, testName+".ast.json")

		t.Run(testName, func(t *testing.T) {
			// Read the expected parser error file if it exists
			expectedParserErrBytes, err := os.ReadFile(parserErrPath)
			isParserErrorTest := err == nil

			// Execute syntax analysis up to the AST level
			currentBytes, compileErr := d.DumpAST(srcPath)

			// -----------------------------------------------------------------
			// Case 1: Handle intentional syntax/parser error test cases
			// -----------------------------------------------------------------
			if isParserErrorTest {
				require.Error(t, compileErr, "expected parser error for %s but it parsed successfully", srcPath)

				expectedErrStr := strings.TrimSpace(string(expectedParserErrBytes))
				actualErrStr := strings.TrimSpace(compileErr.Error())

				assert.Contains(
					t,
					actualErrStr,
					expectedErrStr,
					"The emitted parser error message did not contain the expected text baseline",
				)
				return
			}

			// -----------------------------------------------------------------
			// Case 2: Standard valid program AST snapshot verification
			// -----------------------------------------------------------------
			require.NoError(t, compileErr)

			if *updateSnapshots {
				err := os.WriteFile(snapshotPath, currentBytes, 0644)
				require.NoError(t, err, "failed to write snapshot baseline file")
				return
			}

			expectedBytes, err := os.ReadFile(snapshotPath)
			require.NoError(t, err, "missing baseline snapshot file; run with '-update' to create it")

			assert.Equal(
				t,
				strings.TrimSpace(string(expectedBytes)),
				strings.TrimSpace(string(currentBytes)),
				"AST Snapshot mismatch found for %s", srcPath,
			)
		})
	}
}
