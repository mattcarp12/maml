package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/layout"
	"github.com/mattcarp12/maml/frontend/types"
)

// ==========================================================================
// MIR Program and Builder
// ==========================================================================

type Function struct {
	Name       string
	Params     []*hir.Param
	ReturnType types.Type
	IsAsync    bool
	Graph      *Graph
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
			// Generate the CFG for the function body
			graph := buildFn(d)

			// Bundle the CFG with the function's static signature
			mirProg.Functions = append(mirProg.Functions, Function{
				Name:       d.Name,
				Params:     d.Params,
				ReturnType: d.ReturnType,
				IsAsync:    d.IsAsync,
				Graph:      graph,
			})
		}
	}

	return mirProg
}

// =============================================================================
// State Management
// =============================================================================

type LoopTracker struct {
	Header BlockID
	Exit   BlockID
}

type Builder struct {
	graph     *Graph
	nextID    BlockID
	loops     []LoopTracker
	tempCount int
	symNames  map[*types.Symbol]string
	nameFreq  map[string]int
	Target    *layout.Target
}

// buildFn translates a hierarchical HIR function into a flat MIR Control Flow Graph.
func buildFn(fn *hir.FnDecl) *Graph {
	b := &Builder{
		graph:    NewGraph(),
		symNames: make(map[*types.Symbol]string),
		nameFreq: make(map[string]int),
		Target:   layout.DefaultTarget, // TODO - allow different targets to be passed.
	}

	// Register function parameters so they keep their original names
	// and trigger suffixes only on local variables that try to shadow them.
	for _, p := range fn.Params {
		if p.Symbol != nil {
			b.nameFreq[p.Name] = 1
			b.symNames[p.Symbol] = p.Name
		}
	}

	entry := b.newBlock()
	b.graph.Entry = entry.ID

	// =========================================================================
	// Coroutine Initialization
	// =========================================================================
	if fn.IsAsync {
		entry.Statements = append(entry.Statements, &CoroPrologueInst{})
	}

	// Traverse the AST function body
	finalBlock := b.buildBlockStmt(fn.Body, entry)

	// If the trailing basic block was left completely open/unterminated,
	// inject an implicit ReturnTerminator to seal the block control-flow sequence cleanly.
	if finalBlock != nil && finalBlock.Terminator == nil {
		finalBlock.Terminator = &ReturnTerminator{}
	}

	return b.graph
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
// Traversal and Flattening
// =============================================================================

func (b *Builder) buildBlockStmt(body *hir.BlockStmt, current *BasicBlock) *BasicBlock {
	if body == nil {
		return current
	}

	for _, stmt := range body.Statements {
		if current == nil {
			break // Dead code after terminator
		}
		current = b.buildStmt(stmt, current)
	}

	return current
}

func (b *Builder) buildStmt(stmt hir.Stmt, current *BasicBlock) *BasicBlock {
	switch s := stmt.(type) {
	case *hir.DeclareStmt:
		current = b.buildDeclareStmt(s, current)
		return current
	case *hir.AssignStmt:
		return b.buildAssignStmt(s, current)
	case *hir.BlockStmt:
		return b.buildBlockStmt(s, current)
	case *hir.ExprStmt:
		_, current = b.flattenExpr(s.Value, current)
		return current
	case *hir.ReturnStmt:
		return b.buildReturnStmt(s, current)
	case *hir.YieldStmt:
		_, current = b.flattenExpr(s.Value, current)
		return current
	case *hir.LoopStmt:
		return b.buildLoopStmt(s, current)
	case *hir.BreakStmt:
		if len(b.loops) == 0 {
			return current
		}
		activeLoop := b.loops[len(b.loops)-1]
		current.Terminator = &JumpTerminator{Target: activeLoop.Exit}
		return nil
	case *hir.ContinueStmt:
		if len(b.loops) == 0 {
			return current
		}
		activeLoop := b.loops[len(b.loops)-1]
		current.Terminator = &JumpTerminator{Target: activeLoop.Header}
		return nil
	case *hir.MapInsertStmt:
		return b.buildMapInsertStmt(s, current)
	case *hir.VecWriteStmt:
		return b.buildVecWriteStmt(s, current)
	case *hir.VecPushStmt:
		return b.buildVecPushStmt(s, current)
	}
	return current
}

func (b *Builder) buildReturnStmt(stmt *hir.ReturnStmt, current *BasicBlock) *BasicBlock {
	var flatRet Value = nil
	if stmt.Value != nil {
		flatRet, current = b.flattenExpr(stmt.Value, current)
		if reg, ok := flatRet.(*Register); ok {
			if _, isU := reg.Type.(types.UnitType); isU {
				flatRet = nil
			}
		}
	}
	current.Terminator = &ReturnTerminator{Value: flatRet}
	return nil
}

func (b *Builder) buildLoopStmt(stmt *hir.LoopStmt, current *BasicBlock) *BasicBlock {
	condBlock := b.newBlock()
	bodyBlock := b.newBlock()
	postBlock := b.newBlock() // Dedicated block for the increment step
	exitBlock := b.newBlock()

	// 1. Enter loop by jumping to condition
	current.Terminator = &JumpTerminator{Target: condBlock.ID}

	// 2. Evaluate Condition
	var flatCond Value
	condEvalBlock := condBlock
	if stmt.Condition != nil {
		flatCond, condEvalBlock = b.flattenExpr(stmt.Condition, condBlock)
	} else {
		flatCond = &BoolConstant{Value: true, Type: types.BoolType{}}
	}

	condEvalBlock.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  bodyBlock.ID,
		FalseTarget: exitBlock.ID,
	}

	// 3. Track loop. Header is the 'continue' target (now postBlock!). Exit is the 'break' target.
	b.loops = append(b.loops, LoopTracker{Header: postBlock.ID, Exit: exitBlock.ID})

	// 4. Build Body
	bodyEndBlock := b.buildBlockStmt(stmt.Body, bodyBlock)
	if bodyEndBlock != nil && bodyEndBlock.Terminator == nil {
		bodyEndBlock.Terminator = &JumpTerminator{Target: postBlock.ID}
	}

	// 5. Build Post step (the i++)
	postEndBlock := postBlock
	if stmt.Post != nil {
		postEndBlock = b.buildStmt(stmt.Post, postBlock)
	}
	if postEndBlock != nil && postEndBlock.Terminator == nil {
		postEndBlock.Terminator = &JumpTerminator{Target: condBlock.ID}
	}

	// 6. Pop loop tracker
	b.loops = b.loops[:len(b.loops)-1]

	return exitBlock
}

