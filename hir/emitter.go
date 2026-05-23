package hir

import (
	"encoding/json"
	"io"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/sema"
)

type Emitter struct {
	typeMap   map[ast.Node]sema.Type
	escapeMap map[ast.Node]escape.EscapeState
}

func NewEmitter(typeMap map[ast.Node]sema.Type, escapeMap map[ast.Node]escape.EscapeState) *Emitter {
	return &Emitter{typeMap: typeMap, escapeMap: escapeMap}
}

func (e *Emitter) Emit(program *ast.Program, w io.Writer) error {
	jsonAST := e.marshalNode(program)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonAST)
}

func (e *Emitter) marshalNode(node ast.Node) map[string]interface{} {
	if node == nil {
		return nil
	}

	res := make(map[string]interface{})

	// Inject Type and Escape Metadata if available
	if t, ok := e.typeMap[node]; ok && t != nil {
		res["maml_type"] = e.marshalType(t)
	}
	if esc, ok := e.escapeMap[node]; ok {
		res["is_heap"] = esc == escape.StateHeap
	}

	// Dynamic dispatch based on AST node type
	switch n := node.(type) {
	case *ast.Program:
		res["node_type"] = "Program"
		var decls []interface{}
		for _, d := range n.Decls {
			decls = append(decls, e.marshalNode(d))
		}
		res["decls"] = decls

	case *ast.FnDecl:
		res["node_type"] = "FnDecl"
		res["name"] = n.Name
		res["is_async"] = n.IsAsync

		var params []interface{}
		for _, p := range n.Params {
			params = append(params, map[string]interface{}{
				"name": p.Name,
				"type": e.marshalType(e.typeMap[p.Type]),
				"own":  p.Own,
				"mut":  p.Mut,
			})
		}
		res["params"] = params

		if n.ReturnType != nil {
			res["return_type"] = e.marshalType(e.typeMap[n.ReturnType])
		}

		res["body"] = e.marshalNode(n.Body)
	case *ast.BlockStmt:
		res["node_type"] = "BlockStmt"
		var stmts []interface{}
		for _, s := range n.Statements {
			stmts = append(stmts, e.marshalNode(s))
		}
		res["statements"] = stmts

	case *ast.DeclareStmt:
		res["node_type"] = "DeclareStmt"
		res["name"] = n.Name
		res["mutable"] = n.Mutable
		res["value"] = e.marshalNode(n.Value)

	case *ast.AssignStmt:
		res["node_type"] = "AssignStmt"
		res["lvalue"] = e.marshalNode(n.LValue)
		res["rvalue"] = e.marshalNode(n.RValue)

	case *ast.ReturnStmt:
		res["node_type"] = "ReturnStmt"
		res["value"] = e.marshalNode(n.Value)

	case *ast.YieldStmt:
		res["node_type"] = "YieldStmt"
		res["value"] = e.marshalNode(n.Value)

	case *ast.ExprStmt:
		res["node_type"] = "ExprStmt"
		res["value"] = e.marshalNode(n.Value)

	case *ast.ForStmt:
		res["node_type"] = "ForStmt"
		res["init"] = e.marshalNode(n.Init)
		res["condition"] = e.marshalNode(n.Condition)
		res["post"] = e.marshalNode(n.Post)
		res["body"] = e.marshalNode(n.Body)

	case *ast.IfExpr:
		res["node_type"] = "IfExpr"
		res["condition"] = e.marshalNode(n.Condition)
		res["consequence"] = e.marshalNode(n.Consequence)
		if n.Alternative != nil {
			res["alternative"] = e.marshalNode(n.Alternative)
		}

	case *ast.MatchExpr:
		res["node_type"] = "MatchExpr"
		res["subject"] = e.marshalNode(n.Subject)
		var arms []interface{}
		for _, arm := range n.Arms {
			arms = append(arms, map[string]interface{}{
				"pattern": e.marshalPattern(arm.Pattern),
				"body":    e.marshalNode(arm.Body),
			})
		}
		res["arms"] = arms

	case *ast.AwaitExpr:
		res["node_type"] = "AwaitExpr"
		res["value"] = e.marshalNode(n.Value)

	case *ast.CallExpr:
		res["node_type"] = "CallExpr"
		res["function"] = e.marshalNode(n.Function)
		var args []interface{}
		for _, arg := range n.Arguments {
			args = append(args, map[string]interface{}{
				"value": e.marshalNode(arg.Argument),
				"own":   arg.Own,
				"mut":   arg.Mut,
			})
		}
		res["arguments"] = args

	case *ast.InfixExpr:
		res["node_type"] = "InfixExpr"
		res["operator"] = n.Operator
		res["left"] = e.marshalNode(n.Left)
		res["right"] = e.marshalNode(n.Right)

	case *ast.PrefixExpr:
		res["node_type"] = "PrefixExpr"
		res["operator"] = n.Operator
		res["right"] = e.marshalNode(n.Right)

	case *ast.ArrayLiteral:
		res["node_type"] = "ArrayLiteral"
		var elements []interface{}
		for _, el := range n.Elements {
			elements = append(elements, e.marshalNode(el))
		}
		res["elements"] = elements

	case *ast.StructLiteral:
		res["node_type"] = "StructLiteral"
		res["struct_type"] = e.marshalNode(n.Type)
		fields := make([]interface{}, 0)
		for _, f := range n.Fields {
			fields = append(fields, map[string]interface{}{
				"name":  f.Name.Value,
				"value": e.marshalNode(f.Value),
			})
		}
		res["fields"] = fields

	case *ast.VariantLiteral:
		res["node_type"] = "VariantLiteral"
		res["variant_name"] = n.VariantName
		fields := make([]interface{}, 0)
		for _, f := range n.Fields {
			fields = append(fields, map[string]interface{}{
				"name":  f.Name.Value,
				"value": e.marshalNode(f.Value),
			})
		}
		res["fields"] = fields

	case *ast.FieldAccess:
		res["node_type"] = "FieldAccess"
		res["object"] = e.marshalNode(n.Object)
		res["field"] = n.Field.Value

	case *ast.IndexExpr:
		res["node_type"] = "IndexExpr"
		res["left"] = e.marshalNode(n.Left)
		res["index"] = e.marshalNode(n.Index)

	case *ast.SliceExpr:
		res["node_type"] = "SliceExpr"
		res["left"] = e.marshalNode(n.Left)
		res["low"] = e.marshalNode(n.Low)
		res["high"] = e.marshalNode(n.High)

	case *ast.Identifier:
		res["node_type"] = "Identifier"
		res["value"] = n.Value

	case *ast.IntLiteral:
		res["node_type"] = "IntLiteral"
		res["value"] = n.Value

	case *ast.BoolLiteral:
		res["node_type"] = "BoolLiteral"
		res["value"] = n.Value

	case *ast.StringLiteral:
		res["node_type"] = "StringLiteral"
		res["value"] = n.Value
	}

	return res
}

