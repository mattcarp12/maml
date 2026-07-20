package mir

import (
	"fmt"
	"slices"
	"sort"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

// ==========================================================================
// Graph Definitions
// ==========================================================================

type Graph struct {
	Entry  BlockID
	Blocks map[BlockID]*BasicBlock
	Params []Param
}

type BlockID int

type BasicBlock struct {
	ID         BlockID
	Statements []Instruction
	Terminator Terminator
}

type Param struct {
	Name string
	Type types.Type
}

func NewGraph() *Graph {
	return &Graph{
		Blocks: make(map[BlockID]*BasicBlock),
	}
}

func (g *Graph) SortedBlocks() []*BasicBlock {
	if g == nil || len(g.Blocks) == 0 {
		return nil
	}

	// 1. Extract and sort the keys
	var ids []int
	for id := range g.Blocks {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)

	// 2. Build the deterministic slice of blocks
	var blocks []*BasicBlock
	for _, id := range ids {
		blocks = append(blocks, g.Blocks[BlockID(id)])
	}

	return blocks
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

var _ hir.Lowerer[Value] = (*Builder)(nil)

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
		futReg := "__coro_handle"
		b.locals[futReg] = types.PtrType{}
		b.currentFuture = &Register{Name: futReg, Type: types.PtrType{}}
	}

	b.LowerBlockStmt(fn.Body)

	if b.current != nil && b.current.Terminator == nil {
		if fn.IsAsync {
			suspendBlock := b.newBlock()
			cleanupBlock := b.newBlock()

			b.current.Terminator = &CoroFinalSuspendTerminator{
				Value:        nil,
				SuspendBlock: suspendBlock.ID,
				CleanupBlock: cleanupBlock.ID,
			}
			cleanupBlock.Terminator = &CoroYieldTerminator{}
			suspendBlock.Terminator = &UnreachableTerminator{}
		} else {
			b.current.Terminator = &ReturnTerminator{}
		}
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

func (b *Builder) LowerProgram(n *hir.Program) Value {
	return unitValue
}

func (b *Builder) LowerFnDecl(n *hir.FnDecl) Value {
	return unitValue
}

func (b *Builder) LowerTypeDecl(n *hir.TypeDecl) Value {
	return unitValue
}

// =============================================================================
// Statement Lowerers
// =============================================================================

func (b *Builder) LowerBlockStmt(n *hir.BlockStmt) Value {
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
				return hir.LowerNode(s.Value, b)
			case *hir.ExprStmt:
				return hir.LowerNode(s.Value, b)
			}
		}
		hir.LowerNode(stmt, b)
	}

	return unitValue
}

func (b *Builder) LowerExprStmt(n *hir.ExprStmt) Value {
	hir.LowerNode(n.Value, b)
	return unitValue
}

func (b *Builder) LowerYieldStmt(n *hir.YieldStmt) Value {
	hir.LowerNode(n.Value, b)
	return unitValue
}

func (b *Builder) LowerBreakStmt(n *hir.BreakStmt) Value {
	if len(b.loops) == 0 {
		return unitValue
	}
	activeLoop := b.loops[len(b.loops)-1]
	b.current.Terminator = &JumpTerminator{Target: activeLoop.Exit}
	b.current = nil
	return unitValue
}

func (b *Builder) LowerContinueStmt(n *hir.ContinueStmt) Value {
	if len(b.loops) == 0 {
		return unitValue
	}
	activeLoop := b.loops[len(b.loops)-1]
	b.current.Terminator = &JumpTerminator{Target: activeLoop.Header}
	b.current = nil
	return unitValue
}

