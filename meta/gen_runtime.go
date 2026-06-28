//go:build ignore

package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed runtime_schema.yaml
var schemaData []byte

//go:embed runtime_go.tmpl
var goTemplateStr string

//go:embed runtime_cpp.tmpl
var cppTemplateStr string

//go:embed runtime_zig.tmpl
var zigTemplateStr string

//go:embed mir_runtime_go.tmpl
var mirTemplateStr string

type TypeAlias struct {
	Alias   string `yaml:"alias"`
	GoType  string `yaml:"go_type"`
	CppType string `yaml:"cpp_type"`
	ZigType string `yaml:"zig_type"`
}

type ArgDef struct {
	Type string `yaml:"type"`
	Mut  bool   `yaml:"mut"`
}

type FunctionDef struct {
	Symbol string   `yaml:"symbol"`
	Args   []ArgDef `yaml:"args"`
	Return string   `yaml:"return"`
}

type ModuleGroup struct {
	Module      string        `yaml:"module"`
	Definitions []FunctionDef `yaml:"definitions"`
}

type Schema struct {
	Types     []TypeAlias   `yaml:"types"`
	Functions []ModuleGroup `yaml:"functions"`
}

type TemplateContext struct {
	Functions []FunctionContext
}

type ArgContext struct {
	Alias   string
	GoType  string
	CppType string
	ZigType string
	IsMut   bool
}

type FunctionContext struct {
	Symbol    string
	ZigModule string
	ConstName string
	CamelName string
	ReturnGo  string
	ReturnCpp string
	ReturnZig string
	ArgsGo    []string
	ArgsCpp   []string
	ArgsZig   []string
	Args      []ArgContext
}

// toCamelCase converts snake_case to CamelCase (e.g. maml_vec_push -> MamlVecPush)
func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}

func main() {
	goOut := flag.String("goOut", "frontend/types/", "output directory for runtime_generated.go")
	cppOut := flag.String("cppOut", "backend/include/", "output directory for autogen_runtime.hpp")
	zigOut := flag.String("zigOut", "runtime/src/", "output directory for abi_assertions.zig")
	mirOut := flag.String("mirOut", "frontend/mir/", "output directory for MIR runtime calls")
	flag.Parse()

	var schema Schema
	if err := yaml.Unmarshal(schemaData, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML: %v\n", err)
		os.Exit(1)
	}

	typeMap := make(map[string]TypeAlias)
	for _, t := range schema.Types {
		typeMap[t.Alias] = t
	}

	ctx := buildContext(schema, typeMap)

	// 1. Generate Go
	executeTemplate("Go Frontend", goTemplateStr, path.Join(*goOut, "runtime_generated.go"), ctx, true)

	// 2. Generate C++
	executeTemplate("C++ Backend (.inc)", cppTemplateStr, path.Join(*cppOut, "autogen_runtime.hpp"), ctx, false)
	generateCppConstants(ctx, path.Join(*cppOut, "RuntimeConstants.h"))

	// 3. Generate Zig
	executeTemplate("Zig Assertions", zigTemplateStr, path.Join(*zigOut, "abi_assertions.zig"), ctx, false)

	// 4. Generate MIR Builder Calls
	executeTemplate("MIR Builders", mirTemplateStr, path.Join(*mirOut, "runtime_calls_generated.go"), ctx, true)

	fmt.Println("✅ Successfully generated unified Runtime ABI via templates.")
}

func buildContext(schema Schema, tm map[string]TypeAlias) TemplateContext {
	var ctx TemplateContext

	// Loop through each module group block
	for _, group := range schema.Functions {
		// Loop through individual function definitions inside the module group
		for _, fn := range group.Definitions {
			fCtx := FunctionContext{
				Symbol:    fn.Symbol,
				ZigModule: group.Module, // 👈 Capture the group's module name ("alloc", "vec", "", etc.)
				ConstName: "SYM_" + strings.ToUpper(strings.ReplaceAll(fn.Symbol, "maml_", "")),
				CamelName: toCamelCase(fn.Symbol),
				ReturnGo:  tm[fn.Return].GoType,
				ReturnCpp: tm[fn.Return].CppType,
				ReturnZig: tm[fn.Return].ZigType,
			}
			for _, arg := range fn.Args {
				fCtx.ArgsGo = append(fCtx.ArgsGo, tm[arg.Type].GoType)
				fCtx.ArgsCpp = append(fCtx.ArgsCpp, tm[arg.Type].CppType)
				fCtx.ArgsZig = append(fCtx.ArgsZig, tm[arg.Type].ZigType)
				fCtx.Args = append(fCtx.Args, ArgContext{
					Alias:   arg.Type,
					GoType:  tm[arg.Type].GoType,
					CppType: tm[arg.Type].CppType,
					ZigType: tm[arg.Type].ZigType,
					IsMut:   arg.Mut,
				})
			}
			ctx.Functions = append(ctx.Functions, fCtx)
		}
	}
	return ctx
}

func executeTemplate(name, tmplStr, outPath string, data TemplateContext, formatGo bool) {
	tmpl := template.Must(template.New(name).Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(tmplStr))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Fprintf(os.Stderr, "Template Execution Error (%s): %v\n", name, err)
		os.Exit(1)
	}

	outData := buf.Bytes()
	if formatGo {
		formatted, err := format.Source(outData)
		if err != nil {
			os.WriteFile(outPath+".debug", outData, 0644)
			fmt.Fprintf(os.Stderr, "Go Format Error in %s: %v\n", name, err)
			os.Exit(1)
		}
		outData = formatted
	}

	os.MkdirAll(filepath.Dir(outPath), 0755)
	if err := os.WriteFile(outPath, outData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Write Error (%s): %v\n", name, err)
		os.Exit(1)
	}
}

func generateCppConstants(ctx TemplateContext, outPath string) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by tools/gen_runtime.go. DO NOT EDIT.\n")
	buf.WriteString("#pragma once\n\nnamespace maml {\nnamespace rt {\n\n")
	for _, fn := range ctx.Functions {
		buf.WriteString(fmt.Sprintf("constexpr const char* %s = \"%s\";\n", strings.ToUpper(strings.ReplaceAll(fn.Symbol, "maml_", "")), fn.Symbol))
	}
	buf.WriteString("\n} // namespace rt\n} // namespace maml\n")
	os.WriteFile(outPath, buf.Bytes(), 0644)
}