func (b *Builder) buildDeclareStmt(stmt *hir.DeclareStmt, current *BasicBlock) *BasicBlock {
	flatRHS, current := b.flattenExpr(stmt.Value, current)

	// Get the collision-free unique name for this specific variable instance
	uniqueName := b.getSymbolName(stmt.Symbol)

	b.emitMemoryTransfer(uniqueName, flatRHS, stmt.Symbol, current)
	return current
}

// buildAssignStmt translates a reassignment into explicit memory ops without re-allocating.
func (b *Builder) buildAssignStmt(stmt *hir.AssignStmt, current *BasicBlock) *BasicBlock {
	// Guard against uninitialized or partially empty statements
	if stmt == nil || stmt.LValue == nil || stmt.RValue == nil {
		return current
	}

	// Intercept array/slice index mutations before they are flattened into read evaluations
	if idx, ok := stmt.LValue.(*hir.IndexExpr); ok {
		flatTarget, current := b.flattenExpr(idx.Left, current)
		flatIdx, current := b.flattenExpr(idx.Index, current)
		flatRHS, current := b.flattenExpr(stmt.RValue, current)

		targetName := ""
		var targetType types.Type
		if reg, isReg := flatTarget.(*Register); isReg {
			targetName = reg.Name
			targetType = reg.Type
		} else {
			panic(fmt.Sprintf("Unsupported target for IndexExpr: %T\n", flatTarget))
		}

		current.Statements = append(current.Statements, &IndexAssignInst{
			Target:     targetName,
			TargetType: targetType,
			Index:      flatIdx,
			Value:      flatRHS,
		})
		return current
	}

	flatLHS, current := b.flattenExpr(stmt.LValue, current)
	flatRHS, current := b.flattenExpr(stmt.RValue, current)

	dstName := ""
	var dstType types.Type = types.UnknownType{}

	// 1. Extract the true variable name and its type safely
	if flatLHS != nil {
		if reg, ok := flatLHS.(*Register); ok && reg != nil {
			dstName = reg.Name
			dstType = reg.Type // Preserves exact structural type (Struct, SumType, etc.)
		} else {
			panic(fmt.Sprintf("Unsupported LHS for AssignStmt: %T\n", flatLHS))
		}
	}

	// Failsafe guard if destination evaluation completely dropped out
	if dstName == "" {
		return current
	}

	// 2. ABI Routing: Perform direct transfers
	if reg, isReg := flatRHS.(*Register); isReg && reg != nil {
		if dstType != nil && dstType.IsReferenceType() {
			// Affine transfer of ownership
			current.Statements = append(current.Statements, &MoveInst{Dst: dstName, Src: reg.Name})
		} else {
			// Primitive duplication
			current.Statements = append(current.Statements, &CopyInst{Dst: dstName, Src: reg.Name})
		}
	} else if flatRHS != nil {
		// Pure flat expression evaluation assignment
		current.Statements = append(current.Statements, &AssignInst{Dst: dstName, RValue: flatRHS})
	}

	return current
}