func (b *Builder) LowerReturnStmt(n *hir.ReturnStmt) Value {
	var flatRet Value = nil
	if n.Value != nil {
		flatRet = hir.LowerNode(n.Value, b)
		if reg, ok := flatRet.(*Register); ok {
			if _, isU := reg.Type.(types.UnitType); isU {
				flatRet = nil
			}
		}
	}

	if b.currentFuture != nil {
		// Async Function: Emit a final suspend instead of a raw return
		suspendBlock := b.newBlock()
		cleanupBlock := b.newBlock()

		b.current.Terminator = &CoroFinalSuspendTerminator{
			Value:        flatRet,
			SuspendBlock: suspendBlock.ID,
			CleanupBlock: cleanupBlock.ID,
		}
		cleanupBlock.Terminator = &CoroYieldTerminator{}
		suspendBlock.Terminator = &UnreachableTerminator{}
	} else {
		// Standard Function
		b.current.Terminator = &ReturnTerminator{Value: flatRet}
	}

	b.current = nil
	return unitValue
}

func (b *Builder) LowerLoopStmt(n *hir.LoopStmt) Value {
	condBlock := b.newBlock()
	bodyBlock := b.newBlock()
	postBlock := b.newBlock()
	exitBlock := b.newBlock()

	b.current.Terminator = &JumpTerminator{Target: condBlock.ID}

	var flatCond Value
	b.current = condBlock
	if n.Condition != nil {
		flatCond = hir.LowerNode(n.Condition, b)
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
	b.LowerBlockStmt(n.Body)
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &JumpTerminator{Target: postBlock.ID}
	}

	b.current = postBlock
	if n.Post != nil {
		hir.LowerNode(n.Post, b)
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &JumpTerminator{Target: condBlock.ID}
	}

	b.loops = b.loops[:len(b.loops)-1]

	b.current = exitBlock
	return unitValue
}

func (b *Builder) LowerDeclareStmt(n *hir.DeclareStmt) Value {
	flatRHS := hir.LowerNode(n.Value, b)
	uniqueName := b.getSymbolName(n.Symbol)
	var t types.Type = types.UnknownType{}
	if n.Symbol != nil {
		t = n.Symbol.Type
	}
	b.locals[uniqueName] = t
	b.emitTransfer(uniqueName, flatRHS)
	return unitValue
}

func (b *Builder) LowerAliasDecl(n *hir.AliasDecl) Value {
	flatSrc := hir.LowerNode(n.Value, b)
	aliasName := b.getSymbolName(n.Symbol)
	b.locals[aliasName] = n.Symbol.Type
	srcReg, ok := flatSrc.(*Register)
	if !ok {
		panic("compiler error: cannot take alias of non-register value")
	}
	b.emitCapTransfer(aliasName, srcReg.Name, n.Symbol.Cap)
	return unitValue
}

