//go:build ignore

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"text/template"
)

//go:embed ast_schema.json
var schemaData []byte

//go:embed ast.tmpl
var astTemplate string

var ifaceMarker = map[string]string{
	"Decl":     "declNode",
	"Stmt":     "stmtNode",
	"Expr":     "exprNode",
	"TypeExpr": "typeNode",
	"Pattern":  "patternNode",
}

type Field struct {
	Name string
	Type string
}

type StructSpec struct {
	Name    string
	Fields  []Field
	Markers []string
}

func main() {
	outDir := flag.String("dir", "frontend/ast/", "output directory for ast_generated.go")
	flag.Parse()

	if err := generateAST(*outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Successfully generated AST code in %s\n", filepath.Join(*outDir, "ast_generated.go"))
}

func generateAST(outDir string) error {
	var rawSchema struct {
		Nodes map[string]struct {
			Interfaces []string          `json:"interfaces"`
			Fields     map[string]string `json:"fields"`
		} `json:"nodes"`
		Helpers map[string]struct {
			Fields map[string]string `json:"fields"`
		} `json:"helpers"`
	}
	if err := json.Unmarshal(schemaData, &rawSchema); err != nil {
		return fmt.Errorf("parsing json: %w", err)
	}

	nodes := make([]StructSpec, 0, len(rawSchema.Nodes))
	for name, node := range rawSchema.Nodes {
		markers := make([]string, 0, len(node.Interfaces))
		for _, iface := range node.Interfaces {
			if m := ifaceMarker[iface]; m != "" {
				markers = append(markers, m)
			}
		}
		nodes = append(nodes, StructSpec{Name: name, Fields: sortedFields(node.Fields), Markers: markers})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	helpers := make([]StructSpec, 0, len(rawSchema.Helpers))
	for name, h := range rawSchema.Helpers {
		helpers = append(helpers, StructSpec{Name: name, Fields: sortedFields(h.Fields)})
	}
	sort.Slice(helpers, func(i, j int) bool { return helpers[i].Name < helpers[j].Name })

	tmpl, err := template.New("ast").Parse(astTemplate)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Nodes, Helpers []StructSpec }{nodes, helpers}); err != nil {
		return fmt.Errorf("template execution error: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		_ = os.WriteFile(filepath.Join(outDir, "ast_generated.debug.go"), buf.Bytes(), 0644)
		return fmt.Errorf("go format error (unformatted output written to ast_generated.debug.go): %w", err)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	return os.WriteFile(filepath.Join(outDir, "ast_generated.go"), formatted, 0644)
}

func sortedFields(fieldsMap map[string]string) []Field {
	names := make([]string, 0, len(fieldsMap))
	for name := range fieldsMap {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]Field, len(names))
	for i, name := range names {
		fields[i] = Field{Name: name, Type: fieldsMap[name]}
	}
	return fields
}
