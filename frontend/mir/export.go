// =============================================================================
// frontend/mir/export.go
// =============================================================================

package mir

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mattcarp12/maml/frontend/layout"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// DTO Schemas
// =============================================================================

type exportProgram struct {
	Functions []exportFunction `json:"functions"`
}

type exportFunction struct {
	Name       string        `json:"name"`
	ReturnType any           `json:"return_type"`
	Params     []exportParam `json:"params"`
	IsAsync    bool          `json:"is_async"`
	Entry      string        `json:"entry_block"`
	Blocks     []exportBlock `json:"blocks"`
}

type exportParam struct {
	Name string `json:"name"`
	Type any    `json:"type"`
}

type exportBlock struct {
	ID           string           `json:"id"`
	Instructions []map[string]any `json:"instructions"`
	Terminator   map[string]any   `json:"terminator"`
}

// =============================================================================
// Exporter Logic
// =============================================================================

// MarshalProgram serializes the MIR program into a strictly controlled,
// flat JSON schema designed explicitly for the LLVM backend.
func MarshalProgram(p *Program, target *layout.Target) ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}

	dto := exportProgram{
		Functions: make([]exportFunction, 0, len(p.Functions)),
	}

	for _, fn := range p.Functions {
		dto.Functions = append(dto.Functions, buildFunctionDTO(&fn, target))
	}

	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(dto)
	return buffer.Bytes(), err
}

func buildFunctionDTO(fn *Function, target *layout.Target) exportFunction {
	blocks := []exportBlock{}
	var entry string

	if fn.Graph != nil {
		entry = fmt.Sprintf("%d", fn.Graph.Entry)
		for _, block := range fn.Graph.SortedBlocks() {
			blocks = append(blocks, buildBlockDTO(block, target))
		}
	}

	params := make([]exportParam, 0, len(fn.Params))
	for _, p := range fn.Params {
		params = append(params, exportParam{
			Name: p.Name,
			Type: lowerType(p.Type, target),
		})
	}

	return exportFunction{
		Name:       fn.Name,
		ReturnType: lowerType(fn.ReturnType, target),
		Params:     params,
		IsAsync:    fn.IsAsync,
		Entry:      entry,
		Blocks:     blocks,
	}
}

func buildBlockDTO(block *BasicBlock, target *layout.Target) exportBlock {
	insts := make([]map[string]any, 0, len(block.Statements))
	for _, inst := range block.Statements {
		if _, isKeepAlive := inst.(*KeepAliveInst); isKeepAlive {
			continue
		}
		insts = append(insts, buildInstructionDTO(inst, target))
	}

	return exportBlock{
		ID:           fmt.Sprintf("%d", block.ID),
		Instructions: insts,
		Terminator:   buildTerminatorDTO(block.Terminator, target),
	}
}

// =============================================================================
// Instruction & Terminator Mappers
// =============================================================================