func (b *Builder) LowerAssignStmt(n *hir.AssignStmt) Value {
	if n == nil || n.LValue == nil || n.RValue == nil {
		return unitValue
	}

	if fa, ok := n.LValue.(*hir.FieldAccess); ok {
		ptrVal := b.addressOf(fa)

		var writeVal Value
		if n.Operator != "" {
			writeVal = b.emitCompoundMath(ptrVal, n.Operator, n.RValue, fa.Type)
		} else {
			writeVal = hir.LowerNode(n.RValue, b)
		}

		b.push(&StoreInst{DstPtr: ptrVal, Value: writeVal, Type: fa.Type})
		return unitValue
	}

	if idx, ok := n.LValue.(*hir.IndexExpr); ok {
		ptrVal := b.addressOf(idx)

		elemType := hir.TypeOf(n.LValue)
		var writeVal Value

		if n.Operator != "" {
			writeVal = b.emitCompoundMath(ptrVal, n.Operator, n.RValue, elemType)
		} else {
			writeVal = hir.LowerNode(n.RValue, b)
		}

		b.push(&StoreInst{DstPtr: ptrVal, Value: writeVal, Type: elemType})
		return unitValue
	}

	if ident, ok := n.LValue.(*hir.Identifier); ok {
		dstName := b.getSymbolName(ident.Symbol)

		// IMPLICIT STORE: If the local is a pointer, mutate the underlying memory.
		if _, isPtr := b.locals[dstName].(types.PtrType); isPtr {
			ptrReg := &Register{Name: dstName, Type: types.PtrType{}}
			var writeVal Value

			if n.Operator != "" {
				writeVal = b.emitCompoundMath(ptrReg, n.Operator, n.RValue, ident.Type)
			} else {
				writeVal = hir.LowerNode(n.RValue, b)
			}

			// NEW: Pass ptrReg (Value) instead of dstName (string)
			b.push(&StoreInst{DstPtr: ptrReg, Value: writeVal, Type: ident.Type})
			return unitValue
		}

		// Standard assignment fallback for value types
		b.locals[dstName] = ident.Type
		var writeVal Value
		if n.Operator != "" {
			flatLHS := hir.LowerNode(n.LValue, b)
			flatRHS := hir.LowerNode(n.RValue, b)

			opTmp := b.newTemp()
			writeVal = b.emit(&BinaryOpInst{
				Dst:      opTmp,
				Operator: n.Operator,
				Left:     flatLHS,
				Right:    flatRHS,
				Type:     ident.Type,
			}, opTmp, ident.Type)
		} else {
			writeVal = hir.LowerNode(n.RValue, b)
		}

		b.emitTransfer(dstName, writeVal)
		return unitValue
	}

	flatLHS := hir.LowerNode(n.LValue, b)
	flatRHS := hir.LowerNode(n.RValue, b)
	if reg, ok := flatLHS.(*Register); ok {
		b.push(&AssignInst{Dst: reg.Name, RValue: flatRHS})
	}
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
// Core Emission Primitives
//
// Every instruction that enters the current block should be built through one
// of the helpers below rather than appending to b.current.Statements by hand.
// This keeps the "shape" of MIR construction (allocate a temp, register its
// type, append the instruction, return a Register) in one place instead of
// re-implemented at each of the ~30 call sites across flatten.go/vec.go/map.go.
// =============================================================================

// push appends inst to the current block. This is the single place
// b.current.Statements is mutated; every other emit* helper routes through it.
func (b *Builder) push(inst Instruction) {
	b.current.Statements = append(b.current.Statements, inst)
}

// newPtrTemp allocates a fresh temp registered as a pointer local. This is
// the overwhelmingly common case (addresses, borrows, bitcasts) so it gets
// its own helper rather than repeating `newTemp` + `locals[x] = PtrType{}`.
func (b *Builder) newPtrTemp() string {
	tmp := b.newTemp()
	b.locals[tmp] = types.PtrType{}
	return tmp
}

func (b *Builder) emitTemp(t types.Type) string {
	tmp := b.newTemp()
	b.locals[tmp] = t
	return tmp
}

// emit appends inst, records dst's type, and returns a Register referring
// to dst. Use this for any instruction that produces a value.
func (b *Builder) emit(inst Instruction, dst string, t types.Type) Value {
	b.locals[dst] = t
	b.push(inst)
	return &Register{Name: dst, Type: t}
}

// emitLoad loads the value at ptr into a fresh temp of type t.
func (b *Builder) emitLoad(ptr Value, t types.Type) Value {
	tmp := b.emitTemp(t)
	return b.emit(&LoadPtrInst{Dst: tmp, Ptr: ptr, Type: t}, tmp, t)
}

// emitFieldAddr computes the address of a named field on obj and returns a
// pointer Value. This is the single place FieldAddrInst is constructed;
// storeField/loadField build on it for the common read/write cases.
func (b *Builder) emitFieldAddr(obj Value, objType types.Type, fieldName string, fieldIdx int, fieldType types.Type) Value {
	addr := b.newPtrTemp()
	b.push(&FieldAddrInst{
		Dst:        addr,
		Object:     obj,
		ObjectType: objType,
		FieldName:  fieldName,
		FieldIndex: fieldIdx,
		FieldType:  fieldType,
	})
	return &Register{Name: addr, Type: types.PtrType{}}
}

// storeField writes val into the named field of obj.
func (b *Builder) storeField(obj Value, objType types.Type, val Value, fieldName string, fieldIdx int, fieldType types.Type) {
	addr := b.emitFieldAddr(obj, objType, fieldName, fieldIdx, fieldType)
	b.push(&StoreInst{DstPtr: addr, Value: val, Type: fieldType})
}

// loadField reads the named field of obj.
func (b *Builder) loadField(obj Value, objType types.Type, fieldName string, fieldIdx int, fieldType types.Type) Value {
	addr := b.emitFieldAddr(obj, objType, fieldName, fieldIdx, fieldType)
	return b.emitLoad(addr, fieldType)
}

// emitBorrow takes a borrow of src (mutable or read-only) and returns a
// pointer Value to it.
func (b *Builder) emitBorrow(src string, isMut bool) Value {
	dst := b.newPtrTemp()
	b.push(&BorrowInst{Dst: dst, Src: src, IsMut: isMut})
	return &Register{Name: dst, Type: types.PtrType{}}
}

// =============================================================================
// Utilities
// =============================================================================

func ownsHeapMemory(t types.Type) bool {
	if t == nil {
		return false
	}
	switch v := t.(type) {
	case *types.VectorType, *types.MapType, types.StringType, *types.StringType:
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
	if _, isUnit := b.locals[dst].(types.UnitType); isUnit {
		return
	}
	if reg, isReg := val.(*Register); isReg && reg != nil {
		if reg.Type != nil && ownsHeapMemory(reg.Type) {
			b.push(&MoveInst{Dst: dst, Src: reg.Name})
		} else {
			b.push(&CopyInst{Dst: dst, Src: reg.Name})
		}
		return
	}
	b.push(&AssignInst{Dst: dst, RValue: val})
}

func (b *Builder) emitCapTransfer(dst, src string, cap any) {
	dstType, ok := b.locals[dst]
	if !ok {
		panic("compiler error: emitCapTransfer called before local type was registered")
	}
	_, isPtr := dstType.(types.PtrType)

	switch cap {
	case types.CapMut:
		b.push(&BorrowInst{Dst: dst, Src: src, IsMut: true})
	case types.CapRo:
		if isPtr {
			b.push(&BorrowInst{Dst: dst, Src: src, IsMut: false})
		} else {
			b.push(&CopyInst{Dst: dst, Src: src})
		}
	case types.CapOwn:
		b.push(&MoveInst{Dst: dst, Src: src})
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
	readVal := b.emitLoad(ptrVal, elemType)
	flatRHS := hir.LowerNode(rhsExpr, b)

	opTmp := b.newTemp()
	return b.emit(&BinaryOpInst{
		Dst:      opTmp,
		Operator: operator,
		Left:     readVal,
		Right:    flatRHS,
		Type:     elemType,
	}, opTmp, elemType)
}

func lowerParamType(t types.Type, cap types.Cap) types.Type {
	if cap == types.CapMut {
		return types.PtrType{}
	}
	// Pass heap-owning types by reference even for read-only to avoid deep copies / double frees
	if cap == types.CapRo && ownsHeapMemory(t) {
		return types.PtrType{}
	}
	return t
}

// boxScalar allocates a stack temp of type t, stores val into it, and returns
// a pointer to that temp. Runtime ABI calls (maml_vec_push, maml_vec_set,
// maml_map_put, ...) take element data by address, not by value — this is
// the one place that boxing should happen so we don't reimplement it at
// every call site.
func (b *Builder) boxScalar(val Value, t types.Type) Value {
	slot := b.newTemp()
	b.locals[slot] = t
	ptrVal := b.emitBorrow(slot, true)
	b.push(&StoreInst{DstPtr: ptrVal, Value: val, Type: t})
	return ptrVal
}

// emitRuntimeCall builds a CallInst to the runtime function sym with the
// given arguments and return type, appends it, and returns the destination
// Register (or nil for a UnitType return). This is the single implementation
// of the CallInst/append/return boilerplate shared by every generated
// EmitMaml* runtime wrapper in abi_generated.go.
func (b *Builder) emitRuntimeCall(sym string, retType types.Type, args ...Value) Value {
	dst := ""
	if _, isUnit := retType.(types.UnitType); !isUnit {
		dst = b.newTemp()
		b.locals[dst] = retType
	}
	b.push(&CallInst{
		Dst:       dst,
		Function:  &Register{Name: sym, Type: types.UnknownType{}},
		Arguments: args,
		Type:      retType,
	})
	if dst == "" {
		return nil
	}
	return &Register{Name: dst, Type: retType}
}
