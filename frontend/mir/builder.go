package mir

import (
	"fmt"
	"slices"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

// ==========================================================================
// Memory Class Helpers
// ==========================================================================

// isByRefType returns true if the type's memory class is a heap-allocated pointer (by_reference).
func isByRefType(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.(type) {
	case *types.VectorType, *types.MapType, *types.FutureType:
		return true
	default:
		return false
	}
}

// resolveBasePtr injects an explicit dereference instruction if the object type is a by_reference memory class.
func (b *Builder) resolveBasePtr(basePtr Value, objType types.Type) Value {
	if isByRefType(objType) {
		heapPtr := b.newTemp()
		b.locals[heapPtr] = types.PtrType{}
		b.current.Statements = append(b.current.Statements, &LoadPtrInst{
			Dst:  heapPtr,
			Ptr:  basePtr,
			Type: types.PtrType{},
		})
		return &Register{Name: heapPtr, Type: types.PtrType{}}
	}
	return basePtr
}

// ==========================================================================
// MIR Program and Builder
// ==========================================================================

type Function struct {
	Name       string
	Params     []Param
	ReturnType types.Type
	IsAsync    bool
	IsExtern   bool
	Graph      *Graph
	Locals     map[string]types.Type
}

type Program struct {
	TypeDecls []*hir.TypeDecl
	Functions []Function
}

// BuildProgram translates an entire HIR program into a flattened MIR program.
func BuildProgram(hirProg *hir.Program) *Program {
	if hirProg == nil {
		return nil
	}

	mirProg := &Program{
		TypeDecls: make([]*hir.TypeDecl, 0),
		Functions: make([]Function, 0),
	}

	for _, decl := range hirProg.Decls {
		switch d := decl.(type) {
		case *hir.TypeDecl:
			mirProg.TypeDecls = append(mirProg.TypeDecls, d)

		case *hir.FnDecl:
			var graph *Graph = nil
			var locals map[string]types.Type = nil
			if !d.IsExtern {
				graph, locals = buildFn(d)
			}

			params := make([]Param, len(d.Params))
			for i, p := range d.Params {
				params[i] = Param{Name: p.Name, Type: lowerParamType(p.Type, p.Symbol.Cap)}
			}

			// Bundle the CFG with the function's static signature
			mirProg.Functions = append(mirProg.Functions, Function{
				Name:       d.Name,
				Params:     params,
				ReturnType: d.ReturnType,
				IsAsync:    d.IsAsync,
				IsExtern:   d.IsExtern,
				Graph:      graph,
				Locals:     locals,
			})
		}
	}

	return mirProg
}

// unitValue is the sentinel Value used for statement-shaped Mapper methods
// and other nodes that don't produce a meaningful runtime value.
var unitValue Value = &Register{Name: "_unit", Type: types.UnitType{}}

type LoopTracker struct {
	Header BlockID
	Exit   BlockID
}

type Builder struct {
	graph         *Graph
	nextID        BlockID
	loops         []LoopTracker
	tempCount     int
	symNames      map[*types.Symbol]string
	nameFreq      map[string]int
	Target        *Target
	locals        map[string]types.Type
	current       *BasicBlock
	currentFuture Value
}

var _ hir.Mapper[Value] = (*Builder)(nil)

// buildFn translates a hierarchical HIR function into a flat MIR Control Flow Graph.
func buildFn(fn *hir.FnDecl) (*Graph, map[string]types.Type) {
	b := &Builder{
		graph:    NewGraph(),
		symNames: make(map[*types.Symbol]string),
		nameFreq: make(map[string]int),
		locals:   make(map[string]types.Type),
		Target:   DefaultTarget,
	}

	for _, p := range fn.Params {
		if p.Symbol != nil {
			b.nameFreq[p.Name] = 1
			b.symNames[p.Symbol] = p.Name
			b.locals[p.Name] = lowerParamType(p.Type, p.Symbol.Cap)
		}
	}

	entry := b.newBlock()
	b.graph.Entry = entry.ID
	b.current = entry

	if fn.IsAsync {
		entry.Statements = append(entry.Statements, &CoroPrologueInst{})
		futReg := b.newTemp()
		b.locals[futReg] = &types.FutureType{}
		b.currentFuture = &Register{Name: futReg, Type: &types.FutureType{}}
	}

	b.MapBlockStmt(fn.Body)

	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &ReturnTerminator{}
	}

	return b.graph, b.locals
}

