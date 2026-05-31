package integration

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattcarp12/maml/driver"
	"github.com/stretchr/testify/require"
)

var updateMirSnapshots = flag.Bool("update-mir", false, "update .mir.json snapshot files")

func TestMIRSnapshots(t *testing.T) {
	matches, err := filepath.Glob("../programs/*/*.maml")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	p := driver.New(driver.Config{})

	for _, match := range matches {
		name := filepath.Base(match)
		t.Run(name, func(t *testing.T) {
			out, err := p.DumpMIR(match)
			require.NoError(t, err, "Pipeline failed to dump MIR")

			snapshotFile := strings.Replace(match, ".maml", ".mir.json", 1)

			if *updateMirSnapshots {
				err = os.WriteFile(snapshotFile, out, 0644)
				require.NoError(t, err, "Failed to write MIR snapshot")
				return
			}

			expected, err := os.ReadFile(snapshotFile)
			if os.IsNotExist(err) {
				t.Fatalf("Snapshot file %s does not exist. Run with -update-mir to generate it.", snapshotFile)
			}
			require.NoError(t, err)

			require.Equal(t, string(expected), string(out), "MIR mismatch for %s", name)
		})
	}
}