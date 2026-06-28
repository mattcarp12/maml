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
	IsExtern   bool          `json:"is_extern"`
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
		IsExtern:   fn.IsExtern,
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
// Exporter Mapper Implementation
// =============================================================================

// Ensure Exporter implements the generated MIR Mapper
var _ Mapper[map[string]any] = (*Exporter)(nil)

type Exporter struct {
	target *layout.Target
}

func buildInstructionDTO(inst Instruction, target *layout.Target) map[string]any {
	return MapNode(inst, &Exporter{target: target})
}

func buildTerminatorDTO(term Terminator, target *layout.Target) map[string]any {
	return MapNode(term, &Exporter{target: target})
}

func buildValueDTO(val Value, target *layout.Target) any {
	if val == nil {
		return nil
	}
	return MapNode(val, &Exporter{target: target})
}

// -----------------------------------------------------------------------------
// Value Mappers
// -----------------------------------------------------------------------------
func (e *Exporter) MapRegister(v *Register) map[string]any {
	return map[string]any{"op": "ident", "name": v.Name, "type": lowerType(v.Type, e.target)}
}
func (e *Exporter) MapIntConstant(v *IntConstant) map[string]any {
	return map[string]any{"op": "const_int", "value": v.Value, "type": lowerType(v.Type, e.target)}
}
func (e *Exporter) MapBoolConstant(v *BoolConstant) map[string]any {
	return map[string]any{"op": "const_bool", "value": v.Value, "type": lowerType(v.Type, e.target)}
}
func (e *Exporter) MapStringConstant(v *StringConstant) map[string]any {
	return map[string]any{"op": "const_string", "value": v.Value, "type": lowerType(v.Type, e.target)}
}

