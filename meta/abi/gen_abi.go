//go:build ignore

package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// 1. Embedded Files
// =============================================================================

//go:embed abi_schema.yaml
var schemaData []byte

//go:embed runtime_go.tmpl
var goTemplateStr string

//go:embed runtime_cpp.tmpl
var cppTemplateStr string

//go:embed runtime_zig.tmpl
var zigTemplateStr string

// =============================================================================
// 2. Language-Specific Type Mappings
// =============================================================================

var goPrimitiveMap = map[string]string{
	"i8":        "types.I8Type{}",
	"i16":       "types.I16Type{}",
	"i32":       "types.I32Type{}",
	"i64":       "types.I64Type{}",
	"i128":      "types.I128Type{}",
	"u8":        "types.U8Type{}",
	"u16":       "types.U16Type{}",
	"u32":       "types.U32Type{}",
	"u64":       "types.U64Type{}",
	"u128":      "types.U128Type{}",
	"usize":     "types.U64Type{}",
	"bool":      "types.BoolType{}",
	"ptr":       "types.PtrType{}",
	"?ptr":      "types.PtrType{}",
	"bytes_ptr": "types.PtrType{}",
	"unit":      "types.UnitType{}",
}

var zigPrimitiveMap = map[string]string{
	"i8":        "i8",
	"i16":       "i16",
	"i32":       "i32",
	"i64":       "i64",
	"i128":      "i128",
	"u8":        "u8",
	"u16":       "u16",
	"u32":       "u32",
	"u64":       "u64",
	"u128":      "u128",
	"usize":     "usize",
	"bool":      "bool",
	"ptr":       "*anyopaque",
	"?ptr":      "?*anyopaque",
	"unit":      "void",
	"bytes_ptr": "[*]const u8",
}

var llvmPrimitiveMap = map[string]string{
	"i8":        "i8",
	"i16":       "i16",
	"i32":       "i32",
	"i64":       "i64",
	"i128":      "i128",
	"u8":        "i8",
	"u16":       "i16",
	"u32":       "i32",
	"u64":       "i64",
	"u128":      "i128",
	"usize":     "i64",
	"bool":      "i1",
	"ptr":       "ptr",
	"?ptr":      "ptr",
	"unit":      "void",
	"bytes_ptr": "ptr",
}

var primLayouts = map[string]struct{ Size, Align int }{
	"i8": {1, 1}, "u8": {1, 1}, "bool": {1, 1},
	"i16": {2, 2}, "u16": {2, 2},
	"i32": {4, 4}, "u32": {4, 4},
	"i64": {8, 8}, "u64": {8, 8}, "usize": {8, 8},
	"ptr": {8, 8}, "unit": {0, 1},
}

// =============================================================================
// 3. YAML Schema Structures
// =============================================================================

type YamlPrimitive struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
	Size int    `yaml:"size"`
}

type YamlField struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type YamlType struct {
	Name              string      `yaml:"name"`
	PassingConvention string      `yaml:"passing_convention"`
	Allocation        string      `yaml:"allocation"`
	IsBorrow          bool        `yaml:"is_borrow"`
	IsCopyable        bool        `yaml:"is_copyable"`
	Fields            []YamlField `yaml:"fields"`
}

type YamlArg struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	ByReference bool   `yaml:"by_reference,omitempty"`
	Mut         bool   `yaml:"mut,omitempty"`
}

type YamlFunctionDef struct {
	Symbol string    `yaml:"symbol"`
	Args   []YamlArg `yaml:"args"`
	Return YamlArg   `yaml:"return"`
}

type YamlModuleGroup struct {
	Module      string            `yaml:"module"`
	Definitions []YamlFunctionDef `yaml:"definitions"`
}

type YamlSchema struct {
	Primitives []YamlPrimitive   `yaml:"primitives"`
	Types      []YamlType        `yaml:"types"`
	Modules    []YamlModuleGroup `yaml:"modules"`
}

// =============================================================================
// 4. Calculated Template Context
// =============================================================================

type CtxField struct {
	Name   string
	Type   string
	Offset int
	Size   int
}

