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
	filePath := flag.String("file", "", "Path to the .maml source file (required)")
	outputName := flag.String("out", "maml_app", "Name of the output executable")
	keepBinary := flag.Bool("keep", false, "Keep the generated binary after execution")
	printIR := flag.Bool("printir", false, "Print the generated LLVM IR")
	flag.Parse()

	if *filePath == "" {
		fmt.Println("Error: --file flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Create compiler and run full pipeline
	c := compiler.New()

	llvmIR, err := c.CompileFile(*filePath)
	if err != nil {
		fmt.Printf("Compilation failed:\n%s\n", err)
		os.Exit(1)
	}

	if *printIR {
		fmt.Println("=== Generated LLVM IR ===")
		fmt.Println(llvmIR)
	}

	// Build and run the native executable
	buildAndRun(llvmIR, *outputName, *keepBinary)
}

func buildAndRun(llvmIR string, outputName string, keepBinary bool) {
	fmt.Println("🚧 Building LLVM IR...")

	irFile := "output.ll"
	if err := os.WriteFile(irFile, []byte(llvmIR), 0644); err != nil {
		fmt.Printf("Failed to write IR: %v\n", err)
		return
	}
	defer os.Remove(irFile)

	fmt.Println("🔨 Invoking Clang...")
	cmd := exec.Command("clang", "-Wno-override-module", irFile, "-o", outputName)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Clang build failed:\n%s\n", string(output))
		return
	}

	absPath, _ := filepath.Abs(outputName)
	fmt.Printf("✅ Success! Binary compiled to: %s\n", absPath)

	runBinary(outputName, keepBinary)
}

func runBinary(exeName string, keepBinary bool) {
	fmt.Println("\n🚀 Executing binary...")

	cmd := exec.Command("./" + exeName)
	output, err := cmd.CombinedOutput()

	if exitErr, ok := err.(*exec.ExitError); ok {
		fmt.Printf("Program exited with status: %d\n", exitErr.ExitCode())
	} else if err != nil {
		fmt.Printf("Execution error: %v\n", err)
	} else {
		fmt.Println("Program exited with status: 0")
	}

	if len(output) > 0 {
		fmt.Printf("Output:\n%s", string(output))
	}

	if !keepBinary {
		os.Remove(exeName)
	}
}