func (b *Builder) newBlock() *BasicBlock {
	id := b.nextID
	b.nextID++
	block := &BasicBlock{
		ID:         id,
		Statements: []Instruction{},
	}
	b.graph.Blocks[id] = block
	return block
}

// =============================================================================
// Top-level / Declaration Mappers
// =============================================================================

func (b *Builder) MapProgram(n *hir.Program) Value {
	return unitValue
}

func (b *Builder) MapFnDecl(n *hir.FnDecl) Value {
	return unitValue
}

func (b *Builder) MapTypeDecl(n *hir.TypeDecl) Value {
	return unitValue
}

// =============================================================================
// Statement Mappers
// =============================================================================

func (b *Builder) MapBlockStmt(n *hir.BlockStmt) Value {
	if n == nil || len(n.Statements) == 0 {
		return unitValue
	}

	stmts := n.Statements
	for i, stmt := range stmts {
		if b.current == nil {
			break
		}
		if i == len(stmts)-1 {
			switch s := stmt.(type) {
			case *hir.YieldStmt:
				return hir.MapNode(s.Value, b)
			case *hir.ExprStmt:
				return hir.MapNode(s.Value, b)
			}
		}
		hir.MapNode(stmt, b)
	}

	return unitValue
}

func (b *Builder) MapExprStmt(n *hir.ExprStmt) Value {
	hir.MapNode(n.Value, b)
	return unitValue
}

func (b *Builder) MapYieldStmt(n *hir.YieldStmt) Value {
	hir.MapNode(n.Value, b)
	return unitValue
}

func (b *Builder) MapBreakStmt(n *hir.BreakStmt) Value {
	if len(b.loops) == 0 {
		return unitValue
	}
	activeLoop := b.loops[len(b.loops)-1]
	b.current.Terminator = &JumpTerminator{Target: activeLoop.Exit}
	b.current = nil
	return unitValue
}

func (b *Builder) MapContinueStmt(n *hir.ContinueStmt) Value {
	if len(b.loops) == 0 {
		return unitValue
	}
	activeLoop := b.loops[len(b.loops)-1]
	b.current.Terminator = &JumpTerminator{Target: activeLoop.Header}
	b.current = nil
	return unitValue
}

func (b *Builder) MapReturnStmt(n *hir.ReturnStmt) Value {
	var flatRet Value = nil
	if n.Value != nil {
		flatRet = hir.MapNode(n.Value, b)
		if reg, ok := flatRet.(*Register); ok {
			if _, isU := reg.Type.(types.UnitType); isU {
				flatRet = nil
			}
		}
	}
	b.current.Terminator = &ReturnTerminator{Value: flatRet}
	b.current = nil
	return unitValue
}

func (b *Builder) MapLoopStmt(n *hir.LoopStmt) Value {
	condBlock := b.newBlock()
	bodyBlock := b.newBlock()
	postBlock := b.newBlock()
	exitBlock := b.newBlock()

	b.current.Terminator = &JumpTerminator{Target: condBlock.ID}

	var flatCond Value
	b.current = condBlock
	if n.Condition != nil {
		flatCond = hir.MapNode(n.Condition, b)
	} else {
		flatCond = &BoolConstant{Value: true, Type: types.BoolType{}}
	}
	condEvalBlock := b.current

	condEvalBlock.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  bodyBlock.ID,
		FalseTarget: exitBlock.ID,
	}

	b.loops = append(b.loops, LoopTracker{Header: postBlock.ID, Exit: exitBlock.ID})

	b.current = bodyBlock
	b.MapBlockStmt(n.Body)
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &JumpTerminator{Target: postBlock.ID}
	}

	b.current = postBlock
	if n.Post != nil {
		hir.MapNode(n.Post, b)
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &JumpTerminator{Target: condBlock.ID}
	}

	b.loops = b.loops[:len(b.loops)-1]

	b.current = exitBlock
	return unitValue
}