type CtxType struct {
	Name              string
	Size              int
	Align             int
	Fields            []CtxField
	PassingConvention string
	Allocation        string
}

type CtxArg struct {
	Name    string
	ZigType string
	CppType string
	GoType  string
	IsMut   bool
}

type CtxFunction struct {
	Symbol    string
	CamelName string
	ZigModule string
	Args      []CtxArg
	ReturnZig string
	ReturnCpp string
	ReturnGo  string
}

type TemplateContext struct {
	Types     []CtxType
	Functions []CtxFunction
}

// =============================================================================
// 5. Main Generator Logic
// =============================================================================

func main() {
	goOut := flag.String("goOut", "frontend/mir/abi_generated.go", "output path for Go types")
	zigOut := flag.String("zigOut", "runtime/src/abi.zig", "output path for Zig assertions")
	cppOut := flag.String("cppOut", "backend/include/abi_generated.hpp", "output path for cpp abi helpers")
	flag.Parse()

	var schema YamlSchema
	if err := yaml.Unmarshal(schemaData, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML: %v\n", err)
		os.Exit(1)
	}

	// Create a lookup map for compound types to resolve memory classes easily
	compoundTypes := make(map[string]YamlType)
	for _, t := range schema.Types {
		compoundTypes[t.Name] = t
	}

	typesCtx := calculateLayouts(schema.Types)
	funcsCtx := buildFunctions(schema.Modules, compoundTypes)

	ctx := TemplateContext{
		Types:     typesCtx,
		Functions: funcsCtx,
	}

	executeTemplate("Go Frontend", goTemplateStr, *goOut, ctx, true)
	executeTemplate("Zig Runtime", zigTemplateStr, *zigOut, ctx, false)
	executeTemplate("C++ Backend", cppTemplateStr, *cppOut, ctx, false)
}

func calculateLayouts(yamlTypes []YamlType) []CtxType {
	var results []CtxType

	for _, yt := range yamlTypes {
		currentOffset := 0
		maxAlign := 1
		var ctxFields []CtxField

		for _, field := range yt.Fields {
			layout, ok := primLayouts[field.Type]
			if !ok {
				layout = primLayouts["ptr"] // Fallback for safety
			}

			padding := calculatePadding(currentOffset, layout.Align)
			currentOffset += padding

			ctxFields = append(ctxFields, CtxField{
				Name:   field.Name,
				Type:   field.Type,
				Offset: currentOffset,
				Size:   layout.Size,
			})

			currentOffset += layout.Size
			if layout.Align > maxAlign {
				maxAlign = layout.Align
			}
		}

		trailingPadding := calculatePadding(currentOffset, maxAlign)

		results = append(results, CtxType{
			Name:              yt.Name,
			Size:              currentOffset + trailingPadding,
			Align:             maxAlign,
			Fields:            ctxFields,
			PassingConvention: yt.PassingConvention,
			Allocation:        yt.Allocation,
		})
	}
	return results
}

func calculatePadding(currentOffset, align int) int {
	if currentOffset%align != 0 {
		return align - (currentOffset % align)
	}
	return 0
}

func buildFunctions(modules []YamlModuleGroup, compoundTypes map[string]YamlType) []CtxFunction {
	var results []CtxFunction
	for _, group := range modules {
		for _, fn := range group.Definitions {
			args := buildFunctionArgs(fn.Args, compoundTypes)

			// Resolve the strong return type using our semantic type solver
			retZig, retCpp, retGo := resolveType(fn.Return.Type, false, compoundTypes)

			camelName := toCamelCase(strings.TrimPrefix(fn.Symbol, "maml_"))

			results = append(results, CtxFunction{
				Symbol:    fn.Symbol,
				CamelName: camelName,
				ZigModule: group.Module,
				Args:      args,
				ReturnZig: retZig,
				ReturnCpp: retCpp,
				ReturnGo:  retGo,
			})
		}
	}
	return results
}

