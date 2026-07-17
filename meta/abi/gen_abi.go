//go:build ignore

package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/format"
	"log"
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

//go:embed runtime_llvm.tmpl
var llvmTemplateStr string

//go:embed runtime_cpp_header.tmpl
var cppHeaderTemplateStr string

// =============================================================================
// 2. Single Source of Truth for Primitive Types
// =============================================================================
type PrimitiveInfo struct {
	Size     int    // byte size
	Align    int    // byte alignment
	GoType   string // Go AST type constructor, e.g. "types.I32Type{}"
	CppType  string // C-ABI storage type, e.g. "int32_t", "void*"
	LLVMKind string // one of: i1, i8, i16, i32, i64, i128, ptr, void
}

var primitiveTable = map[string]PrimitiveInfo{
	"i8":        {1, 1, "types.I8Type{}", "int8_t", "i8"},
	"i16":       {2, 2, "types.I16Type{}", "int16_t", "i16"},
	"i32":       {4, 4, "types.I32Type{}", "int32_t", "i32"},
	"i64":       {8, 8, "types.I64Type{}", "int64_t", "i64"},
	"i128":      {16, 16, "types.I128Type{}", "__int128_t", "i128"},
	"u8":        {1, 1, "types.U8Type{}", "uint8_t", "i8"},
	"u16":       {2, 2, "types.U16Type{}", "uint16_t", "i16"},
	"u32":       {4, 4, "types.U32Type{}", "uint32_t", "i32"},
	"u64":       {8, 8, "types.U64Type{}", "uint64_t", "i64"},
	"u128":      {16, 16, "types.U128Type{}", "__uint128_t", "i128"},
	"usize":     {8, 8, "types.U64Type{}", "size_t", "i64"},
	"bool":      {1, 1, "types.BoolType{}", "bool", "i1"},
	"ptr":       {8, 8, "types.PtrType{}", "void*", "ptr"},
	"?ptr":      {8, 8, "types.PtrType{}", "void*", "ptr"},
	"bytes_ptr": {8, 8, "types.PtrType{}", "const char*", "ptr"},
	"unit":      {0, 1, "types.UnitType{}", "void", "void"},
	"Future":    {8, 8, "types.PtrType{}", "void*", "ptr"},
}

func mustPrimitive(name string) PrimitiveInfo {
	info, ok := primitiveTable[name]
	if !ok {
		log.Fatalf("gen_abi: unknown primitive type %q -- add it to primitiveTable in gen_abi.go", name)
	}
	return info
}

func llvmTypeExprFor(kind, ctxVar string) string {
	switch kind {
	case "ptr":
		return fmt.Sprintf("llvm::PointerType::getUnqual(%s)", ctxVar)
	case "void":
		return fmt.Sprintf("llvm::Type::getVoidTy(%s)", ctxVar)
	case "i1":
		return fmt.Sprintf("llvm::Type::getInt1Ty(%s)", ctxVar)
	case "i8", "i16", "i32", "i64", "i128":
		return fmt.Sprintf("llvm::Type::getInt%sTy(%s)", strings.TrimPrefix(kind, "i"), ctxVar)
	default:
		log.Fatalf("gen_abi: unhandled LLVMKind %q in llvmTypeExprFor", kind)
		return ""
	}
}

