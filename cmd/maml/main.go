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

const runtimeLibPath = "./runtime/zig-out/lib/libmamlrt.a"

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
	if err := compiler.InvokeClang(llvmIR, *out, runtimeLibPath); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

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

	outName := filepath.Join(os.TempDir(), "maml_run_tmp")
	if err := compiler.InvokeClang(llvmIR, outName, runtimeLibPath); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(outName)

	executeBinary(outName)
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
	_, err := c.CompileFile(file)
	if err != nil {
		fmt.Printf("❌ Check failed:\n%s\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Check passed: No syntax, semantic, or ownership errors.")
}

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