// -----------------------------------------------------------------------------
// Instruction Mappers
// -----------------------------------------------------------------------------
func (e *Exporter) MapTempDeclInst(i *TempDeclInst) map[string]any {
	return map[string]any{"op": "temp_decl", "name": i.Name, "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapAssignInst(i *AssignInst) map[string]any {
	return map[string]any{"op": "assign", "dst": i.Dst, "value": buildValueDTO(i.RValue, e.target)}
}
func (e *Exporter) MapBinaryOpInst(i *BinaryOpInst) map[string]any {
	return map[string]any{"op": "binary_op", "dst": i.Dst, "operator": i.Operator, "left": buildValueDTO(i.Left, e.target), "right": buildValueDTO(i.Right, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapUnaryOpInst(i *UnaryOpInst) map[string]any {
	return map[string]any{"op": "unary_op", "dst": i.Dst, "operator": i.Operator, "operand": buildValueDTO(i.Operand, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapCallInst(i *CallInst) map[string]any {
	args := []any{}
	for _, arg := range i.Arguments {
		args = append(args, buildValueDTO(arg, e.target))
	}
	return map[string]any{"op": "call_inst", "dst": i.Dst, "function": buildValueDTO(i.Function, e.target), "arguments": args, "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapDropInst(i *DropInst) map[string]any {
	return map[string]any{
		"op":  "drop",
		"src": i.Src,
	}
}
func (e *Exporter) MapSliceInst(i *SliceInst) map[string]any {
	var lowDTO, highDTO any
	if i.Low != nil {
		lowDTO = buildValueDTO(i.Low, e.target)
	}
	if i.High != nil {
		highDTO = buildValueDTO(i.High, e.target)
	}
	return map[string]any{"op": "slice_read", "dst": i.Dst, "left": buildValueDTO(i.Left, e.target), "container_type": lowerType(i.ContainerType, e.target), "low": lowDTO, "high": highDTO, "result_type": lowerType(i.ResultType, e.target)}
}
func (e *Exporter) MapStructInitInst(i *StructInitInst) map[string]any {
	m := map[string]any{"op": "struct_init", "dst": i.Dst, "field_name": i.FieldName, "field_index": i.FieldIndex, "value": buildValueDTO(i.Value, e.target)}
	if len(i.VariantLayout) > 0 {
		layoutList := make([]any, len(i.VariantLayout))
		for idx, ty := range i.VariantLayout {
			layoutList[idx] = lowerType(ty, e.target)
		}
		m["variant_layout"] = layoutList
	}
	return m
}
func (e *Exporter) MapFieldAddrInst(i *FieldAddrInst) map[string]any {
	m := map[string]any{
		"op":          "field_addr",
		"dst":         i.Dst,
		"object":      buildValueDTO(i.Object, e.target),
		"field_name":  i.FieldName,
		"field_index": i.FieldIndex,
		"type":        lowerType(i.Type, e.target),
	}

	// Pass along the variant layout if this address calculation involves a sum-type payload
	if len(i.VariantLayout) > 0 {
		layoutList := make([]any, len(i.VariantLayout))
		for idx, ty := range i.VariantLayout {
			layoutList[idx] = lowerType(ty, e.target)
		}
		m["variant_layout"] = layoutList
	}

	return m
}
func (e *Exporter) MapIndexAddrInst(i *IndexAddrInst) map[string]any {
	return map[string]any{
		"op":          "index_addr",
		"dst":         i.Dst,
		"source":      buildValueDTO(i.Source, e.target),
		"source_type": lowerType(i.SourceType, e.target),
		"index":       buildValueDTO(i.Index, e.target),
		"type":        lowerType(i.Type, e.target),
	}
}
func (e *Exporter) MapArrayInitInst(i *ArrayInitInst) map[string]any {
	return map[string]any{"op": "array_init", "dst": i.Dst, "index": i.Index, "value": buildValueDTO(i.Value, e.target)}
}
func (e *Exporter) MapVariantInitInst(i *VariantInitInst) map[string]any {
	payloads := []any{}
	for _, p := range i.Payloads {
		payloads = append(payloads, buildValueDTO(p, e.target))
	}
	return map[string]any{"op": "variant_init", "dst": i.Dst, "variant_name": i.VariantName, "discriminant": i.Discriminant, "payloads": payloads, "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapVariantReadInst(i *VariantReadInst) map[string]any {
	return map[string]any{"op": "variant_read", "dst": i.Dst, "object": buildValueDTO(i.Object, e.target), "variant_name": i.VariantName, "payload_index": i.PayloadIndex, "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapVariantDiscriminantInst(i *VariantDiscriminantInst) map[string]any {
	return map[string]any{"op": "variant_discriminant", "dst": i.Dst, "object": buildValueDTO(i.Object, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapCastInst(i *CastInst) map[string]any {
	return map[string]any{"op": "cast", "dst": i.Dst, "src": buildValueDTO(i.Src, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapLoadPtrInst(i *LoadPtrInst) map[string]any {
	return map[string]any{"op": "load_ptr", "dst": i.Dst, "ptr": buildValueDTO(i.Ptr, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapStoreInst(i *StoreInst) map[string]any {
	return map[string]any{"op": "store", "dst_ptr": i.DstPtr, "value": buildValueDTO(i.Value, e.target), "type": lowerType(i.Type, e.target)}
}
func (e *Exporter) MapCopyInst(i *CopyInst) map[string]any {
	return map[string]any{"op": "copy", "dst": i.Dst, "src": i.Src}
}
func (e *Exporter) MapMoveInst(i *MoveInst) map[string]any {
	return map[string]any{"op": "move", "dst": i.Dst, "src": i.Src}
}
func (e *Exporter) MapBorrowInst(i *BorrowInst) map[string]any {
	return map[string]any{"op": "borrow", "dst": i.Dst, "src": i.Src, "mut": i.IsMut}
}
func (e *Exporter) MapKeepAliveInst(i *KeepAliveInst) map[string]any {
	return map[string]any{"op": "keep_alive", "src": i.Src}
}
func (e *Exporter) MapCoroPrologueInst(i *CoroPrologueInst) map[string]any {
	return map[string]any{"op": "coro_prologue"}
}

// -----------------------------------------------------------------------------
// Terminator Mappers
// -----------------------------------------------------------------------------
func (e *Exporter) MapReturnTerminator(t *ReturnTerminator) map[string]any {
	m := map[string]any{"op": "ret"}
	if t.Value != nil {
		m["value"] = buildValueDTO(t.Value, e.target)
	}
	return m
}
func (e *Exporter) MapJumpTerminator(t *JumpTerminator) map[string]any {
	return map[string]any{"op": "br", "target": fmt.Sprintf("%d", t.Target)}
}
func (e *Exporter) MapBranchTerminator(t *BranchTerminator) map[string]any {
	return map[string]any{"op": "cond_br", "condition": buildValueDTO(t.Condition, e.target), "true_target": fmt.Sprintf("%d", t.TrueTarget), "false_target": fmt.Sprintf("%d", t.FalseTarget)}
}
func (e *Exporter) MapUnreachableTerminator(t *UnreachableTerminator) map[string]any {
	return map[string]any{"op": "unreachable"}
}
func (e *Exporter) MapCoroSuspendTerminator(t *CoroSuspendTerminator) map[string]any {
	return map[string]any{"op": "coro_suspend", "resume_block": fmt.Sprintf("%d", t.ResumeBlock), "cleanup_block": fmt.Sprintf("%d", t.CleanupBlock), "suspend_block": fmt.Sprintf("%d", t.SuspendBlock)}
}
func (e *Exporter) MapCoroYieldTerminator(t *CoroYieldTerminator) map[string]any {
	return map[string]any{"op": "coro_yield"}
}

func lowerType(t types.Type, target *layout.Target) any {
	if t == nil {
		return "void"
	}
	switch v := t.(type) {
	case types.I8Type:
		return "i8"
	case types.I16Type:
		return "i16"
	case types.I32Type:
		return "i32"
	case types.I64Type:
		return "i64"
	case types.U8Type:
		return "u8"
	case types.U16Type:
		return "u16"
	case types.U32Type:
		return "u32"
	case types.U64Type:
		return "u64"
	case types.U128Type:
		return "u128"
	case types.F32Type:
		return "f32"
	case types.F64Type:
		return "f64"
	case types.BoolType:
		return "i1"
	case types.UnitType:
		return "void"
	case types.StringType:
		return "string"
	case types.AnyType:
		return "any"
	case types.PtrType:
		return "ptr"
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
