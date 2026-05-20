package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattcarp12/maml/driver"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		buildCmd(os.Args[2:])
	case "run":
		runCmd(os.Args[2:])
	case "check":
		checkCmd(os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
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
	fmt.Println("  check   Run syntax and semantic checks")
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

	pipeline := driver.New(driver.Config{
		OutputPath: *out,
		PrintIR:    *printIR,
	})

	if err := pipeline.Build(file); err != nil {
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

	pipeline := driver.New(driver.Config{PrintIR: *printIR})
	if err := pipeline.Run(fs.Arg(0)); err != nil {
		fmt.Printf("❌ Run failed: %v\n", err)
		os.Exit(1)
	}
}

func checkCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: maml check <file.maml>")
		os.Exit(1)
	}

	pipeline := driver.New(driver.Config{})
	if err := pipeline.Check(args[0]); err != nil {
		fmt.Printf("❌ Check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ All checks passed.")
}
