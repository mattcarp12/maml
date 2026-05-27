package mir

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

type Emitter struct{}

func NewEmitter() *Emitter {
	return &Emitter{}
}

// Emit serializes the fully lowered Mid-Level IR to JSON for the C++ backend.
func (e *Emitter) Emit(program *Program, w io.Writer) error {
	jsonAST := e.marshalProgram(program)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonAST)
}

func (e *Emitter) marshalProgram(p *Program) map[string]interface{} {
	if p == nil {
		return nil
	}
	var decls []map[string]interface{}

	for _, td := range p.TypeDecls {
		decls = append(decls, e.marshalTypeDecl(td))
	}
	for _, fn := range p.Functions {
		decls = append(decls, e.marshalFunction(fn))
	}

	return map[string]interface{}{
		"node_type": "Program",
		"decls":     decls,
	}
}

// =============================================================================
// Structural Marshalers
// =============================================================================

func (e *Emitter) marshalTypeDecl(td *hir.TypeDecl) map[string]interface{} {
	return map[string]interface{}{
		"node_type": "TypeDecl",
		"name":      td.Name.Value,
		"type":      e.marshalType(td.Type),
	}
}

func (e *Emitter) marshalFunction(fn Function) map[string]interface{} {
	params := make([]map[string]interface{}, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = map[string]interface{}{
			"name": p.Name,
			"type": e.marshalType(p.Type),
		}
	}

	blocksMap := make(map[string]interface{})
	if fn.Graph != nil {
		for id, block := range fn.Graph.Blocks {
			blocksMap[fmt.Sprintf("%d", id)] = e.marshalBlock(block)
		}
	}

	return map[string]interface{}{
		"node_type":   "FnDecl",
		"name":        fn.Name,
		"params":      params,
		"return_type": e.marshalType(fn.ReturnType),
		"is_async":    fn.IsAsync,
		"entry_block": int(fn.Graph.Entry),
		"blocks":      blocksMap,
	}
}

func (e *Emitter) marshalBlock(b *BasicBlock) map[string]interface{} {
	stmts := make([]map[string]interface{}, len(b.Statements))
	for i, stmt := range b.Statements {
		stmts[i] = e.marshalInstruction(stmt)
	}
	return map[string]interface{}{
		"id":         int(b.ID),
		"statements": stmts,
		"terminator": e.marshalTerminator(b.Terminator),
	}
}

// =============================================================================
// Instruction & Terminator Marshalers
// =============================================================================

func (e *Emitter) marshalInstruction(inst Instruction) map[string]interface{} {
	switch i := inst.(type) {
	case *TempDeclInst:
		return map[string]interface{}{"node_type": "TempDeclInst", "name": i.Name, "maml_type": e.marshalType(i.Type)}
	case *AssignInst:
		return map[string]interface{}{"node_type": "AssignInst", "dst": i.Dst, "expr": e.marshalExpr(i.RValue)}
	case *CopyInst:
		return map[string]interface{}{"node_type": "CopyInst", "dst": i.Dst, "src": i.Src}
	case *MoveInst:
		return map[string]interface{}{"node_type": "MoveInst", "dst": i.Dst, "src": i.Src}
	case *RefAllocInst:
		return map[string]interface{}{"node_type": "RefAllocInst", "dst": i.Dst, "maml_type": e.marshalType(i.Type)}
	case *RefIncInst:
		return map[string]interface{}{"node_type": "RefIncInst", "src": i.Src}
	case *RefDecInst:
		return map[string]interface{}{"node_type": "RefDecInst", "src": i.Src}
	case *MutBorrowInst:
		return map[string]interface{}{"node_type": "MutBorrowInst", "src": i.Src}
	case *CoroPrologueInst:
		return map[string]interface{}{"node_type": "CoroPrologueInst"}
	case *IndexAssignInst:
		return map[string]interface{}{
			"node_type": "IndexAssignInst",
			"target":    i.Target,
			"index":     e.marshalExpr(i.Index),
			"value":     e.marshalExpr(i.Value),
		}
	}
	return nil
}

func (e *Emitter) marshalTerminator(term Terminator) map[string]interface{} {
	if term == nil {
		return nil
	}
	switch t := term.(type) {
	case *ReturnTerminator:
		return map[string]interface{}{"node_type": "ReturnTerminator"}
	case *JumpTerminator:
		return map[string]interface{}{"node_type": "JumpTerminator", "target": int(t.Target)}
	case *BranchTerminator:
		return map[string]interface{}{
			"node_type":    "BranchTerminator",
			"condition":    e.marshalExpr(t.Condition),
			"true_target":  int(t.TrueTarget),
			"false_target": int(t.FalseTarget),
		}
	case *UnreachableTerminator:
		return map[string]interface{}{"node_type": "UnreachableTerminator"}
	case *CoroSuspendTerminator:
		return map[string]interface{}{
			"node_type":      "CoroSuspendTerminator",
			"resume_target":  int(t.ResumeBlock),
			"cleanup_target": int(t.CleanupBlock),
			"suspend_target": int(t.SuspendBlock),
		}
	}
	return nil
}