func buildFunctionArgs(args []YamlArg, compoundTypes map[string]YamlType) []CtxArg {
	var result []CtxArg
	for _, arg := range args {
		zigTy, cppTy, goTy := resolveType(arg.Type, arg.ByReference, compoundTypes)

		// Ensure parameter names are safe for Go identifiers (e.g. avoid 'type')
		safeName := arg.Name
		if safeName == "type" {
			safeName = "typ"
		}

		result = append(result, CtxArg{
			Name:    safeName,
			ZigType: zigTy,
			CppType: cppTy,
			GoType:  goTy,
			IsMut:   arg.Mut,
		})
	}
	return result
}

// resolveType is the core engine for Phase 2: It guarantees the exact right pointer semantics
// across Go, C++, and Zig based purely on the yaml `passing_convention`
func resolveType(typ string, isRefOverride bool, compoundTypes map[string]YamlType) (zigTy, cppTy, goTy string) {
	// 1. Is it a Semantic Compound Type? (e.g., Vector, View)
	if ct, ok := compoundTypes[typ]; ok {
		isRef := isRefOverride || ct.PassingConvention == "by_reference"
		if isRef {
			return "*" + typ, "llvm::PointerType::getUnqual(ctx.Context)", "types.PtrType{}"
		} else {
			return typ, "rt::get" + typ + "Type(ctx.Context)", "types." + typ + "Type{}"
		}
	}

	// 2. It's a primitive
	zigTy = zigPrimitiveMap[typ]
	if zigTy == "" {
		zigTy = "anyopaque"
	}

	if isRefOverride {
		return "*" + zigTy, "llvm::PointerType::getUnqual(ctx.Context)", "types.PtrType{}"
	}

	cppTy = toCppType(typ)
	goTy = goPrimitiveMap[typ]
	if goTy == "" {
		goTy = "types.UnknownType{}"
	}

	return zigTy, cppTy, goTy
}

func toCppType(typ string) string {
	llvmTy, ok := llvmPrimitiveMap[typ]
	if !ok || llvmTy == "ptr" {
		return "llvm::PointerType::getUnqual(ctx.Context)"
	}
	if llvmTy == "void" {
		return "llvm::Type::getVoidTy(ctx.Context)"
	}
	if llvmTy == "i1" {
		return "llvm::Type::getInt1Ty(ctx.Context)"
	}
	if strings.HasPrefix(llvmTy, "i") {
		return fmt.Sprintf("llvm::Type::getInt%sTy(ctx.Context)", strings.TrimPrefix(llvmTy, "i"))
	}
	return "llvm::PointerType::getUnqual(ctx.Context)"
}

func toCamelCase(s string) string {
	words := strings.Split(s, "_")
	for i := range words {
		if len(words[i]) > 0 {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return strings.Join(words, "")
}

func llvmFieldTypeExpr(typ string) string {
	switch typ {
	case "ptr", "?ptr", "bytes_ptr":
		return "llvm::PointerType::getUnqual(C)"
	case "unit":
		return "llvm::Type::getVoidTy(C)"
	case "bool":
		return "llvm::Type::getInt1Ty(C)"
	case "u8", "i8":
		return "llvm::Type::getInt8Ty(C)"
	case "u16", "i16":
		return "llvm::Type::getInt16Ty(C)"
	case "u32", "i32":
		return "llvm::Type::getInt32Ty(C)"
	case "u64", "i64", "usize":
		return "llvm::Type::getInt64Ty(C)"
	case "u128", "i128":
		return "llvm::Type::getInt128Ty(C)"
	default:
		return "llvm::PointerType::getUnqual(C)"
	}
}

func executeTemplate(name, tmplStr, outPath string, data TemplateContext, formatGo bool) {
	funcMap := template.FuncMap{
		"toUpper":           strings.ToUpper,
		"trimPrefix":        strings.TrimPrefix,
		"llvmFieldTypeExpr": llvmFieldTypeExpr,
	}

	tmpl := template.Must(template.New(name).Funcs(funcMap).Parse(tmplStr))

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
			fmt.Fprintf(os.Stderr, "Go Format Error in %s (saved to .debug): %v\n", name, err)
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
