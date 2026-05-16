// cmd/maml/main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mattcarp12/maml/internal/compiler"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "build":
		buildCmd(os.Args[2:])
	case "run":
		runCmd(os.Args[2:])
	case "check":
		checkCmd(os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("MAML Compiler")
	fmt.Println("Usage: maml <command> [options] <file>")
	fmt.Println("\nCommands:")
	fmt.Println("  build   Compile a .maml file into a native executable")
	fmt.Println("  run     Compile and immediately run a .maml file")
	fmt.Println("  check   Run syntax and semantic checks without compiling")
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "maml_app", "Output executable name")
	printIR := fs.Bool("printir", false, "Print LLVM IR")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml build [options] <file.maml>")
		os.Exit(1)
	}
	file := fs.Arg(0)

	llvmIR := runCompilerCore(file, *printIR)
	invokeClang(llvmIR, *out)
	
	absPath, _ := filepath.Abs(*out)
	fmt.Printf("✅ Build successful: %s\n", absPath)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	printIR := fs.Bool("printir", false, "Print LLVM IR")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml run [options] <file.maml>")
		os.Exit(1)
	}
	file := fs.Arg(0)

	llvmIR := runCompilerCore(file, *printIR)
	
	// Create a temporary hidden executable for 'run'
	outName := filepath.Join(os.TempDir(), "maml_run_tmp")
	invokeClang(llvmIR, outName)
	
	executeBinary(outName)
	os.Remove(outName) // Clean up the temp binary
}

func checkCmd(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml check <file.maml>")
		os.Exit(1)
	}
	file := fs.Arg(0)

	c := compiler.New()
	// Check runs the full pipeline but we just ignore the IR output
	// If it fails lexing, parsing, or semantic analysis, err will be populated.
	_, err := c.CompileFile(file)
	if err != nil {
		fmt.Printf("❌ Check failed:\n%s\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Check passed: No syntax or semantic errors.")
}

// runCompilerCore handles the MAML pipeline up through LLVM IR generation
func runCompilerCore(file string, printIR bool) string {
	c := compiler.New()
	llvmIR, err := c.CompileFile(file)
	if err != nil {
		fmt.Printf("❌ Compilation failed:\n%s\n", err)
		os.Exit(1)
	}

	if printIR {
		fmt.Println("=== Generated LLVM IR ===")
		fmt.Println(llvmIR)
	}
	return llvmIR
}

// invokeClang handles shelling out to Clang to compile the IR to machine code
func invokeClang(llvmIR, outName string) {
	irFile := outName + ".ll"
	if err := os.WriteFile(irFile, []byte(llvmIR), 0644); err != nil {
		fmt.Printf("Failed to write IR: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(irFile) // Clean up the .ll file

	runtimeLibPath := "./runtime/zig-out/lib/libmamlrt.a"
	cmd := exec.Command("clang", 
		"-Wno-override-module", 
		irFile, // our generated .ll file
		runtimeLibPath, // Link maml runtime
		"-Wl,-z,noexecstack",   // Silences the GNU-stack linker warning
		"-o", outName,
		"-lm",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Clang build failed:\n%s\n", string(output))
		os.Exit(1)
	}
}

// executeBinary runs the compiled executable and pipes its stdout/stderr
func executeBinary(exeName string) {
	cmd := exec.Command(exeName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		fmt.Printf("\n[Process exited with status %d]\n", exitErr.ExitCode())
	} else if err != nil {
		fmt.Printf("\n[Execution error: %v]\n", err)
	}
}