func (b *Builder) MapDeclareStmt(n *hir.DeclareStmt) Value {
	flatRHS := hir.MapNode(n.Value, b)
	uniqueName := b.getSymbolName(n.Symbol)
	var t types.Type = types.UnknownType{}
	if n.Symbol != nil {
		t = n.Symbol.Type
	}
	b.locals[uniqueName] = t
	b.emitTransfer(uniqueName, flatRHS)
	return unitValue
}

func (b *Builder) MapAliasDecl(n *hir.AliasDecl) Value {
	flatSrc := hir.MapNode(n.Value, b)
	aliasName := b.getSymbolName(n.Symbol)
	b.locals[aliasName] = n.Symbol.Type
	srcReg, ok := flatSrc.(*Register)
	if !ok {
		panic("compiler error: cannot take alias of non-register value")
	}
	b.emitCapTransfer(aliasName, srcReg.Name, n.Symbol.Cap, n.Symbol.Type)
	return unitValue
}

func (b *Builder) MapAssignStmt(n *hir.AssignStmt) Value {
	if n == nil || n.LValue == nil || n.RValue == nil {
		return unitValue
	}

	if fa, ok := n.LValue.(*hir.FieldAccess); ok {
		ptrVal := b.flattenPlace(fa)

		var writeVal Value
		if n.Operator != "" {
			writeVal = b.emitCompoundMath(ptrVal, n.Operator, n.RValue, fa.Type)
		} else {
			writeVal = hir.MapNode(n.RValue, b)
		}

		ptrReg := ptrVal.(*Register)
		b.current.Statements = append(b.current.Statements, &StoreInst{
			DstPtr: ptrReg.Name,
			Value:  writeVal,
			Type:   fa.Type,
		})
		return unitValue
	}

	if idx, ok := n.LValue.(*hir.IndexExpr); ok {
		ptrVal := b.flattenPlace(idx)

		elemType := hir.TypeOf(n.LValue)
		var writeVal Value

		if n.Operator != "" {
			writeVal = b.emitCompoundMath(ptrVal, n.Operator, n.RValue, elemType)
		} else {
			writeVal = hir.MapNode(n.RValue, b)
		}

		ptrReg := ptrVal.(*Register)
		b.current.Statements = append(b.current.Statements, &StoreInst{
			DstPtr: ptrReg.Name,
			Value:  writeVal,
			Type:   elemType,
		})
		return unitValue
	}

	if ident, ok := n.LValue.(*hir.Identifier); ok {
		dstName := b.getSymbolName(ident.Symbol)
		b.locals[dstName] = ident.Type
		var writeVal Value
		if n.Operator != "" {
			flatLHS := hir.MapNode(n.LValue, b)
			flatRHS := hir.MapNode(n.RValue, b)

			opTmp := b.newTemp()
			writeVal = b.emit(&BinaryOpInst{
				Dst:      opTmp,
				Operator: n.Operator,
				Left:     flatLHS,
				Right:    flatRHS,
				Type:     ident.Type,
			}, opTmp, ident.Type)
		} else {
			writeVal = hir.MapNode(n.RValue, b)
		}

		b.emitTransfer(dstName, writeVal)
		return unitValue
	}

	flatLHS := hir.MapNode(n.LValue, b)
	flatRHS := hir.MapNode(n.RValue, b)
	if reg, ok := flatLHS.(*Register); ok {
		b.current.Statements = append(b.current.Statements, &AssignInst{Dst: reg.Name, RValue: flatRHS})
	}
	return unitValue
}

func (b *Builder) MapMapInsertStmt(n *hir.MapInsertStmt) Value {
	if n == nil {
		return unitValue
	}

	flatMapPtr := hir.MapNode(n.Map, b)
	hashVal, ptrVal, lenVal := b.lowerMapKey(n.Key)

	var writeVal Value
	var elemType types.Type
	if n.Operator != "" {
		opaquePtr := b.EmitMamlMapGet(flatMapPtr, hashVal, ptrVal, lenVal)
		elemType = hir.TypeOf(n.Value)
		writeVal = b.emitCompoundMath(opaquePtr, n.Operator, n.Value, elemType)
	} else {
		writeVal = hir.MapNode(n.Value, b)
		elemType = getValueType(writeVal)
	}

	boxedVal := b.boxScalar(writeVal, elemType)
	b.EmitMamlMapPut(flatMapPtr, hashVal, ptrVal, lenVal, boxedVal)
	return unitValue
}

