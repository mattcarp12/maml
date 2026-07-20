package passes

import (
	"fmt"
	"maps"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

func InjectDrops(g *mir.Graph, locals map[string]types.Type, globalLiveness *LivenessResult) {
	env := buildTypeEnv(g, locals)
	views := buildViewSet(g, locals)

	// Build reverse alias map: src -> []dst (all variables that alias src)
	revAliases := make(map[string][]string)
	for dst, src := range globalLiveness.Aliases {
		revAliases[src] = append(revAliases[src], dst)
	}

	dropCounter := 0
	newDropTemp := func() string {
		dropCounter++
		return fmt.Sprintf("_drop_t%d", dropCounter)
	}

	for _, block := range g.SortedBlocks() {
		stmtLiveness := AnalyzeStatementLiveness(block, globalLiveness.LiveOut[block.ID], globalLiveness.Aliases)
		var newStmts []mir.Instruction

		for i, inst := range block.Statements {
			newStmts = append(newStmts, inst)

			dying := make([]string, 0)
			for v := range stmtLiveness.LiveIn[i] {
				if !stmtLiveness.LiveOut[i][v] && !views[v] {
					// Check if any alias of v is still live; if so, skip dropping v
					if !hasLiveAlias(v, stmtLiveness.LiveOut[i], revAliases) {

						// NEW: Suppress drop if consumed by a CallInst
						consumedByCall := false
						if callInst, ok := inst.(*mir.CallInst); ok {
							for argIdx, argVal := range callInst.Arguments {
								if argReg, isReg := argVal.(*mir.Register); isReg && argReg.Name == v {
									// Ensure we don't go out of bounds if intrinsics lack the array
									if argIdx < len(callInst.ArgConsumed) && callInst.ArgConsumed[argIdx] {
										consumedByCall = true
										break
									}
								}
							}
						}

						if !consumedByCall {
							dying = append(dying, v)
						}
					}
				}
			}

			def := getDef(inst)
			if def != "" && def != "_" && !stmtLiveness.LiveOut[i][def] && !views[def] {
				if !hasLiveAlias(def, stmtLiveness.LiveOut[i], revAliases) {
					dying = append(dying, def)
				}
			}

			for _, v := range dying {
				t := env[v]
				if !needsDrop(t) {
					continue
				}

				if moveInst, ok := inst.(*mir.MoveInst); ok && moveInst.Src == v {
					continue
				}

				if storeInst, ok := inst.(*mir.StoreInst); ok {
					if srcReg, isReg := storeInst.Value.(*mir.Register); isReg && srcReg.Name == v {
						continue
					}
				}

				buildRecursiveDrop(v, t, false, &newStmts, newDropTemp, locals)
			}
		}

		termDying := make([]string, 0)
		for v := range stmtLiveness.TermLiveIn {
			if !stmtLiveness.TermLiveOut[v] && !views[v] {
				if !hasLiveAlias(v, stmtLiveness.TermLiveOut, revAliases) {
					termDying = append(termDying, v)
				}
			}
		}

		for _, v := range termDying {
			t := env[v]
			if !needsDrop(t) {
				continue
			}

			if retTerm, ok := block.Terminator.(*mir.ReturnTerminator); ok && retTerm.Value != nil {
				if reg, isReg := retTerm.Value.(*mir.Register); isReg && reg.Name == v {
					continue
				}
			}

			buildRecursiveDrop(v, t, false, &newStmts, newDropTemp, locals)
		}

		block.Statements = newStmts
	}
}

// hasLiveAlias checks if any alias (direct or transitive) of `v` is present in `liveSet`.
// It uses the reverse alias map to traverse the alias graph.
func hasLiveAlias(v string, liveSet map[string]bool, revAliases map[string][]string) bool {
	visited := make(map[string]bool)
	var dfs func(string) bool
	dfs = func(current string) bool {
		if visited[current] {
			return false
		}
		visited[current] = true
		// Check if any alias of current is in liveSet
		for _, alias := range revAliases[current] {
			if liveSet[alias] {
				return true
			}
			// Recurse to transitive aliases (e.g., a -> b -> c)
			if dfs(alias) {
				return true
			}
		}
		return false
	}
	return dfs(v)
}

func buildRecursiveDrop(
	vName string,
	t types.Type,
	isAddr bool,
	stmts *[]mir.Instruction,
	newTemp func() string,
	locals map[string]types.Type,
) {
	if !needsDrop(t) {
		return
	}

	if st, isStruct := t.(*types.StructType); isStruct {
		for i, field := range st.Fields {
			if needsDrop(field.Type) {
				tmpPtr := newTemp()
				locals[tmpPtr] = types.PtrType{} // Register the new temporary

				*stmts = append(*stmts, &mir.FieldAddrInst{
					Dst:        tmpPtr,
					Object:     &mir.Register{Name: vName, Type: t},
					ObjectType: t,
					FieldName:  field.Name,
					FieldIndex: i,
					FieldType:  field.Type,
				})
				buildRecursiveDrop(tmpPtr, field.Type, true, stmts, newTemp, locals)
			}
		}
		return
	}

	if arr, isArray := t.(*types.ArrayType); isArray {
		for i := 0; i < arr.Size; i++ {
			tmpPtr := newTemp()
			locals[tmpPtr] = types.PtrType{} // Register the new temporary

			*stmts = append(*stmts, &mir.IndexAddrInst{
				Dst:        tmpPtr,
				Source:     &mir.Register{Name: vName, Type: t},
				SourceType: t,
				Index:      &mir.IntConstant{Value: int64(i), Type: types.I64Type{}},
				Type:       arr.Base,
			})
			buildRecursiveDrop(tmpPtr, arr.Base, true, stmts, newTemp, locals)
		}
		return
	}

	symbol := lookupDestructorSymbol(t)
	if symbol != "" {
		var callArgName string

		if isAddr {
			callArgName = vName
		} else {
			// We have the value itself – take its address.
			ptrTmp := newTemp()
			locals[ptrTmp] = types.PtrType{}
			*stmts = append(*stmts, &mir.BorrowInst{
				Dst:   ptrTmp,
				Src:   vName,
				IsMut: false,
			})
			callArgName = ptrTmp
		}

		*stmts = append(*stmts, &mir.CallInst{
			Dst:       "_",
			Function:  &mir.Register{Name: symbol, Type: types.PtrType{}},
			Arguments: []mir.Value{&mir.Register{Name: callArgName, Type: types.PtrType{}}},
			Type:      types.UnitType{},
		})
	}

}

func lookupDestructorSymbol(t types.Type) string {
	switch t.(type) {
	case *types.VectorType:
		return "maml_vec_free"
	case *types.MapType:
		return "maml_map_free"
	case *types.FutureType:
		return "maml_task_release"
	case types.StringType:
		return "maml_str_free"
	default:
		return "maml_free"
	}
}

func buildViewSet(g *mir.Graph, locals map[string]types.Type) map[string]bool {
	views := make(map[string]bool)
	addrIsDerived := make(map[string]bool)

	// 1. Any local variable that is a pointer type is fundamentally a borrow/reference.
	// This naturally covers function parameters since they are mapped into locals.
	for name, t := range locals {
		if _, isPtr := t.(types.PtrType); isPtr {
			addrIsDerived[name] = true
		}
	}

	for _, p := range g.Params {
		if _, isPtr := p.Type.(types.PtrType); isPtr {
			addrIsDerived[p.Name] = true
		}
	}

	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			switch i := inst.(type) {
			case *mir.FieldAddrInst:
				addrIsDerived[i.Dst] = true
			case *mir.IndexAddrInst:
				addrIsDerived[i.Dst] = true
			case *mir.BorrowInst:
				addrIsDerived[i.Dst] = true
			case *mir.LoadPtrInst:
				// 2. If we load a concrete struct from a borrowed pointer,
				// that local struct is just a view and does NOT own the memory.
				if ptrReg, ok := i.Ptr.(*mir.Register); ok && addrIsDerived[ptrReg.Name] {
					views[i.Dst] = true
				}
			}
		}
	}

	return views
}

