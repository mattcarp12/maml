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

//go:embed tast_schema.json
var schemaData []byte

//go:embed tast.tmpl
var tastTemplate string

var ifaceMarker = map[string]string{
	"Decl":    "declNode",
	"Stmt":    "stmtNode",
	"Expr":    "exprNode",
	"Pattern": "patternNode",
}

type Field struct {
	Name string
	Type string
}

type StructSpec struct {
	Name    string
	Fields  []Field
	Markers []string
	IsExpr  bool
}

func main() {
	outDir := flag.String("dir", "frontend/tast", "output directory for tast_generated.go")
	flag.Parse()
	outPath := filepath.Join(*outDir, "tast_generated.go")
	if err := generateTAST(outPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Successfully generated TAST code in %s\n", outPath)
}

func generateTAST(outPath string) error {
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
		isExpr := false
		for _, iface := range node.Interfaces {
			if iface == "Expr" {
				isExpr = true
			}
			if m := ifaceMarker[iface]; m != "" {
				markers = append(markers, m)
			}
		}
		nodes = append(nodes, StructSpec{Name: name, Fields: sortedFields(node.Fields), Markers: markers, IsExpr: isExpr})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	helpers := make([]StructSpec, 0, len(rawSchema.Helpers))
	for name, h := range rawSchema.Helpers {
		helpers = append(helpers, StructSpec{Name: name, Fields: sortedFields(h.Fields)})
	}
	sort.Slice(helpers, func(i, j int) bool { return helpers[i].Name < helpers[j].Name })

	var exprNodes []string
	for _, n := range nodes {
		if n.IsExpr && n.Name != "BlockStmt" {
			exprNodes = append(exprNodes, n.Name)
		}
	}

	tmpl, err := template.New("tast").Parse(tastTemplate)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		Nodes     []StructSpec
		Helpers   []StructSpec
		ExprNodes []string
	}{nodes, helpers, exprNodes}
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("template execution error: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		_ = os.WriteFile(outPath+".debug.go", buf.Bytes(), 0644)
		return fmt.Errorf("go format error (unformatted output written to %s): %w", outPath+".debug.go", err)
	}

	return os.WriteFile(outPath, formatted, 0644)
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