func (b *Builder) MapVecWriteStmt(n *hir.VecWriteStmt) Value {
	if n == nil {
		return unitValue
	}
	flatVecPtr := hir.MapNode(n.Vec, b)
	flatIdx := hir.MapNode(n.Index, b)

	var writeVal Value
	var elemType types.Type
	if n.Operator != "" {
		opaquePtr := b.EmitMamlVecGet(flatVecPtr, flatIdx)
		elemType = hir.TypeOf(n.Value)
		writeVal = b.emitCompoundMath(opaquePtr, n.Operator, n.Value, elemType)
	} else {
		writeVal = hir.MapNode(n.Value, b)
		elemType = getValueType(writeVal)
	}

	boxedVal := b.boxScalar(writeVal, elemType)
	b.EmitMamlVecSet(flatVecPtr, flatIdx, boxedVal)
	return unitValue
}

func (b *Builder) MapVecPushStmt(n *hir.VecPushStmt) Value {
	if n == nil {
		return unitValue
	}
	flatVecPtr := hir.MapNode(n.Vec, b)
	flatVal := hir.MapNode(n.Value, b)
	elemType := getValueType(flatVal)

	slotTmp := b.newTemp()
	b.locals[slotTmp] = elemType
	b.current.Statements = append(b.current.Statements, &StoreInst{
		DstPtr: slotTmp,
		Value:  flatVal,
		Type:   elemType,
	})

	slotPtr := b.newTemp()
	b.locals[slotPtr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &BorrowInst{
		Dst: slotPtr, Src: slotTmp, IsMut: true,
	})

	b.EmitMamlVecPush(flatVecPtr, &Register{Name: slotPtr, Type: types.PtrType{}})
	return unitValue
}

func (b *Builder) getSymbolName(sym *types.Symbol) string {
	if sym == nil {
		return ""
	}
	if name, exists := b.symNames[sym]; exists {
		return name
	}

	count := b.nameFreq[sym.Name]
	b.nameFreq[sym.Name] = count + 1

	var uniqueName string
	if count == 0 {
		uniqueName = sym.Name
	} else {
		uniqueName = fmt.Sprintf("%s_%d", sym.Name, count)
	}

	b.symNames[sym] = uniqueName
	return uniqueName
}

// =============================================================================
// Utilities
// =============================================================================

func (b *Builder) emitTemp(t types.Type) string {
	tmp := b.newTemp()
	b.locals[tmp] = t
	return tmp
}

func (b *Builder) emit(inst Instruction, dst string, t types.Type) Value {
	b.locals[dst] = t
	b.current.Statements = append(b.current.Statements, inst)
	return &Register{Name: dst, Type: t}
}