// =============================================================================
// Expression Marshaler (Flat HIR Only)
// =============================================================================

func (e *Emitter) marshalExpr(expr hir.Expr) map[string]interface{} {
	if expr == nil {
		return nil
	}
	res := make(map[string]interface{})

	switch ex := expr.(type) {
	case *hir.Identifier:
		res["node_type"] = "Identifier"
		res["value"] = ex.Value
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.IntLiteral:
		res["node_type"] = "IntLiteral"
		res["value"] = ex.Value
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.BoolLiteral:
		res["node_type"] = "BoolLiteral"
		res["value"] = ex.Value
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.StringLiteral:
		res["node_type"] = "StringLiteral"
		res["value"] = ex.Value
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.ZeroAllocExpr:
		res["node_type"] = "ZeroAllocExpr"
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.PrefixExpr:
		res["node_type"] = "PrefixExpr"
		res["operator"] = ex.Operator
		res["right"] = e.marshalExpr(ex.Right)
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.InfixExpr:
		res["node_type"] = "InfixExpr"
		res["left"] = e.marshalExpr(ex.Left)
		res["operator"] = ex.Operator
		res["right"] = e.marshalExpr(ex.Right)
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.CallExpr:
		res["node_type"] = "CallExpr"
		res["function"] = e.marshalExpr(ex.Function)
		args := make([]map[string]interface{}, len(ex.Arguments))
		for i, arg := range ex.Arguments {
			args[i] = map[string]interface{}{
				"argument": e.marshalExpr(arg.Argument),
				"mut":      arg.Mut,
				"own":      arg.Own,
			}
		}
		res["arguments"] = args
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.FieldAccess:
		res["node_type"] = "FieldAccess"
		res["object"] = e.marshalExpr(ex.Object)
		res["field"] = ex.Field.Value
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.IndexExpr:
		res["node_type"] = "IndexExpr"
		res["left"] = e.marshalExpr(ex.Left)
		res["index"] = e.marshalExpr(ex.Index)
		res["maml_type"] = e.marshalType(ex.Type)
	case *hir.SliceExpr:
		res["node_type"] = "SliceExpr"
		res["left"] = e.marshalExpr(ex.Left)
		res["low"] = e.marshalExpr(ex.Low)
		res["high"] = e.marshalExpr(ex.High)
		res["maml_type"] = e.marshalType(ex.Type)
	}
	return res
}

// =============================================================================
// Type Marshaler
// =============================================================================

func (e *Emitter) marshalType(t types.Type) map[string]interface{} {
	if t == nil {
		return nil
	}
	res := make(map[string]interface{})

	switch ty := t.(type) {
	case types.IntType:
		res["kind"] = "Int"
	case types.BoolType:
		res["kind"] = "Bool"
	case types.StringType:
		res["kind"] = "String"
	case types.UnitType:
		res["kind"] = "Unit"
	case *types.StructType:
		res["kind"] = "Struct"
		res["name"] = ty.Name
		fields := make([]map[string]interface{}, len(ty.Fields))
		for i, f := range ty.Fields {
			fields[i] = map[string]interface{}{
				"name": f.Name,
				"type": e.marshalType(f.Type),
			}
		}
		res["fields"] = fields
	case types.ArrayType:
		res["kind"] = "Array"
		res["base"] = e.marshalType(ty.Base)
		res["size"] = ty.Size
	case types.SliceType:
		res["kind"] = "Slice"
		res["base"] = e.marshalType(ty.Base)
	case types.VectorType:
		res["kind"] = "Vector"
		res["base"] = e.marshalType(ty.Base)
	case types.MapType:
		res["kind"] = "Map"
		res["key"] = e.marshalType(ty.Key)
		res["value"] = e.marshalType(ty.Value)
	case *types.SumType:
		res["kind"] = "SumType"
		res["name"] = ty.BaseName
		variants := make([]map[string]interface{}, len(ty.Variants))
		for i, v := range ty.Variants {
			vfields := make([]map[string]interface{}, len(v.Fields))
			for j, f := range v.Fields {
				vfields[j] = map[string]interface{}{
					"name": f.Name,
					"type": e.marshalType(f.Type),
				}
			}
			variants[i] = map[string]interface{}{
				"name":         v.Name,
				"discriminant": v.Discriminant,
				"fields":       vfields,
			}
		}
		res["variants"] = variants
	case types.TaskType:
		res["kind"] = "Task"
		res["base"] = e.marshalType(ty.Base)
	case *types.FunctionType:
		res["kind"] = "Function"
	case types.UnknownType:
		res["kind"] = "Unknown"
	}
	return res
}
