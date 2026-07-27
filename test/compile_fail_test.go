package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertCompileFails is our shared test rig.
// It writes the code, runs the compiler, and asserts the expected errors.
func assertCompileFails(t *testing.T, src string, expectedErrs ...string) {
	t.Helper() // CRITICAL: Tells Go to report failures at the caller's line number!

	// Resolve the maml binary path
	mamlRoot := os.Getenv("MAML_ROOT")
	if mamlRoot == "" {
		mamlRoot = "../.." // Fallback if run directly via go test outside of make
	}
	mamlBin := filepath.Join(mamlRoot, "bin", "maml")

	// Ensure the binary exists before testing
	_, err := os.Stat(mamlBin)
	require.NoError(t, err, "maml binary not found at %s. Run 'make' first.", mamlBin)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "compile_fail.maml")

	err = os.WriteFile(srcPath, []byte(src), 0644)
	require.NoError(t, err, "Failed to write temp source file")

	// Execute the CLI binary to attempt a build
	cmd := exec.Command(mamlBin, "run", srcPath)
	outputBytes, compileErr := cmd.CombinedOutput()

	// We REQUIRE an error.
	require.Error(t, compileErr, "Expected compilation to fail, but it succeeded!")

	// Verify all expected error substrings are present in the output
	errMsg := string(outputBytes)
	for _, expectedStr := range expectedErrs {
		assert.Contains(t, errMsg, expectedStr, "Compiler error missing expected text")
	}
}

// ============================================================================
// Negative Tests
// ============================================================================

func TestCompileFail_ViewLocalArrayEscape(t *testing.T) {
	src := `
fn getSlice() View<int> {
    a := [3]int{1,2,3}
    return a[:]
}
fn main() int {
    a := getSlice()
    return 0
}
`
	assertCompileFails(t, src,
		"[Sema Error] at compile_fail.maml:4:5",
		"cannot return a View from a function",
	)
}

func TestCompileFail_ViewNestedStructEscape(t *testing.T) {
	src := `
type Box = { id int, data View<int> }
fn make_box(id int) Box {
    arr := [3]int{10, 20, 30}
    return Box{id: id, data: arr[:]}
}
fn main() int {
    a := make_box(1)
    return 0
}
`
	assertCompileFails(t, src,
		"[Sema Error] at compile_fail.maml:5:5",
		"cannot return a View from a function",
	)
}

func TestCompileFail_UseAfterMove(t *testing.T) {
	src := `
type Token = { id int }
fn consume_token(own t Token) int { return t.id }
fn main() int {
    mut my_token := Token{ id: 99 }
    val := consume_token(own my_token)
    return my_token.id 
}
`
	assertCompileFails(t, src,
		"use of moved variable",
		"'my_token'",
	)
}

// func TestCompileFail_ImmutableMutation(t *testing.T) {
// 	src := `
// fn main() int {
//     mut data := Vec<int>{}
//     data << 10
//     frozen_data := freeze(data)
//     frozen_data << 20 
//     return len(frozen_data)
// }
// `
// 	assertCompileFails(t, src,
// 		"cannot mutate",
// 		"frozen_data",
// 	)
// }

// func TestCompileFail_AliasingViolation(t *testing.T) {
// 	src := `
// fn main() int {
//     mut v1 := Vec<int>{1, 2, 3}
//     mut v2 := v1 
//     for mut i := 0; i < len(v1); i = i + 1 {
//         v2 << 99 
//     }
//     return 0
// }
// `
// 	assertCompileFails(t, src,
// 		"mutation of aliased data",
// 		"v2",
// 	)
// }
