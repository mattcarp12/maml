package passes

import (
	"github.com/mattcarp12/maml/frontend/layout"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// LowerTypes annotates the MIR with physical byte offsets.
func LowerTypes(g *mir.Graph, escapeMap map[string]EscapeState) {
	target := layout.DefaultTarget

	// Create a map to track temporary register types as we see them
	typeMap := make(map[string]types.Type)

	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			switch i := inst.(type) {

			// 1. Record the type whenever a new temporary is declared
			case *mir.TempDeclInst:
				typeMap[i.Name] = i.Type

			case *mir.FieldReadInst:
				// Safely resolve the Object's type
				objType := getTypeOfValue(i.Object)
				if st, ok := objType.(*types.StructType); ok {
					isEscaped := isValueEscaped(i.Object, escapeMap)
					i.FieldOffset = layout.OffsetOf(st, i.FieldIndex, target, isEscaped)
				}

			case *mir.StructInitInst:
				// 2. Look up the type from our map using the Dst register name
				if t, exists := typeMap[i.Dst]; exists {
					if st, ok := t.(*types.StructType); ok {
						isEscaped := escapeMap[i.Dst] == OnHeap
						i.FieldOffset = layout.OffsetOf(st, i.FieldIndex, target, isEscaped)
					}
				}
			}
		}
	}
}

// getTypeOfValue safely extracts the Type from a MIR Value.
func getTypeOfValue(v mir.Value) types.Type {
	switch val := v.(type) {
	case *mir.Register:
		return val.Type
	case *mir.IntConstant:
		return val.Type
	case *mir.BoolConstant:
		return val.Type
	case *mir.StringConstant:
		return val.Type
	default:
		return nil
	}
}

func isValueEscaped(v mir.Value, escapeMap map[string]EscapeState) bool {
	if reg, ok := v.(*mir.Register); ok {
		return escapeMap[reg.Name] == OnHeap
	}
	return false
}
