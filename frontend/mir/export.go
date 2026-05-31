package mir

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// DTO Schemas
// =============================================================================

type exportProgram struct {
	Functions []exportFunction `json:"functions"`
}

type exportFunction struct {
	Name       string                 `json:"name"`
	ReturnType any                    `json:"return_type"`
	Params     []exportParam          `json:"params"`
	IsAsync    bool                   `json:"is_async"`
	Entry      string                 `json:"entry_block"`
	Blocks     map[string]exportBlock `json:"blocks"`
}

type exportParam struct {
	Name string `json:"name"`
	Type any    `json:"type"`
}

type exportBlock struct {
	Instructions []map[string]any `json:"instructions"`
	Terminator   map[string]any   `json:"terminator"`
}

// =============================================================================
// Exporter Logic
// =============================================================================

// MarshalProgram serializes the MIR program into a strictly controlled,
// flat JSON schema designed explicitly for the LLVM backend.
func MarshalProgram(p *Program) ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}

	dto := exportProgram{
		Functions: make([]exportFunction, 0, len(p.Functions)),
	}

	for _, fn := range p.Functions {
		dto.Functions = append(dto.Functions, buildFunctionDTO(&fn))
	}

	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(dto)
	return buffer.Bytes(), err
}

func buildFunctionDTO(fn *Function) exportFunction {
	blocks := make(map[string]exportBlock)
	var entry string

	if fn.Graph != nil {
		entry = fmt.Sprintf("%d", fn.Graph.Entry)
		for id, block := range fn.Graph.Blocks {
			blocks[fmt.Sprintf("%d", id)] = buildBlockDTO(block)
		}
	}

	params := make([]exportParam, 0, len(fn.Params))
	for _, p := range fn.Params {
		params = append(params, exportParam{
			Name: p.Name,
			Type: lowerType(p.Type),
		})
	}

	return exportFunction{
		Name:       fn.Name,
		ReturnType: lowerType(fn.ReturnType),
		Params:     params,
		IsAsync:    fn.IsAsync,
		Entry:      entry,
		Blocks:     blocks,
	}
}

func buildBlockDTO(block *BasicBlock) exportBlock {
	insts := make([]map[string]any, 0, len(block.Statements))
	for _, inst := range block.Statements {
		insts = append(insts, buildInstructionDTO(inst))
	}

	return exportBlock{
		Instructions: insts,
		Terminator:   buildTerminatorDTO(block.Terminator),
	}
}

// =============================================================================
// Instruction & Terminator Mappers
// =============================================================================

