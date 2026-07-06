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
	"strings"
	"text/template"
)

//go:embed hir_schema.json
var schemaData []byte

//go:embed hir.tmpl
var hirTemplate string

//go:embed hir_lower.tmpl
var lowerTemplate string

// ifaceMarker maps interface names to their marker method names.
var ifaceMarker = map[string]string{
	"Decl": "declNode",
	"Stmt": "stmtNode",
	"Expr": "exprNode",
	"Oper": "isOperand",
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
	Lower   bool // emit a generated lowering constructor for this node/helper
}

func main() {
	outDir := flag.String("out", "frontend/hir", "output directory")
	flag.Parse()

	if err := generateHIR(*outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Successfully generated HIR code.")
}

func generateHIR(outDir string) error {
	var rawSchema struct {
		Nodes map[string]struct {
			Interfaces []string          `json:"interfaces"`
			Fields     map[string]string `json:"fields"`
			Lower      bool              `json:"lower"`
		} `json:"nodes"`
		Helpers map[string]struct {
			Fields map[string]string `json:"fields"`
			Lower  bool              `json:"lower"`
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
		nodes = append(nodes, StructSpec{
			Name:    name,
			Fields:  sortedFields(node.Fields),
			Markers: markers,
			IsExpr:  isExpr,
			Lower:   node.Lower,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	helpers := make([]StructSpec, 0, len(rawSchema.Helpers))
	for name, h := range rawSchema.Helpers {
		helpers = append(helpers, StructSpec{
			Name:   name,
			Fields: sortedFields(h.Fields),
			Lower:  h.Lower,
		})
	}
	sort.Slice(helpers, func(i, j int) bool { return helpers[i].Name < helpers[j].Name })

	// Sorted interface names for deterministic output.
	ifaceNames := make([]string, 0, len(ifaceMarker))
	for name := range ifaceMarker {
		ifaceNames = append(ifaceNames, name)
	}
	sort.Strings(ifaceNames)

	var exprNodes []string
	for _, n := range nodes {
		if n.IsExpr && n.Name != "BlockStmt" {
			exprNodes = append(exprNodes, n.Name)
		}
	}

	data := struct {
		IfaceNames  []string
		IfaceMarker map[string]string
		Nodes       []StructSpec
		Helpers     []StructSpec
		ExprNodes   []string
	}{ifaceNames, ifaceMarker, nodes, helpers, exprNodes}

	if err := renderFile(filepath.Join(outDir, "hir_generated.go"), hirTemplate, data); err != nil {
		return fmt.Errorf("generating hir_generated.go: %w", err)
	}
	if err := renderFile(filepath.Join(outDir, "lower_generated.go"), lowerTemplate, data); err != nil {
		return fmt.Errorf("generating lower_generated.go: %w", err)
	}
	return nil
}

func renderFile(outPath, tmplText string, data any) error {
	funcMap := template.FuncMap{
		// fieldKind classifies a field type for the lowering template.
		// Returns one of: "Expr", "blockstmt", "Stmt", "scalar".
		"fieldKind": func(typ string) string {
			switch {
			case typ == "*BlockStmt":
				return "blockstmt"
			case typ == "Expr" || typ == "Stmt":
				return typ
			default:
				return "scalar"
			}
		},
		// sliceElem returns the element type for a "[]T" string, or "" if not a slice.
		"sliceElem": func(typ string) string {
			if strings.HasPrefix(typ, "[]") {
				return strings.TrimPrefix(typ, "[]")
			}
			return ""
		},
	}

	tmpl, err := template.New("codegen").Funcs(funcMap).Parse(tmplText)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("template execution error: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		debugPath := outPath + ".debug.go"
		_ = os.WriteFile(debugPath, buf.Bytes(), 0644)
		return fmt.Errorf("go format error (unformatted output written to %s): %w", debugPath, err)
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