func ownsHeapMemory(t types.Type) bool {
	if t == nil {
		return false
	}
	switch v := t.(type) {
	case *types.VectorType, *types.MapType:
		return true
	case *types.StructType:
		for _, field := range v.Fields {
			if ownsHeapMemory(field.Type) {
				return true
			}
		}
		return false
	case *types.ArrayType:
		return ownsHeapMemory(v.Base)
	case *types.SumType:
		for _, variant := range v.Variants {
			for _, field := range variant.Fields {
				if ownsHeapMemory(field.Type) {
					return true
				}
			}
			if slices.ContainsFunc(variant.TupleTypes, ownsHeapMemory) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (b *Builder) emitTransfer(dst string, val Value) {
	if reg, isReg := val.(*Register); isReg && reg != nil {
		if reg.Type != nil && (reg.Type.IsReferenceType() || ownsHeapMemory(reg.Type)) {
			b.current.Statements = append(b.current.Statements, &MoveInst{Dst: dst, Src: reg.Name})
		} else {
			b.current.Statements = append(b.current.Statements, &CopyInst{Dst: dst, Src: reg.Name})
		}
		return
	}
	b.current.Statements = append(b.current.Statements, &AssignInst{Dst: dst, RValue: val})
}

func (b *Builder) emitCapTransfer(dst, src string, cap any, t types.Type) {
	switch cap {
	case types.CapMut:
		if isByRefType(t) {
			b.current.Statements = append(b.current.Statements, &CopyInst{Dst: dst, Src: src})
		} else {
			b.current.Statements = append(b.current.Statements, &BorrowInst{Dst: dst, Src: src, IsMut: true})
		}
	case types.CapRo:
		if isByRefType(t) {
			b.current.Statements = append(b.current.Statements, &CopyInst{Dst: dst, Src: src})
		} else {
			b.current.Statements = append(b.current.Statements, &BorrowInst{Dst: dst, Src: src, IsMut: false})
		}
	case types.CapOwn:
		b.current.Statements = append(b.current.Statements, &MoveInst{Dst: dst, Src: src})
	case types.CapCopy:
		b.current.Statements = append(b.current.Statements, &CopyInst{Dst: dst, Src: src})
	}
}

func (b *Builder) emitCoroSuspend() *BasicBlock {
	resumeBlock := b.newBlock()
	cleanupBlock := b.newBlock()
	suspendBlock := b.newBlock()

	b.current.Terminator = &CoroSuspendTerminator{
		ResumeBlock:  resumeBlock.ID,
		CleanupBlock: cleanupBlock.ID,
		SuspendBlock: suspendBlock.ID,
	}
	cleanupBlock.Terminator = &JumpTerminator{Target: suspendBlock.ID}
	suspendBlock.Terminator = &CoroYieldTerminator{}

	return resumeBlock
}

func getValueType(val Value) types.Type {
	switch v := val.(type) {
	case *Register:
		return v.Type
	case *BoolConstant:
		return v.Type
	case *IntConstant:
		return v.Type
	case *StringConstant:
		return v.Type
	default:
		panic(fmt.Sprintf("Type %T does not implement mir.Value interface", val))
	}
}

func (b *Builder) emitCompoundMath(ptrVal Value, operator string, rhsExpr hir.Expr, elemType types.Type) Value {
	readTmp := b.newTemp()
	readVal := b.emit(&LoadPtrInst{
		Dst:  readTmp,
		Ptr:  ptrVal,
		Type: elemType,
	}, readTmp, elemType)

	flatRHS := hir.MapNode(rhsExpr, b)

	opTmp := b.newTemp()
	return b.emit(&BinaryOpInst{
		Dst:      opTmp,
		Operator: operator,
		Left:     readVal,
		Right:    flatRHS,
		Type:     elemType,
	}, opTmp, elemType)
}

// func lowerParamType(t types.Type, cap types.Cap) types.Type {
// 	switch cap {
// 	case types.CapMut, types.CapRo:
// 		if isByRefType(t) {
// 			return t
// 		}
// 		return types.PtrType{}
// 	default:
// 		return t
// 	}
// }

func lowerParamType(t types.Type, cap types.Cap) types.Type {
	switch cap {
	case types.CapMut, types.CapRo:
		return types.PtrType{}
	default:
		return t
	}
}


// boxScalar allocates a stack temp of type t, stores val into it, and returns
// a pointer to that temp. Runtime ABI calls (maml_vec_push, maml_vec_set,
// maml_map_put, ...) take element data by address, not by value — this is
// the one place that boxing should happen so we don't reimplement it at
// every call site.
func (b *Builder) boxScalar(val Value, t types.Type) Value {
	slot := b.newTemp()
	b.locals[slot] = t
	b.current.Statements = append(b.current.Statements, &StoreInst{
		DstPtr: slot,
		Value:  val,
		Type:   t,
	})

	ptr := b.newTemp()
	b.locals[ptr] = types.PtrType{}
	b.current.Statements = append(b.current.Statements, &BorrowInst{
		Dst:   ptr,
		Src:   slot,
		IsMut: false,
	})

	return &Register{Name: ptr, Type: types.PtrType{}}
}
