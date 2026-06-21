package passes

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// LowerLinearTypes desugars linear type instructions (Own, Freeze, MutBorrow) 
// into standard assignments and field reads, and scrubs metadata instructions (KeepAlive).
// IMPORTANT: This pass must run AFTER InjectARC and BEFORE LowerTypes.
func LowerLinearTypes(g *mir.Graph) {
	tempCount := 0
	newTemp := func(t types.Type) string {
		tempCount++
		return fmt.Sprintf("_unroll_t%d", tempCount)
	}

	// We maintain a type map to resolve register types for MutBorrow and nested paths
	typeMap := make(map[string]types.Type)

	for _, block := range g.Blocks {
		var newStmts []mir.Instruction

		for _, inst := range block.Statements {
			// 1. Track types as they are declared
			switch i := inst.(type) {
			case *mir.TempDeclInst:
				typeMap[i.Name] = i.Type
			case *mir.RefAllocInst:
				typeMap[i.Dst] = i.Type
			}

			// 2. Desugar or Scrub
			switch i := inst.(type) {
			case *mir.OwnInst:
				newStmts = append(newStmts, unrollPath(i.Dst, i.Root, i.Path, i.Type, typeMap, newTemp)...)

			case *mir.FreezeInst:
				newStmts = append(newStmts, unrollPath(i.Dst, i.Root, i.Path, i.Type, typeMap, newTemp)...)

			case *mir.MutBorrowInst:
				// Lower mutable borrows to a standard assignment.
				// In LLVM, aliasing a pointer is just copying the pointer value.
				srcType := typeMap[i.Src]
				if srcType == nil {
					srcType = types.UnknownType{}
				}
				newStmts = append(newStmts, &mir.AssignInst{
					Dst:    i.Dst,
					RValue: &mir.Register{Name: i.Src, Type: srcType},
				})

			case *mir.KeepAliveInst:
				// SCRUBBED: This instruction only exists to keep variables alive 
				// during Liveness/ARC analysis. The backend doesn't need it.
				continue

			default:
				// Keep all other instructions intact
				newStmts = append(newStmts, inst)
			}
		}
		block.Statements = newStmts
	}
}

// unrollPath converts a path-based access into a linear sequence of FieldReadInsts.
func unrollPath(dst string, root mir.Value, path []string, finalType types.Type, typeMap map[string]types.Type, newTemp func(types.Type) string) []mir.Instruction {
	var stmts []mir.Instruction

	// Base case: No path, just a direct move/freeze of the root
	if len(path) == 0 {
		stmts = append(stmts, &mir.AssignInst{
			Dst:    dst,
			RValue: root,
		})
		return stmts
	}

	currentVal := root
	var currentType types.Type

	// Extract base type of the root
	if reg, ok := root.(*mir.Register); ok {
		currentType = reg.Type
		if currentType == nil {
			currentType = typeMap[reg.Name]
		}
	}

	// Traverse the struct path
	for i, fieldName := range path {
		st, ok := currentType.(*types.StructType)
		if !ok {
			// Failsafe: if type resolution fails, abort the unroll loop
			break
		}

		fieldIndex := st.GetFieldIndex(fieldName)
		var fieldType types.Type
		if fieldIndex >= 0 && fieldIndex < len(st.Fields) {
			fieldType = st.Fields[fieldIndex].Type
		} else {
			fieldType = types.UnknownType{}
		}

		var targetName string
		if i == len(path)-1 {
			// The final step in the path assigns directly to the original Dst register
			targetName = dst
			fieldType = finalType 
		} else {
			// Intermediate steps need fresh temporary registers declared
			targetName = newTemp(fieldType)
			stmts = append(stmts, &mir.TempDeclInst{
				Name: targetName,
				Type: fieldType,
			})
			typeMap[targetName] = fieldType
		}

		stmts = append(stmts, &mir.FieldReadInst{
			Dst:        targetName,
			Object:     currentVal,
			FieldName:  fieldName,
			FieldIndex: fieldIndex,
			Type:       fieldType,
		})

		currentVal = &mir.Register{Name: targetName, Type: fieldType}
		currentType = fieldType
	}

	return stmts
}