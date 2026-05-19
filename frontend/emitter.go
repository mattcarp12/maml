package frontend

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
	case sema.TaskType:
		res["kind"] = "Task"
		res["result"] = e.marshalType(v.Base)
	case sema.VectorType:
		res["kind"] = "Vector"
		res["base"] = e.marshalType(v.Base)
	case *sema.StructType:
		res["kind"] = "Struct"
		res["name"] = v.Name
	case *sema.FunctionType:
		res["kind"] = "Function"
		res["return"] = e.marshalType(v.Return)
	default:
		res["kind"] = "Unknown"
	}
	return res
}
