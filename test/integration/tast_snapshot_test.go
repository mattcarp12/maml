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

// Run with `go test ./test/integration/... -update-tast` to regenerate baseline TAST snapshots
var updateTastSnapshots = flag.Bool("update-tast", false, "update .tast.json snapshot files markers")

func TestTASTSnapshots(t *testing.T) {
	// Scan the test program directories using the canonical project layout
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
		semaErrPath := filepath.Join(dir, testName+".sema_err.txt")
		snapshotPath := filepath.Join(dir, testName+".tast.json")

		t.Run(testName, func(t *testing.T) {
			// Read the expected type-checker error baseline if it exists
			expectedSemaErrBytes, err := os.ReadFile(semaErrPath)
			isSemaErrorTest := err == nil

			// Execute semantic analysis up to the TAST generation layer
			currentBytes, compileErr := d.DumpTAST(srcPath)

			// -----------------------------------------------------------------
			// Case 1: Handle intentional semantic/type error test cases
			// -----------------------------------------------------------------
			if isSemaErrorTest {
				require.Error(t, compileErr, "expected semantic error for %s but it passed type-checking", srcPath)

				expectedErrStr := strings.TrimSpace(string(expectedSemaErrBytes))
				actualErrStr := strings.TrimSpace(compileErr.Error())

				assert.Contains(
					t,
					actualErrStr,
					expectedErrStr,
					"The emitted semantic error message did not contain the expected text baseline",
				)
				return
			}

			// -----------------------------------------------------------------
			// Case 2: Standard valid program TAST snapshot verification
			// -----------------------------------------------------------------
			require.NoError(t, compileErr)

			if *updateTastSnapshots {
				err := os.WriteFile(snapshotPath, currentBytes, 0644)
				require.NoError(t, err, "failed to write TAST snapshot baseline file")
				return
			}

			expectedBytes, err := os.ReadFile(snapshotPath)
			require.NoError(t, err, "missing baseline TAST snapshot file; run with '-update-tast' to create it")

			assert.Equal(
				t,
				strings.TrimSpace(string(expectedBytes)),
				strings.TrimSpace(string(currentBytes)),
				"TAST Snapshot mismatch found for %s", srcPath,
			)
		})
	}
}