func (e *Emitter) marshalPattern(pat ast.Pattern) map[string]interface{} {
	if pat == nil {
		return nil
	}
	res := make(map[string]interface{})

	switch p := pat.(type) {
	case *ast.WildcardPattern:
		res["kind"] = "Wildcard"
	case *ast.LiteralPattern:
		res["kind"] = "Literal"
		res["value"] = e.marshalNode(p.Value)
	case *ast.VariantPattern:
		res["kind"] = "Variant"
		res["name"] = p.Name
		if p.Binding != nil {
			res["binding"] = p.Binding.Value
		}
		fields := make([]interface{}, 0)
		for _, f := range p.Fields {
			fields = append(fields, map[string]interface{}{
				"field":   f.Field,
				"binding": f.Binding.Value,
			})
		}
		res["fields"] = fields
	}
	return res
}

func (e *Emitter) marshalType(t sema.Type) map[string]interface{} {
	if t == nil {
		return nil
	}
	res := make(map[string]interface{})

	switch v := t.(type) {
	case sema.IntType:
		res["kind"] = "Int"
	case sema.BoolType:
		res["kind"] = "Bool"
	case sema.StringType:
		res["kind"] = "String"
	case sema.UnitType:
		res["kind"] = "Unit"
	case sema.ArrayType:
		res["kind"] = "Array"
		res["size"] = v.Size
		res["base"] = e.marshalType(v.Base)
	case sema.SliceType:
		res["kind"] = "Slice"
		res["base"] = e.marshalType(v.Base)
	case sema.MapType:
		res["kind"] = "Map"
		res["key"] = e.marshalType(v.Key)
		res["value"] = e.marshalType(v.Value)
	case sema.TaskType:
		res["kind"] = "Task"
		res["result"] = e.marshalType(v.Base)
	case sema.VectorType:
		res["kind"] = "Vector"
		res["base"] = e.marshalType(v.Base)
	case *sema.StructType:
		res["kind"] = "Struct"
		res["name"] = v.Name
		fields := make([]interface{}, 0)
		for _, f := range v.Fields {
			fields = append(fields, map[string]interface{}{
				"name": f.Name,
				"type": e.marshalType(f.Type),
			})
		}
		res["fields"] = fields
	case *sema.SumType:
		res["kind"] = "SumType"
		res["name"] = v.BaseName
		var args []interface{}
		for _, arg := range v.TypeArgs {
			args = append(args, e.marshalType(arg))
		}
		res["type_args"] = args

		var variants []interface{}
		for _, variant := range v.Variants {
			fields := make([]interface{}, 0)
			for _, f := range variant.Fields {
				fields = append(fields, map[string]interface{}{
					"name": f.Name,
					"type": e.marshalType(f.Type),
				})
			}
			variants = append(variants, map[string]interface{}{
				"name":         variant.Name,
				"discriminant": variant.Discriminant,
				"fields":       fields,
			})
		}
		res["variants"] = variants
	case *sema.FunctionType:
		res["kind"] = "Function"
		res["return"] = e.marshalType(v.Return)
	default:
		res["kind"] = "Unknown"
	}
	return res
}