func (b *Builder) buildMapInsertStmt(stmt *hir.MapInsertStmt, current *BasicBlock) *BasicBlock {
	if stmt == nil {
		return current
	}

	var flatMap, flatVal Value
	flatMap, current = b.flattenExpr(stmt.Map, current)
	flatVal, current = b.flattenExpr(stmt.Value, current)

	hashVal, ptrVal, lenVal, intKey, current := b.lowerMapKey(stmt.Key, current)

	putTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{Name: putTmp, Type: types.UnitType{}})

	// EMIT EXACTLY 6 ARGUMENTS FOR ZIG ABI
	current.Statements = append(current.Statements, &CallInst{
		Dst:      putTmp,
		Function: &Register{Name: "maml_map_put", Type: types.UnknownType{}},
		Arguments: []MIRCallArg{
			{Argument: flatMap, Mut: true},
			{Argument: hashVal},
			{Argument: ptrVal},
			{Argument: lenVal},
			{Argument: intKey},
			{Argument: flatVal},
		},
		Type: types.UnitType{},
	})

	return current
}

func (b *Builder) buildVecWriteStmt(stmt *hir.VecWriteStmt, current *BasicBlock) *BasicBlock {
	if stmt == nil {
		return current
	}

	var flatVec, flatIdx, flatVal Value

	// 1. Flatten the receiver, index, and value into atomic operands
	flatVec, current = b.flattenExpr(stmt.Vec, current)
	flatIdx, current = b.flattenExpr(stmt.Index, current)
	flatVal, current = b.flattenExpr(stmt.Value, current)

	// 2. Create a temporary for the unit return value
	setTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{
		Name: setTmp,
		Type: types.UnitType{},
	})

	// 3. Emit the runtime call to mutate the vector in-place
	current.Statements = append(current.Statements, &CallInst{
		Dst:      setTmp,
		Function: &Register{Name: "maml_vec_set", Type: types.UnknownType{}},
		Arguments: []MIRCallArg{
			{Argument: flatVec, Mut: true}, // Pass vector by mutable reference
			{Argument: flatIdx},            // The integer index
			{Argument: flatVal},            // The value to write
		},
		Type: types.UnitType{},
	})

	return current
}

func (b *Builder) buildVecPushStmt(stmt *hir.VecPushStmt, current *BasicBlock) *BasicBlock {
	if stmt == nil {
		return current
	}

	var flatVec, flatVal Value

	// 1. Flatten the receiver, index, and value into atomic operands
	flatVec, current = b.flattenExpr(stmt.Vec, current)
	flatVal, current = b.flattenExpr(stmt.Value, current)

	// 2. Create a temporary for the unit return value
	setTmp := b.newTemp()
	current.Statements = append(current.Statements, &TempDeclInst{
		Name: setTmp,
		Type: types.UnitType{},
	})

	// 3. Emit the runtime call to mutate the vector in-place
	current.Statements = append(current.Statements, &CallInst{
		Dst:      setTmp,
		Function: &Register{Name: "maml_vec_push", Type: types.UnknownType{}},
		Arguments: []MIRCallArg{
			{Argument: flatVec, Mut: true}, // Pass vector by mutable reference
			{Argument: flatVal},            // The value to write
		},
		Type: types.UnitType{},
	})

	return current
}

func (b *Builder) emitMemoryTransfer(dst string, flatRHS Value, dstSym *types.Symbol, current *BasicBlock) {
	// Emit an allocation-agnostic temporary declaration
	var t types.Type = types.UnknownType{}
	if dstSym != nil {
		t = dstSym.Type
	}

	current.Statements = append(current.Statements, &TempDeclInst{
		Name: dst,
		Type: t,
	})

	if reg, isReg := flatRHS.(*Register); isReg {
		if t.IsReferenceType() {
			current.Statements = append(current.Statements, &MoveInst{Dst: dst, Src: reg.Name})
		} else {
			current.Statements = append(current.Statements, &CopyInst{Dst: dst, Src: reg.Name})
		}
	} else {
		current.Statements = append(current.Statements, &AssignInst{Dst: dst, RValue: flatRHS})
	}
}

// getSymbolName guarantees a collision-free variable name for the MIR environment.
func (b *Builder) getSymbolName(sym *types.Symbol) string {
	if sym == nil {
		return ""
	}
	// If we have already mapped this specific symbol, return its unique name
	if name, exists := b.symNames[sym]; exists {
		return name
	}

	// Otherwise, it's a new declaration. Generate a unique name if shadowed.
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