func buildInstructionDTO(inst Instruction, target *layout.Target) map[string]any {
	switch i := inst.(type) {
	case *TempDeclInst:
		return map[string]any{"op": "temp_decl", "name": i.Name, "type": lowerType(i.Type, target)}
	case *AssignInst:
		return map[string]any{"op": "assign", "dst": i.Dst, "value": buildValueDTO(i.RValue, target)}
	case *IndexAssignInst:
		return map[string]any{"op": "index_assign", "target": i.Target, "target_type": lowerType(i.TargetType, target), "index": buildValueDTO(i.Index, target), "value": buildValueDTO(i.Value, target)}
	case *BinaryOpInst:
		return map[string]any{
			"op":       "binary_op",
			"dst":      i.Dst,
			"operator": i.Operator,
			"left":     buildValueDTO(i.Left, target),
			"right":    buildValueDTO(i.Right, target),
			"type":     lowerType(i.Type, target),
		}
	case *UnaryOpInst:
		return map[string]any{
			"op":       "unary_op",
			"dst":      i.Dst,
			"operator": i.Operator,
			"operand":  buildValueDTO(i.Operand, target),
			"type":     lowerType(i.Type, target),
		}
	case *IndexReadInst:
		return map[string]any{
			"op":          "index_read",
			"dst":         i.Dst,
			"source":      buildValueDTO(i.Source, target),
			"source_type": lowerType(i.SourceType, target),
			"index":       buildValueDTO(i.Index, target),
			"type":        lowerType(i.Type, target),
		}
	case *CallInst:
		args := []any{}
		for _, arg := range i.Arguments {
			args = append(args, map[string]any{
				"argument": buildValueDTO(arg.Argument, target),
				"mut":      arg.Mut,
			})
		}
		return map[string]any{
			"op":        "call_inst",
			"dst":       i.Dst,
			"function":  buildValueDTO(i.Function, target),
			"arguments": args,
			"type":      lowerType(i.Type, target),
		}
	case *StructInitInst:
		m := map[string]any{
			"op":          "struct_init",
			"dst":         i.Dst,
			"field_name":  i.FieldName,
			"field_index": i.FieldIndex,
			"value":       buildValueDTO(i.Value, target),
		}
		if len(i.VariantLayout) > 0 {
			layoutList := make([]any, len(i.VariantLayout))
			for idx, ty := range i.VariantLayout {
				layoutList[idx] = lowerType(ty, target)
			}
			m["variant_layout"] = layoutList
		}
		return m
	case *FieldReadInst:
		m := map[string]any{
			"op":          "field_read",
			"dst":         i.Dst,
			"object":      buildValueDTO(i.Object, target),
			"field_name":  i.FieldName,
			"field_index": i.FieldIndex,
			"type":        lowerType(i.Type, target),
		}
		if len(i.VariantLayout) > 0 {
			layoutList := make([]any, len(i.VariantLayout))
			for idx, ty := range i.VariantLayout {
				layoutList[idx] = lowerType(ty, target)
			}
			m["variant_layout"] = layoutList
		}
		return m
	case *ArrayInitInst:
		return map[string]any{
			"op":    "array_init",
			"dst":   i.Dst,
			"index": i.Index,
			"value": buildValueDTO(i.Value, target),
		}
	case *SliceInst:
		var lowDTO, highDTO any
		if i.Low != nil {
			lowDTO = buildValueDTO(i.Low, target)
		}
		if i.High != nil {
			highDTO = buildValueDTO(i.High, target)
		}
		return map[string]any{
			"op":             "slice_read",
			"dst":            i.Dst,
			"left":           buildValueDTO(i.Left, target),
			"container_type": lowerType(i.ContainerType, target),
			"low":            lowDTO,
			"high":           highDTO,
			"result_type":    lowerType(i.ResultType, target),
		}
	case *VariantInitInst:
		payloads := []any{}
		for _, p := range i.Payloads {
			payloads = append(payloads, buildValueDTO(p, target))
		}
		return map[string]any{
			"op":           "variant_init",
			"dst":          i.Dst,
			"variant_name": i.VariantName,
			"discriminant": i.Discriminant,
			"payloads":     payloads,
			"type":         lowerType(i.Type, target),
		}
	case *VariantReadInst:
		return map[string]any{
			"op":            "variant_read",
			"dst":           i.Dst,
			"object":        buildValueDTO(i.Object, target),
			"variant_name":  i.VariantName,
			"payload_index": i.PayloadIndex,
			"type":          lowerType(i.Type, target),
		}
	case *VariantDiscriminantInst:
		return map[string]any{
			"op":     "variant_discriminant",
			"dst":    i.Dst,
			"object": buildValueDTO(i.Object, target),
			"type":   lowerType(i.Type, target),
		}
	case *CopyInst:
		return map[string]any{"op": "copy", "dst": i.Dst, "src": i.Src}
	case *MoveInst:
		return map[string]any{"op": "move", "dst": i.Dst, "src": i.Src}
	case *RefAllocInst:
		return map[string]any{"op": "ref_alloc", "dst": i.Dst, "type": lowerType(i.Type, target)}
	case *RefIncInst:
		return map[string]any{"op": "ref_inc", "src": i.Src}
	case *RefDecInst:
		return map[string]any{"op": "ref_dec", "src": i.Src}
	case *MutBorrowInst:
		return map[string]any{"op": "mut_borrow", "src": i.Src}
	case *CastInst:
		return map[string]any{
			"op":   "cast",
			"dst":  i.Dst,
			"src":  buildValueDTO(i.Src, target),
			"type": lowerType(i.Type, target),
		}
	case *LoadPtrInst:
		return map[string]any{
			"op":   "load_ptr",
			"dst":  i.Dst,
			"ptr":  buildValueDTO(i.Ptr, target),
			"type": lowerType(i.Type, target),
		}
	case *StoreInst:
		return map[string]any{
			"op":      "store",
			"dst_ptr": i.DstPtr,
			"value":   buildValueDTO(i.Value, target),
			"type":    lowerType(i.Type, target),
		}
	case *CoroPrologueInst:
		return map[string]any{"op": "coro_prologue"}
	default:
		return map[string]any{"op": "unknown_inst"}
	}
}

