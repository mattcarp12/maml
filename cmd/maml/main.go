package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattcarp12/maml/toolchain"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	d := toolchain.New()

	switch os.Args[1] {
	case "build":
		buildCmd(d, os.Args[2:])

	case "run":
		runCmd(d, os.Args[2:])

	case "check":
		checkCmd(d, os.Args[2:])

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("MAML Compiler")
	fmt.Println("Usage: maml <command> [options] <file>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  build   Compile a .maml file into a native executable")
	fmt.Println("  run     Compile and immediately run a .maml file")
	fmt.Println("  check   Run syntax and semantic checks")
}

func buildCmd(d *toolchain.Driver, args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)

	out := fs.String("out", "maml_app", "Output executable name")
	printIR := fs.Bool("printir", false, "Print LLVM IR")

	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml build [options] <file.maml>")
		os.Exit(1)
	}

	file := fs.Arg(0)

	if err := d.Build(file, *out, *printIR); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	absPath, _ := filepath.Abs(*out)

	fmt.Printf("✅ Build successful: %s\n", absPath)
}

func runCmd(d *toolchain.Driver, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	printIR := fs.Bool("printir", false, "Print LLVM IR")

	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml run [options] <file.maml>")
		os.Exit(1)
	}

	file := fs.Arg(0)

	if err := d.Run(file, *printIR); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
}

func checkCmd(d *toolchain.Driver, args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)

	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Println("Usage: maml check <file.maml>")
		os.Exit(1)
	}

	file := fs.Arg(0)

	if err := d.Check(file); err != nil {
		fmt.Printf("❌ Check failed:\n%s\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Check passed")
}