func buildInstructionDTO(inst Instruction) map[string]any {
	switch i := inst.(type) {
	case *TempDeclInst:
		return map[string]any{"op": "temp_decl", "name": i.Name, "type": lowerType(i.Type)}
	case *AssignInst:
		return map[string]any{"op": "assign", "dst": i.Dst, "value": buildExprDTO(i.RValue)}
	case *IndexAssignInst:
		return map[string]any{"op": "index_assign", "target": i.Target, "target_type": lowerType(i.TargetType), "index": buildExprDTO(i.Index), "value": buildExprDTO(i.Value)}
	case *StructInitInst:
		// Emitted once per field of a struct literal.
		// The backend uses "field_index" as the GEP constant offset and
		// "field_name" for debug info / error messages.
		m := map[string]any{
			"op":          "struct_init",
			"dst":         i.Dst,
			"field_name":  i.FieldName,
			"field_index": i.FieldIndex,
			"value":       buildExprDTO(i.Value),
		}
		if len(i.VariantLayout) > 0 {
			layout := make([]any, len(i.VariantLayout))
			for idx, ty := range i.VariantLayout {
				layout[idx] = lowerType(ty)
			}
			m["variant_layout"] = layout
		}
		return m
	case *FieldReadInst:
		m := map[string]any{
			"op":          "field_read",
			"dst":         i.Dst,
			"object":      buildExprDTO(i.Object),
			"field_name":  i.FieldName,
			"field_index": i.FieldIndex,
			"type":        lowerType(i.Type),
		}
		if len(i.VariantLayout) > 0 {
			layout := make([]any, len(i.VariantLayout))
			for idx, ty := range i.VariantLayout {
				// Reuse your existing type-lowering serialization logic here
				layout[idx] = lowerType(ty)
			}
			m["variant_layout"] = layout
		}
		return m
	case *ArrayInitInst:
		return map[string]any{
			"op":    "array_init",
			"dst":   i.Dst,
			"index": i.Index,
			"value": buildExprDTO(i.Value),
		}
	case *SliceInst:
		var lowDTO, highDTO any
		if i.Low != nil {
			lowDTO = buildExprDTO(i.Low)
		}
		if i.High != nil {
			highDTO = buildExprDTO(i.High)
		}
		return map[string]any{
			"op":             "slice_read",
			"dst":            i.Dst,
			"left":           buildExprDTO(i.Left),
			"container_type": lowerType(i.ContainerType),
			"low":            lowDTO,
			"high":           highDTO,
			"result_type":    lowerType(i.ResultType),
		}
	case *CopyInst:
		return map[string]any{"op": "copy", "dst": i.Dst, "src": i.Src}
	case *MoveInst:
		return map[string]any{"op": "move", "dst": i.Dst, "src": i.Src}
	case *RefAllocInst:
		return map[string]any{"op": "ref_alloc", "dst": i.Dst, "type": lowerType(i.Type)}
	case *RefIncInst:
		return map[string]any{"op": "ref_inc", "src": i.Src}
	case *RefDecInst:
		return map[string]any{"op": "ref_dec", "src": i.Src}
	case *MutBorrowInst:
		return map[string]any{"op": "mut_borrow", "src": i.Src}
	case *CoroPrologueInst:
		return map[string]any{"op": "coro_prologue"}
	default:
		return map[string]any{"op": "unknown_inst"}
	}
}

func buildTerminatorDTO(term Terminator) map[string]any {
	switch t := term.(type) {
	case *ReturnTerminator:
		return map[string]any{"op": "ret"}
	case *JumpTerminator:
		return map[string]any{"op": "br", "target": fmt.Sprintf("%d", t.Target)}
	case *BranchTerminator:
		return map[string]any{"op": "cond_br", "condition": buildExprDTO(t.Condition), "true_target": fmt.Sprintf("%d", t.TrueTarget), "false_target": fmt.Sprintf("%d", t.FalseTarget)}
	case *UnreachableTerminator:
		return map[string]any{"op": "unreachable"}
	case *CoroSuspendTerminator:
		return map[string]any{"op": "coro_suspend", "resume": fmt.Sprintf("%d", t.ResumeBlock), "cleanup": fmt.Sprintf("%d", t.CleanupBlock), "suspend": fmt.Sprintf("%d", t.SuspendBlock)}
	default:
		return map[string]any{"op": "unknown_term"}
	}
}

// =============================================================================
// Expression & Type Lowering
// =============================================================================