func buildTypeEnv(g *mir.Graph, locals map[string]types.Type) map[string]types.Type {
	env := make(map[string]types.Type)
	maps.Copy(env, locals)
	for _, p := range g.Params {
		env[p.Name] = p.Type
	}
	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			if c, ok := inst.(*mir.CastInst); ok {
				env[c.Dst] = c.Type
			}
			if f, ok := inst.(*mir.FieldAddrInst); ok {
				env[f.Dst] = types.PtrType{}
			}
			if idx, ok := inst.(*mir.IndexAddrInst); ok {
				env[idx.Dst] = types.PtrType{}
			}
			if b, ok := inst.(*mir.BitcastPtrInst); ok {
				env[b.Dst] = b.Type
			}
		}
	}
	return env
}

func needsDrop(t types.Type) bool {
	if t == nil || isPrimitive(t) {
		return false
	}

	switch ty := t.(type) {
	case *types.ViewType:
		return false

	case *types.VectorType, *types.MapType, *types.FutureType, types.StringType:
		return true

	case *types.StructType:
		for _, field := range ty.Fields {
			if needsDrop(field.Type) {
				return true
			}
		}
		return false

	case *types.ArrayType:
		return needsDrop(ty.Base)

	case *types.SumType:
		for _, variant := range ty.Variants {
			for _, field := range variant.Fields {
				if needsDrop(field.Type) {
					return true
				}
			}
			for _, tupleTy := range variant.TupleTypes {
				if needsDrop(tupleTy) {
					return true
				}
			}
		}
		return false

	default:
		return true
	}
}

func isPrimitive(t types.Type) bool {
	switch t.(type) {
	case types.I8Type, types.I16Type, types.I32Type, types.I64Type, types.I128Type,
		types.U8Type, types.U16Type, types.U32Type, types.U64Type, types.U128Type,
		types.F32Type, types.F64Type, types.BoolType, types.CharType, types.UnitType,
		types.PtrType:
		return true
	default:
		return false
	}
}

func getDef(inst mir.Instruction) string {
	switch i := inst.(type) {
	case *mir.AssignInst:
		return i.Dst
	case *mir.CopyInst:
		return i.Dst
	case *mir.MoveInst:
		return i.Dst
	case *mir.BorrowInst:
		return i.Dst
	case *mir.CallInst:
		return i.Dst
	case *mir.FieldAddrInst:
		return i.Dst
	case *mir.BinaryOpInst:
		return i.Dst
	case *mir.UnaryOpInst:
		return i.Dst
	case *mir.CastInst:
		return i.Dst
	case *mir.LoadPtrInst:
		return i.Dst
	case *mir.BitcastPtrInst:
		return i.Dst
	}
	return ""
}