func buildTerminatorDTO(term Terminator, target *layout.Target) map[string]any {
	switch t := term.(type) {
	case *ReturnTerminator:
		m := map[string]any{"op": "ret"}
		if t.Value != nil {
			m["value"] = buildValueDTO(t.Value, target)
		}
		return m
	case *JumpTerminator:
		return map[string]any{"op": "br", "target": fmt.Sprintf("%d", t.Target)}
	case *BranchTerminator:
		return map[string]any{
			"op":           "cond_br",
			"condition":    buildValueDTO(t.Condition, target),
			"true_target":  fmt.Sprintf("%d", t.TrueTarget),
			"false_target": fmt.Sprintf("%d", t.FalseTarget),
		}
	case *UnreachableTerminator:
		return map[string]any{"op": "unreachable"}
	case *CoroSuspendTerminator:
		return map[string]any{
			"op":      "coro_suspend",
			"resume":  fmt.Sprintf("%d", t.ResumeBlock),
			"cleanup": fmt.Sprintf("%d", t.CleanupBlock),
			"suspend": fmt.Sprintf("%d", t.SuspendBlock),
		}
	default:
		return map[string]any{"op": "unknown_term"}
	}
}

// =============================================================================
// Value & Type Lowering
// =============================================================================

// buildValueDTO serializes the native MIR values for the backend.
func buildValueDTO(val Value, target *layout.Target) any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case *Register:
		return map[string]any{"op": "ident", "name": v.Name, "type": lowerType(v.Type, target)}
	case *IntConstant:
		return map[string]any{"op": "const_int", "value": v.Value, "type": lowerType(v.Type, target)}
	case *BoolConstant:
		return map[string]any{"op": "const_bool", "value": v.Value, "type": lowerType(v.Type, target)}
	case *StringConstant:
		return map[string]any{"op": "const_string", "value": v.Value, "type": lowerType(v.Type, target)}
	default:
		panic(fmt.Sprintf("MIR Exporter Error: Illegal unflattened value type reached backend: %T", val))
	}
}

// lowerType handles the critical task of stripping frontend Type interfaces
// and emitting low-level primitive strings (like "i32") or layout objects
func lowerType(t types.Type, target *layout.Target) any {
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
	case types.AnyType:
		return "any"
	case types.UnknownType:
		return "unknown"
	case *types.StructType:
		fields := make([]map[string]any, 0, len(v.Fields))
		for _, f := range v.Fields {
			fields = append(fields, map[string]any{
				"name": f.Name,
				"type": lowerType(f.Type, target),
			})
		}
		return map[string]any{
			"kind":      "struct",
			"name":      v.Name,
			"fields":    fields,
			"is_repr_c": v.IsReprC,
		}
	case *types.SumType:
		variants := make([]map[string]any, 0, len(v.Variants))
		for _, varDef := range v.Variants {
			tupleTypes := make([]any, 0, len(varDef.TupleTypes))
			for _, tt := range varDef.TupleTypes {
				tupleTypes = append(tupleTypes, lowerType(tt, target))
			}
			fields := make([]map[string]any, 0, len(varDef.Fields))
			for _, f := range varDef.Fields {
				fields = append(fields, map[string]any{
					"name": f.Name,
					"type": lowerType(f.Type, target),
				})
			}
			variants = append(variants, map[string]any{
				"name":         varDef.Name,
				"discriminant": varDef.Discriminant,
				"tuple_types":  tupleTypes,
				"fields":       fields,
			})
		}
		return map[string]any{
			"kind":      "sum_type",
			"base_name": v.BaseName,
			"variants":  variants,
		}
	case *types.ArrayType:
		return map[string]any{
			"kind": "array",
			"base": lowerType(v.Base, target),
			"size": v.Size,
		}
	case *types.ViewType:
		return map[string]any{
			"kind": "view",
			"base": lowerType(v.Base, target),
		}
	case *types.VectorType:
		return map[string]any{
			"kind": "vector",
			"base": lowerType(v.Base, target),
		}
	case *types.MapType:
		return map[string]any{
			"kind":  "map",
			"key":   lowerType(v.Key, target),
			"value": lowerType(v.Value, target),
		}
	case *types.FutureType:
		return map[string]any{
			"kind": "future",
			"base": lowerType(v.Base, target),
		}
	case *types.FunctionType:
		return "ptr"
	default:
		if t.IsReferenceType() {
			return "ptr"
		}
		return "void"
	}
}
