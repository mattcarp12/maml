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
	case "dump-ast":
		dumpAstCmd(os.Args[2:])
	case "dump-tast":
		dumpTastCmd(os.Args[2:])
	case "dump-mir":
		dumpMirCmd(os.Args[2:])
	case "dump-llvm":
		dumpLlvmCmd(os.Args[2:])
	case "dump-all":
		dumpAllCmd(os.Args[2:])
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
	fmt.Println("  build      Compile a .maml file into a native executable")
	fmt.Println("  run        Compile and immediately run a .maml file")
	fmt.Println("  check      Run syntax and semantic checks")
	fmt.Println("  dump-ast   Parse file and output JSON serialized AST to stdout")
	fmt.Println("  dump-tast  Parse file and output JSON serialized TAST to stdout")
	fmt.Println("  dump-hir   Parse file and output JSON serialized HIR to stdout")
	fmt.Println("  dump-mir   Parse file and output JSON serialized MIR to stdout")
	fmt.Println("  dump-llvm  Parse file, invoke backend, and output LLVM IR to stdout")
	fmt.Println("  dump-all    Dump source and all IR phases into a single file")
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

func dumpAstCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: maml dump-ast <file.maml>")
		os.Exit(1)
	}

	pipeline := driver.New(driver.Config{})
	jsonBytes, err := pipeline.DumpAST(args[0])
	if err != nil {
		fmt.Printf("❌ AST Generation failed: %v\n", err)
		os.Exit(1)
	}

	// Print directly to standard output so users can redirect it to a file
	fmt.Println(string(jsonBytes))
}

func dumpTastCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: maml dump-tast <file.maml>")
		os.Exit(1)
	}

	pipeline := driver.New(driver.Config{})
	jsonBytes, err := pipeline.DumpTAST(args[0])
	if err != nil {
		fmt.Printf("❌ TAST Generation failed: %v\n", err)
		os.Exit(1)
	}

	// Print directly to standard output so users can redirect it to a file
	fmt.Println(string(jsonBytes))
}

func dumpMirCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: maml dump-mir <file.maml>")
		os.Exit(1)
	}
	p := driver.New(driver.Config{})
	out, err := p.DumpMIR(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compilation failed:\n%v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func dumpLlvmCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: maml dump-llvm <file.maml>")
		os.Exit(1)
	}
	p := driver.New(driver.Config{})
	out, err := p.DumpLLVM(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "LLVM IR Generation failed:\n%v\n", err)
		os.Exit(1)
	}
	// Print directly to standard output so users can inspect it or pipe it to a file
	fmt.Println(string(out))
}

func dumpAllCmd(args []string) {
	fs := flag.NewFlagSet("dump-all", flag.ExitOnError)
	out := fs.String("out", "maml_dump.txt", "Output file path")
	fs.Parse(args)

	positionalArgs := fs.Args()
	if len(positionalArgs) < 1 {
		fmt.Println("Usage: maml dump-all [options] <file.maml>")
		fs.PrintDefaults()
		os.Exit(1)
	}

	srcPath := positionalArgs[0]
	p := driver.New(driver.Config{})

	fmt.Printf("Dumping all compiler phases for '%s' to '%s'...\n", srcPath, *out)

	err := p.DumpAll(srcPath, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dump failed:\n%v\n", err)
		os.Exit(1)
	}

	fmt.Println("Success.")
}
