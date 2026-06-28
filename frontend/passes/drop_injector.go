package passes

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

// InjectDrops performs deterministic memory management by injecting DropInst
// based on statement-level Non-Lexical Liveness (NLL) analysis.
func InjectDrops(g *mir.Graph, globalLiveness *LivenessResult) {
	env := buildTypeEnv(g)

	dropCounter := 0
	newDropTemp := func() string {
		dropCounter++
		return fmt.Sprintf("_drop_t%d", dropCounter)
	}

	for _, block := range g.SortedBlocks() {
		stmtLiveness := AnalyzeStatementLiveness(block, globalLiveness.LiveOut[block.ID])
		var newStmts []mir.Instruction

		for i, inst := range block.Statements {
			newStmts = append(newStmts, inst)

			dying := make([]string, 0)
			for v := range stmtLiveness.LiveIn[i] {
				if !stmtLiveness.LiveOut[i][v] {
					dying = append(dying, v)
				}
			}

			def := getDef(inst)
			if def != "" && def != "_" && !stmtLiveness.LiveOut[i][def] {
				dying = append(dying, def)
			}

			// 3. Inject drops safely
			for _, v := range dying {
				t := env[v]
				if !needsDrop(t) {
					continue
				}

				if moveInst, ok := inst.(*mir.MoveInst); ok && moveInst.Src == v {
					continue
				}

				// Generate precise drops for inner heap fields
				buildRecursiveDrop(v, t, &newStmts, newDropTemp)
			}
		}

		// 4. Handle variables that survive the statements but die at the Terminator
		termDying := make([]string, 0)
		for v := range stmtLiveness.TermLiveIn {
			if !stmtLiveness.TermLiveOut[v] {
				termDying = append(termDying, v)
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

			buildRecursiveDrop(v, t, &newStmts, newDropTemp)
		}

		block.Statements = newStmts
	}
}

// buildRecursiveDrop walks complex types and emits granular address calculations
// so the C++ backend only ever frees exact memory locations.
func buildRecursiveDrop(vName string, t types.Type, stmts *[]mir.Instruction, newTemp func() string) {
	if !needsDrop(t) {
		return
	}

	// If it is a user-defined struct, we do NOT drop the struct itself.
	// We calculate the address of its heap fields and drop those instead.
	if st, isStruct := t.(*types.StructType); isStruct {
		for i, field := range st.Fields {
			if needsDrop(field.Type) {
				tmpPtr := newTemp()

				*stmts = append(*stmts, &mir.TempDeclInst{Name: tmpPtr, Type: field.Type})
				*stmts = append(*stmts, &mir.FieldAddrInst{
					Dst:        tmpPtr,
					Object:     &mir.Register{Name: vName, Type: t},
					FieldName:  field.Name,
					FieldIndex: i,
					Type:       field.Type,
				})

				// Recursively drop the inner field using the newly calculated pointer
				buildRecursiveDrop(tmpPtr, field.Type, stmts, newTemp)
			}
		}
		return
	}

	// Arrays: Unroll the drops using IndexAddrInst
	// Since these are fixed-size stack arrays, unrolling is safe and avoids CFG splitting!
	if arr, isArray := t.(*types.ArrayType); isArray {
		for i := 0; i < arr.Size; i++ {
			tmpPtr := newTemp()

			// 1. Declare a temporary pointer for the specific element
			*stmts = append(*stmts, &mir.TempDeclInst{Name: tmpPtr, Type: arr.Base})

			// 2. Get the exact memory address of arr[i]
			// Notice how we can just pass an IntConstant directly as the Index value!
			*stmts = append(*stmts, &mir.IndexAddrInst{
				Dst:        tmpPtr,
				Source:     &mir.Register{Name: vName, Type: t},
				SourceType: t,
				Index:      &mir.IntConstant{Value: int64(i), Type: types.I64Type{}},
				Type:       arr.Base,
			})

			// 3. Recursively drop the inner element using the newly calculated pointer!
			buildRecursiveDrop(tmpPtr, arr.Base, stmts, newTemp)
		}
		return
	}

	// Base case: Native heap types (String, Vec, Map, etc.)
	*stmts = append(*stmts, &mir.DropInst{Src: vName})
}

// buildTypeEnv maps every register name to its Type so the injector knows what to free.
func buildTypeEnv(g *mir.Graph) map[string]types.Type {
	env := make(map[string]types.Type)
	for _, p := range g.Params {
		env[p.Name] = p.Type
	}
	for _, block := range g.Blocks {
		for _, inst := range block.Statements {
			if t, ok := inst.(*mir.TempDeclInst); ok {
				env[t.Name] = t.Type
			}
			if c, ok := inst.(*mir.CastInst); ok {
				env[c.Dst] = c.Type
			}
			// Let the environment know about FieldAddrInst!
			if f, ok := inst.(*mir.FieldAddrInst); ok {
				env[f.Dst] = f.Type
			}
		}
	}
	return env
}

// needsDrop determines if a value should be explicitly dropped.
func needsDrop(t types.Type) bool {
	if t == nil {
		return false
	}
	if isPrimitive(t) {
		return false
	}
	// The compiler never inserts free() for Views
	if _, isView := t.(*types.ViewType); isView {
		return false
	}

	// Recursively check structs
	if st, isStruct := t.(*types.StructType); isStruct {
		for _, field := range st.Fields {
			if needsDrop(field.Type) {
				return true
			}
		}
		return false
	}

	if arr, isArray := t.(*types.ArrayType); isArray {
		return needsDrop(arr.Base)
	}
	return true
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
	case *mir.VariantReadInst:
		return i.Dst
	case *mir.VariantDiscriminantInst:
		return i.Dst
	case *mir.SliceInst:
		return i.Dst
	case *mir.BinaryOpInst:
		return i.Dst
	case *mir.UnaryOpInst:
		return i.Dst
	case *mir.CastInst:
		return i.Dst
	case *mir.LoadPtrInst:
		return i.Dst
	case *mir.VariantInitInst:
		return i.Dst
	}
	return ""
}
