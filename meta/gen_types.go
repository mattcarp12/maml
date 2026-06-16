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
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed types_schema.yaml
var schemaData []byte

//go:embed types_go.tmpl
var goTemplate string

//go:embed types_cpp.tmpl
var cppTemplate string

type Property struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type Primitive struct {
	Name        string `yaml:"name"`
	CppKind     string `yaml:"cpp_kind"`
	IsReference bool   `yaml:"is_reference"`
	IsNeedsArc  bool   `yaml:"is_needs_arc"`
}

type Complex struct {
	Name       string     `yaml:"name"`
	JsonKind   string     `yaml:"json_kind"`
	IsNeedsArc bool       `yaml:"is_needs_arc"`
	Properties []Property `yaml:"properties"`
}

type Schema struct {
	Primitives []Primitive `yaml:"primitives"`
	Composites []Complex   `yaml:"composites"`
	Containers []Complex   `yaml:"containers"`
	Helpers    []Complex   `yaml:"helpers"`
}

func main() {
	var schema Schema
	if err := yaml.Unmarshal(schemaData, &schema); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML: %v\n", err)
		os.Exit(1)
	}

	funcs := template.FuncMap{
		"toSnake": toSnakeCase,
		"cppType": mapCppType,
	}

	goOut := flag.String("goOut", "frontend/types/", "output directory for types_generated.go")
	cppOut := flag.String("cppOut", "backend/include/", "output directory for types_generated.hpp")
	flag.Parse()

	// Generate Go
	goOutFile := path.Join(*goOut, "types_generated.go")
	if err := generate(goTemplate, goOutFile, schema, funcs, true); err != nil {
		fmt.Fprintf(os.Stderr, "Go Gen Error: %v\n", err)
		os.Exit(1)
	}

	// Generate C++
	cppOutFile := path.Join(*cppOut, "types_generated.hpp")
	if err := generate(cppTemplate, cppOutFile, schema, funcs, false); err != nil {
		fmt.Fprintf(os.Stderr, "C++ Gen Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Successfully generated unified Type definitions.")
}

func generate(tmplStr, outPath string, data Schema, funcs template.FuncMap, formatGo bool) error {
	tmpl := template.Must(template.New("").Funcs(funcs).Parse(tmplStr))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	outData := buf.Bytes()
	if formatGo {
		formatted, err := format.Source(outData)
		if err != nil {
			os.WriteFile(outPath+".debug", outData, 0644)
			return fmt.Errorf("go format error: %w", err)
		}
		outData = formatted
	}

	os.MkdirAll(filepath.Dir(outPath), 0755)
	return os.WriteFile(outPath, outData, 0644)
}

func toSnakeCase(s string) string {
	var res []rune
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				res = append(res, '_')
			}
			res = append(res, r+32)
		} else {
			res = append(res, r)
		}
	}
	return string(res)
}

func mapCppType(goType string) string {
	switch goType {
	case "string":
		return "std::string"
	case "int":
		return "int32_t"
	case "bool":
		return "bool"
	case "Type":
		return "std::shared_ptr<maml::Type>"
	case "[]Type":
		return "std::vector<std::shared_ptr<maml::Type>>"
	case "[]StructField":
		return "std::vector<maml::StructField>"
	case "[]SumVariant":
		return "std::vector<maml::SumVariant>"
	default:
		return goType
	}
}