func validatePrimitiveTable(yamlPrimitives []YamlPrimitive) {
	var errs []string
	for _, yp := range yamlPrimitives {
		info, ok := primitiveTable[yp.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("primitive %q is declared in abi_schema.yaml but missing from primitiveTable in gen_abi.go", yp.Name))
			continue
		}
		if info.Size != yp.Size {
			errs = append(errs, fmt.Sprintf("primitive %q: abi_schema.yaml declares size %d but primitiveTable has size %d", yp.Name, yp.Size, info.Size))
		}
	}
	if len(errs) > 0 {
		log.Fatalf("gen_abi: primitive table validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
}

// =============================================================================
// 3. YAML Schema Structures
// =============================================================================

type YamlPrimitive struct {
	Name string `yaml:"name"`
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
	Name       string
	Type       string
	SchemaType string
	Offset     int
	Size       int
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
	Name        string
	RuntimeType string
	CppType     string
	GoType      string
	IsMut       bool
}

type CtxFunction struct {
	Symbol        string
	CamelName     string
	RuntimeModule string
	Args          []CtxArg
	ReturnRuntime string
	ReturnCpp     string
	ReturnGo      string
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
	runtimeOut := flag.String("runtimeOut", "runtime/include/mamlrt_abi.h", "output path for C++ runtime ABI header")
	cppOut := flag.String("cppOut", "backend/include/abi_generated.hpp", "output path for cpp abi helpers")
	flag.Parse()

	var schema YamlSchema
	if err := yaml.Unmarshal(schemaData, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML: %v\n", err)
		os.Exit(1)
	}

	// Fail fast if the hand-maintained primitiveTable has drifted from the
	// schema, before generating a single file.
	validatePrimitiveTable(schema.Primitives)

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
	executeTemplate("C++ Runtime Header", cppHeaderTemplateStr, *runtimeOut, ctx, false)
	executeTemplate("LLVM Backend", llvmTemplateStr, *cppOut, ctx, false)
}

func calculateLayouts(yamlTypes []YamlType) []CtxType {
	var results []CtxType

	for _, yt := range yamlTypes {
		currentOffset := 0
		maxAlign := 1
		var ctxFields []CtxField

		for _, field := range yt.Fields {
			// No silent fallback: a field type that isn't in primitiveTable
			// is a schema bug and must stop the build, not default to a
			// pointer-sized/aligned slot that misrepresents the real layout.
			info := mustPrimitive(field.Type)

			padding := calculatePadding(currentOffset, info.Align)
			currentOffset += padding

			ctxFields = append(ctxFields, CtxField{
				Name:       field.Name,
				Type:       info.CppType,
				SchemaType: field.Type,
				Offset:     currentOffset,
				Size:       info.Size,
			})

			currentOffset += info.Size
			if info.Align > maxAlign {
				maxAlign = info.Align
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

			retRuntime, retCpp, retGo := resolveType(fn.Return.Type, false, true, compoundTypes)

			camelName := toCamelCase(strings.TrimPrefix(fn.Symbol, "maml_"))

			results = append(results, CtxFunction{
				Symbol:        fn.Symbol,
				CamelName:     camelName,
				RuntimeModule: group.Module,
				Args:          args,
				ReturnRuntime: retRuntime,
				ReturnCpp:     retCpp,
				ReturnGo:      retGo,
			})
		}
	}
	return results
}

func buildFunctionArgs(args []YamlArg, compoundTypes map[string]YamlType) []CtxArg {
	var result []CtxArg
	for _, arg := range args {
		runtimeTy, cppTy, goTy := resolveType(arg.Type, arg.ByReference, false, compoundTypes)

		safeName := arg.Name
		if safeName == "type" {
			safeName = "typ"
		}

		result = append(result, CtxArg{
			Name:        safeName,
			RuntimeType: runtimeTy,
			CppType:     cppTy,
			GoType:      goTy,
			IsMut:       arg.Mut,
		})
	}
	return result
}

func resolveType(typ string, isRefOverride bool, isReturn bool, compoundTypes map[string]YamlType) (runtimeTy, cppTy, goTy string) {
	// 1. Is it a Semantic Compound Type? (e.g., Vector, View)
	if ct, ok := compoundTypes[typ]; ok {
		// Only apply 'by_reference' to function arguments, not return types
		isRef := isRefOverride || (!isReturn && ct.PassingConvention == "by_reference")

		if isRef {
			return ct.Name + "*", "llvm::PointerType::getUnqual(ctx.Context)", "types.PtrType{}"
		}
		return ct.Name, "rt::get" + typ + "Type(ctx.Context)", "&types." + typ + "Type{}"
	}

	// 2. It's a primitive -- must be a known one.
	info := mustPrimitive(typ)

	if isRefOverride {
		return info.CppType + "*", "llvm::PointerType::getUnqual(ctx.Context)", "types.PtrType{}"
	}

	return info.CppType, llvmTypeExprFor(info.LLVMKind, "ctx.Context"), info.GoType
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

func llvmFieldTypeExpr(schemaType string) string {
	info := mustPrimitive(schemaType)
	return llvmTypeExprFor(info.LLVMKind, "C")
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
