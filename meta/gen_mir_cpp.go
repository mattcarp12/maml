//go:build ignore

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

//go:embed mir_schema.json
var schemaData []byte

//go:embed mir_cpp.tmpl
var cppTemplate string

type Field struct {
	GoName  string
	CppName string
	CppType string
	JsonKey string // explicitly matches export.go
}

type StructSpec struct {
	Name   string
	Opcode string
	Fields []Field
}

type TemplateData struct {
	Values       []StructSpec
	Instructions []StructSpec
	Terminators  []StructSpec
	Helpers      []StructSpec
}

func main() {
	outDir := flag.String("dir", "backend/include/", "output directory for mir_generated.hpp")
	flag.Parse()

	if err := generateCpp(*outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Successfully generated C++ MIR headers in %s\n", filepath.Join(*outDir, "mir_generated.hpp"))
}

func generateCpp(outDir string) error {
	var rawSchema struct {
		Nodes map[string]struct {
			Interfaces []string          `json:"interfaces"`
			Fields     map[string]string `json:"fields"`
			EmitCpp    *bool             `json:"emitCpp,omitempty"` // Add this field
		} `json:"nodes"`
		Helpers map[string]struct {
			Fields map[string]string `json:"fields"`
		} `json:"helpers"`
	}
	json.Unmarshal(schemaData, &rawSchema)

	data := TemplateData{}

	for name, node := range rawSchema.Nodes {
		// Skip C++ generation if explicitly marked as false
		if node.EmitCpp != nil && !*node.EmitCpp {
			continue
		}

		spec := StructSpec{
			Name:   name,
			Opcode: toSnakeCase(strings.ReplaceAll(strings.ReplaceAll(name, "Inst", ""), "Terminator", "")),
			Fields: buildFields(node.Fields),
		}

		if contains(node.Interfaces, "Value") {
			data.Values = append(data.Values, spec)
		} else if contains(node.Interfaces, "Instruction") {
			data.Instructions = append(data.Instructions, spec)
		} else if contains(node.Interfaces, "Terminator") {
			data.Terminators = append(data.Terminators, spec)
		}
	}

	for name, h := range rawSchema.Helpers {
		data.Helpers = append(data.Helpers, StructSpec{
			Name:   name,
			Fields: buildFields(h.Fields),
		})
	}

	sortStructs(data.Values)
	sortStructs(data.Instructions)
	sortStructs(data.Terminators)
	sortStructs(data.Helpers)

	tmpl := template.Must(template.New("cpp").Parse(cppTemplate))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	os.MkdirAll(outDir, 0755)
	outfile := filepath.Join(outDir, "mir_generated.hpp")
	output := buf.Bytes()
	if shouldWriteFile(outfile, string(output)) {
		os.WriteFile(outfile, output, 0644)
	}
	return nil
}

func shouldWriteFile(path string, newContent string) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return true // file doesn't exist
	}
	return string(existing) != newContent
}

func buildFields(fields map[string]string) []Field {
	var names []string
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)

	var res []Field
	for _, name := range names {
		jsonKey := toSnakeCase(name)
		cppName := jsonKey

		// Intercept C++ keyword conflicts and export.go mapping deviations
		if cppName == "operator" {
			cppName = "operator_"
		}
		if jsonKey == "r_value" {
			jsonKey = "value"
		}

		res = append(res, Field{
			GoName:  name,
			CppName: cppName,
			CppType: mapCppType(fields[name]),
			JsonKey: jsonKey,
		})
	}
	return res
}

func mapCppType(goType string) string {
	switch goType {
	case "string":
		return "std::string"
	case "[]string":
		return "std::vector<std::string>"
	case "int", "int32":
		return "int32_t"
	case "int64":
		return "int64_t"
	case "bool":
		return "bool"
	case "types.Type":
		return "std::shared_ptr<maml::Type>"
	case "[]types.Type":
		return "std::vector<std::shared_ptr<maml::Type>>"
	case "Value", "mir.Value":
		return "Value"
	case "[]Value", "[]mir.Value":
		return "std::vector<Value>"
	case "[]MIRCallArg":
		return "std::vector<MIRCallArg>"
	case "BlockID":
		return "std::string"
	default:
		return goType
	}
}

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

func toSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func sortStructs(s []StructSpec) {
	sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
}
