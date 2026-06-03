package passes

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Optimize performs Scalar Replacement of Aggregates (SROA).
// It returns true if the graph was modified (allowing you to loop optimization passes).
func Optimize(g *mir.Graph) bool {
	if g == nil {
		return false
	}

	// Step 1: Identify all structurally typed temporary declarations as candidates.
	candidates := make(map[string]*types.StructType)
	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			if decl, ok := inst.(*mir.TempDeclInst); ok {
				if st, isStruct := decl.Type.(*types.StructType); isStruct {
					candidates[decl.Name] = st
				}
			}
		}
	}

	if len(candidates) == 0 {
		return false
	}

	// Step 2: The "Cheap" Local Escape Analysis.
	// Disqualify any candidate that is used as a whole aggregate.
	// It is ONLY allowed to appear in StructInitInst (as Dst) or FieldReadInst (as Object).
	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			switch i := inst.(type) {
			case *mir.StructInitInst:
				// Allowed usage: we are mutating a specific field.
				// But we must disqualify if the *value* being assigned is one of our struct candidates!
				disqualifyOperand(i.Value, candidates)
			case *mir.FieldReadInst:
				// Allowed usage: we are reading a specific field.
				// (But disqualify if the destination variable somehow shares the same name as a candidate)
				delete(candidates, i.Dst)
			case *mir.AssignInst:
				delete(candidates, i.Dst)
				disqualifyOperand(i.RValue, candidates)
			case *mir.CallInst:
				delete(candidates, i.Dst)
				for _, arg := range i.Arguments {
					disqualifyOperand(arg.Argument, candidates)
				}
			case *mir.VariantInitInst:
				delete(candidates, i.Dst)
				for _, p := range i.Payloads {
					disqualifyOperand(p, candidates)
				}
			case *mir.CopyInst:
				delete(candidates, i.Dst)
				delete(candidates, i.Src)
			case *mir.MoveInst:
				delete(candidates, i.Dst)
				delete(candidates, i.Src)
			default:
				// For all other instructions, aggressively disqualify to be safe.
				// (You can add specific safe checks for Arrays/Variants later).
			}
		}

		// Check Terminators (Cannot return or branch on an aggregate)
		if ret, ok := block.Terminator.(*mir.ReturnTerminator); ok && ret.Value != nil {
			disqualifyOperand(ret.Value, candidates)
		}
	}

	// If no candidates survived the local escape analysis, exit early.
	if len(candidates) == 0 {
		return false
	}

	// Step 3: Rewrite the Graph!
	// Shred the surviving structs into flattened scalar variables.
	changed := false
	for _, block := range g.Blocks {
		var newStatements []mir.Instruction

		for _, inst := range block.Statements {
			switch i := inst.(type) {

			case *mir.TempDeclInst:
				if st, promotable := candidates[i.Name]; promotable {
					// SHRED: Replace `let pt: Point` with `let pt_x: int`, `let pt_y: int`
					for _, field := range st.Fields {
						newStatements = append(newStatements, &mir.TempDeclInst{
							Name: generateScalarName(i.Name, field.Name),
							Type: field.Type,
						})
					}
					changed = true
					continue
				}

			case *mir.StructInitInst:
				if _, promotable := candidates[i.Dst]; promotable {
					// SHRED: Replace `pt.x = 5` with `pt_x = 5`
					newStatements = append(newStatements, &mir.AssignInst{
						Dst:    generateScalarName(i.Dst, i.FieldName),
						RValue: i.Value,
					})
					changed = true
					continue
				}

			case *mir.FieldReadInst:
				if id, ok := i.Object.(*hir.Identifier); ok {
					if _, promotable := candidates[id.Value]; promotable {
						// SHRED: Replace `v = pt.x` with `v = pt_x`
						scalarName := generateScalarName(id.Value, i.FieldName)

						// Because this is replacing a read, we inject an AssignInst where the
						// RValue is a synthesized identifier pointing to our new scalar variable.
						newStatements = append(newStatements, &mir.AssignInst{
							Dst: i.Dst,
							RValue: &hir.Identifier{
								Value: scalarName,
								Type:  i.Type,
							},
						})
						changed = true
						continue
					}
				}
			}

			// If it wasn't shredded, keep the original instruction
			newStatements = append(newStatements, inst)
		}

		block.Statements = newStatements
	}

	return changed
}

// disqualifyOperand checks if an operand references a struct candidate.
// If it does, that struct is being used as a whole aggregate (escaping local bounds),
// so it must be disqualified from SROA.
func disqualifyOperand(op hir.Operand, candidates map[string]*types.StructType) {
	if op == nil {
		return
	}
	if ident, ok := op.(*hir.Identifier); ok {
		delete(candidates, ident.Value)
	}
}

// generateScalarName deterministically constructs the shredded variable name.
func generateScalarName(structVarName, fieldName string) string {
	return fmt.Sprintf("%s_%s", structVarName, fieldName)
}