// buildExprDTO strips away frontend TAST node types and translates
// expressions into simple opcode objects for the backend.
//
// NOTE: After the flatten pass, struct-related compound nodes should no longer
// appear inside AssignInst.RValue (they are replaced by StructInitInst and
// FieldReadInst).  The cases below are kept as a safety net for any path that
// bypasses flattening (e.g. direct AssignInst emission in builder.go) and for
// the Map/Vec literal pass-through path in flattenStructLiteral.
func buildExprDTO(expr tast.Expr) any {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *tast.Identifier:
		return map[string]any{"op": "ident", "value": e.Value}
	case *tast.IntLiteral:
		return map[string]any{"op": "const_int", "value": e.Value}
	case *tast.BoolLiteral:
		return map[string]any{"op": "const_bool", "value": e.Value}
	case *tast.StringLiteral:
		return map[string]any{"op": "const_string", "value": e.Value}
	case *tast.InfixExpr:
		return map[string]any{"op": "infix", "operator": e.Operator, "left": buildExprDTO(e.Left), "right": buildExprDTO(e.Right)}
	case *tast.PrefixExpr:
		return map[string]any{"op": "prefix", "operator": e.Operator, "right": buildExprDTO(e.Right)}
	case *tast.CallExpr:
		args := []any{}
		for _, arg := range e.Arguments {
			args = append(args, buildExprDTO(arg.Argument))
		}
		return map[string]any{"op": "call", "function": buildExprDTO(e.Function), "arguments": args}

	case *tast.VariantLiteral, *tast.MapLiteral, *tast.VecLiteral, *tast.StructLiteral:
		// All composite literals go through alloc_composite for runtime construction
		fields := []any{}
		var typ any
		switch v := e.(type) {
		case *tast.VariantLiteral:
			for _, arg := range v.Arguments {
				fields = append(fields, map[string]any{"value": buildExprDTO(arg)})
			}
			typ = lowerType(v.Type)
		case *tast.MapLiteral:
			for _, el := range v.Elements {
				fields = append(fields, map[string]any{
					"key":   buildExprDTO(el.Key),
					"value": buildExprDTO(el.Value),
				})
			}
			typ = lowerType(v.Type)
		case *tast.VecLiteral:
			for _, el := range v.Elements {
				fields = append(fields, map[string]any{"value": buildExprDTO(el)})
			}
			typ = lowerType(v.Type)
		case *tast.StructLiteral:
			// existing logic...
			for _, f := range v.Fields {
				entry := map[string]any{"value": buildExprDTO(f.Value)}
				if f.Key != nil {
					entry["key"] = buildExprDTO(f.Key)
				}
				fields = append(fields, entry)
			}
			typ = lowerType(v.Type)
		}

		return map[string]any{
			"op":     "alloc_composite",
			"type":   typ,
			"fields": fields,
		}

	// FieldAccess: only reaches here if a FieldAccess somehow bypassed
	// flattenFieldAccess (should not happen in practice).  Emit a best-effort
	// DTO so the backend can at least report a meaningful error.
	case *tast.FieldAccess:
		fieldIndex := -1
		if ident, ok := e.Object.(*tast.Identifier); ok {
			if st, ok := ident.Type.(*types.StructType); ok {
				fieldIndex = st.GetFieldIndex(e.Field.Value)
			}
		}
		return map[string]any{
			"op":          "field_access",
			"object":      buildExprDTO(e.Object),
			"field_name":  e.Field.Value,
			"field_index": fieldIndex,
		}

	case *tast.IndexExpr:
		return map[string]any{
			"op":        "index_read",
			"left":      buildExprDTO(e.Left),
			"left_type": lowerType(e.Left.(*tast.Identifier).Type),
			"index":     buildExprDTO(e.Index),
			"elem_type": lowerType(e.Type),
		}

	default:
		return map[string]any{"op": "unsupported_expr"}
	}
}

// lowerType handles the critical task of stripping frontend Type interfaces
// (like types.IntType) and emitting low-level primitive strings (like "i32")
func lowerType(t types.Type) any {
	if t == nil {
		return "void"
	}
	switch v := t.(type) {
	case types.IntType:
		return "i32"
	case types.BoolType:
		return "i1"
	case types.UnitType:
		return "void"
	case types.StringType:
		return "string"
	case *types.StructType:
		// Emit the full field layout so the backend can verify its own alloca size.
		fields := make([]map[string]any, 0, len(v.Fields))
		for idx, f := range v.Fields {
			fields = append(fields, map[string]any{
				"index": idx,
				"name":  f.Name,
				"type":  lowerType(f.Type),
			})
		}
		return map[string]any{
			"kind":   "struct",
			"name":   v.Name,
			"size":   v.SizeInBytes(),
			"align":  v.Alignment(),
			"fields": fields,
		}
	case *types.SumType:
		return map[string]any{
			"kind": "sum_type",
			"name": v.BaseName,
			"size": v.SizeInBytes(),
		}
	case types.ArrayType:
		return map[string]any{
			"kind":      "array",
			"size":      v.Size,
			"elem_type": lowerType(v.Base),
		}
	case types.SliceType:
		return map[string]any{
			"kind":      "slice",
			"elem_type": lowerType(v.Base),
		}
	case types.VectorType:
		return map[string]any{
			"kind":      "vector",
			"elem_type": lowerType(v.Base),
		}
	default:
		if t.IsReferenceType() {
			return "ptr"
		}
		return "void"
	}
}
