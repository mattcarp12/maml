package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	buildOut      string
	buildPrintIR  bool
	buildSanitize bool

	runPrintIR  bool
	runSanitize bool

	dumpEmit string
	dumpOut  string
)

var rootCmd = &cobra.Command{
	Use:   "maml",
	Short: "MAML Compiler",
	Long:  "The MAML compiler toolchain.",
}

func init() {
	// build
	buildCmd := &cobra.Command{
		Use:   "build <file.maml>",
		Short: "Compile a .maml file into a native executable",
		Args:  cobra.ExactArgs(1),
		RunE:  doBuild,
	}
	buildCmd.Flags().StringVarP(&buildOut, "out", "o", "maml_app", "Output executable name")
	buildCmd.Flags().BoolVar(&buildPrintIR, "printir", false, "Print LLVM IR")
	buildCmd.Flags().BoolVar(&buildSanitize, "sanitize", false, "Enable AddressSanitizer")
	rootCmd.AddCommand(buildCmd)

	// run
	runCmd := &cobra.Command{
		Use:   "run <file.maml>",
		Short: "Compile and immediately run a .maml file",
		Args:  cobra.ExactArgs(1),
		RunE:  doRun,
	}
	runCmd.Flags().BoolVar(&runPrintIR, "printir", false, "Print LLVM IR")
	runCmd.Flags().BoolVar(&runSanitize, "sanitize", false, "Enable AddressSanitizer")
	rootCmd.AddCommand(runCmd)

	// check
	rootCmd.AddCommand(&cobra.Command{
		Use:   "check <file.maml>",
		Short: "Run syntax and semantic checks",
		Args:  cobra.ExactArgs(1),
		RunE:  doCheck,
	})

	// dump (replaces dump-ast, dump-tast, dump-hir, dump-mir, dump-llvm)
	dumpCmd := &cobra.Command{
		Use:   "dump <file.maml>",
		Short: "Dump compiler IR phases",
		Long:  "Emit one or more compiler phases. Use --emit to select phases (ast,tast,hir,mir,llvm,all).",
		Args:  cobra.ExactArgs(1),
		RunE:  doDump,
	}
	dumpCmd.Flags().StringVar(&dumpEmit, "emit", "all", "Comma-separated phases to emit")
	dumpCmd.Flags().StringVarP(&dumpOut, "out", "o", "", "Write output to file instead of stdout")
	rootCmd.AddCommand(dumpCmd)

	// dump-all (thin wrapper kept for convenience)
	dumpAllCmd := &cobra.Command{
		Use:   "dump-all <file.maml>",
		Short: "Dump source and all IR phases into a single file",
		Args:  cobra.ExactArgs(1),
		RunE:  doDumpAll,
	}
	dumpAllCmd.Flags().StringVarP(&dumpOut, "out", "o", "maml_dump.txt", "Output file path")
	rootCmd.AddCommand(dumpAllCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func doBuild(cmd *cobra.Command, args []string) error {
	p := NewPipeline(Config{
		OutputPath: buildOut,
		PrintIR:    buildPrintIR,
		Sanitize:   buildSanitize,
	})
	if err := p.Build(args[0]); err != nil {
		return err
	}
	absPath, _ := filepath.Abs(buildOut)
	fmt.Printf("✅ Build successful: %s\n", absPath)
	return nil
}

func doRun(cmd *cobra.Command, args []string) error {
	p := NewPipeline(Config{
		PrintIR:  runPrintIR,
		Sanitize: runSanitize,
	})
	return p.Run(args[0])
}

func doCheck(cmd *cobra.Command, args []string) error {
	if err := NewPipeline(Config{}).Check(args[0]); err != nil {
		return err
	}
	fmt.Println("✅ All checks passed.")
	return nil
}

func doDump(cmd *cobra.Command, args []string) error {
	srcPath := args[0]

	var requested []string
	if dumpEmit == "all" {
		requested = []string{"ast", "tast", "hir", "mir", "llvm"}
	} else {
		for _, p := range strings.Split(dumpEmit, ",") {
			if s := strings.TrimSpace(strings.ToLower(p)); s != "" {
				requested = append(requested, s)
			}
		}
		if len(requested) == 0 {
			return fmt.Errorf("no phases specified")
		}
	}

	out, err := NewPipeline(Config{}).DumpPhases(srcPath, requested)
	if err != nil {
		return err
	}

	if dumpOut != "" {
		if err := os.WriteFile(dumpOut, out, 0644); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
		fmt.Printf("Dump written to %s\n", dumpOut)
		return nil
	}

	fmt.Print(string(out))
	return nil
}

func doDumpAll(cmd *cobra.Command, args []string) error {
	srcPath := args[0]
	fmt.Printf("Dumping all compiler phases for '%s' to '%s'...\n", srcPath, dumpOut)
	if err := NewPipeline(Config{}).DumpAll(srcPath, dumpOut); err != nil {
		return err
	}
	fmt.Println("Success.")
	return nil
}
