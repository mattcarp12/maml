package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mattcarp12/maml/internal/codegen"
	"github.com/mattcarp12/maml/internal/lexer"
	"github.com/mattcarp12/maml/internal/parser"
)

func main() {
	// 1. The Target Program
	filePath := flag.String("file", "", "path to the maml program")
	output := flag.String("out", "maml_app", "filename of output binary file")
	keep := flag.Bool("keep", false, "keep the output binary file")
	printIR := flag.Bool("printir", false, "print the LLVM IR")

	// Parse command-line arguments
	flag.Parse()

	// Read the entire file content
	input, err := os.ReadFile(*filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	// 2. The Frontend
	l := lexer.New(string(input))
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Println("Parser errors:")
		for _, msg := range p.Errors() {
			fmt.Println("\t" + msg)
		}
		return
	}

	// 3. The Backend
	c := codegen.New()
	err = c.Compile(program)
	if err != nil {
		fmt.Println("Compiler Error:", err)
		return
	}

	if *printIR {
		fmt.Printf("%s\n", c.String())
	}

	// 4. The Native Toolchain
	buildExecutable(c.String(), *output)

	// 5. Cleanup
	if !*keep {
		os.Remove(*output)
	}

}

func buildExecutable(llvmIR string, outputName string) {
	fmt.Println("🚧 Building LLVM IR...")

	// Write the IR to a temporary file
	irFile := "output.ll"
	err := os.WriteFile(irFile, []byte(llvmIR), 0644)
	if err != nil {
		fmt.Println("Failed to write IR file:", err)
		return
	}
	defer os.Remove(irFile) // Clean up the .ll file when done

	// Command clang to compile the .ll file into a native executable
	fmt.Println("🔨 Invoking Clang...")
	cmd := exec.Command("clang", "-Wno-override-module", irFile, "-o", outputName)

	// Capture any errors from clang
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Clang failed:\n%s\n", string(output))
		return
	}

	// Get the absolute path to make it clear where it ended up
	absPath, _ := filepath.Abs(outputName)
	fmt.Printf("✅ Success! Binary compiled to: %s\n", absPath)

	// Bonus: Automatically run the program to prove it works!
	runCompiledBinary(outputName)
}

func runCompiledBinary(exeName string) {
	fmt.Println("\n🚀 Executing binary...")
	cmd := exec.Command("./" + exeName)

	err := cmd.Run()

	// Since our MAML program returns an integer exit code (x + y),
	// we want to read that exit code. If it's 42, we win.
	if exitError, ok := err.(*exec.ExitError); ok {
		fmt.Printf("Program exited with status: %d\n", exitError.ExitCode())
	} else if err != nil {
		fmt.Println("Execution error:", err)
	} else {
		fmt.Println("Program exited with status: 0")
	}
}
