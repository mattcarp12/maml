package passes

import (
	"github.com/mattcarp12/maml/frontend/layout"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

func LowerTypes(g *mir.Graph) {
	target := layout.DefaultTarget

	typeMap := make(map[string]types.Type)

	for _, p := range g.Params {
		typeMap[p.Name] = p.Type
	}

	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			switch i := inst.(type) {

			case *mir.TempDeclInst:
				typeMap[i.Name] = i.Type

			case *mir.FieldAddrInst: // NEW: The critical addition!
				objType := getTypeOfValue(i.Object)
				if st, ok := objType.(*types.StructType); ok {
					i.FieldOffset = layout.OffsetOf(st, i.FieldIndex, target)
				}

			case *mir.StructInitInst:
				if t, exists := typeMap[i.Dst]; exists {
					if st, ok := t.(*types.StructType); ok {
						i.FieldOffset = layout.OffsetOf(st, i.FieldIndex, target)
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
