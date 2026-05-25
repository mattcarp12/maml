package mir

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

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
}

// Build translates a hierarchical HIR function into a flat MIR Control Flow Graph.
func Build(fn *hir.FnDecl) *Graph {
	b := &Builder{
		graph: NewGraph(),
	}

	entry := b.newBlock()
	b.graph.Entry = entry.ID

	// =========================================================================
	// Coroutine Initialization
	// =========================================================================
	// If the function is async, the absolute first instruction of the entry
	// block must be the coroutine frame allocation.
	if fn.IsAsync {
		entry.Statements = append(entry.Statements, &CoroPrologueInst{})
	}

	b.buildBlockStmt(fn.Body, entry)
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
	case *hir.ExprStmt:
		_, current = b.flattenExpr(s.Value, current)
		return current
	case *hir.ReturnStmt:
		return b.buildReturnStmt(s, current)

	case *hir.LoopStmt:
		return b.flattenLoopStmt(s, current)
	case *hir.BreakStmt:
		if len(b.loops) == 0 {
			// Parser/Sema guarantees we only see breaks inside loops, but guard for safety.
			return current
		}
		activeLoop := b.loops[len(b.loops)-1]
		current.Terminator = &JumpTerminator{Target: activeLoop.Exit}
		// Return nil because any statements following a break are dead code
		return nil

	case *hir.ContinueStmt:
		if len(b.loops) == 0 {
			return current
		}
		activeLoop := b.loops[len(b.loops)-1]
		current.Terminator = &JumpTerminator{Target: activeLoop.Header}
		// Return nil because any statements following a continue are dead code
		return nil
	}
	return current
}

func (b *Builder) buildReturnStmt(stmt *hir.ReturnStmt, current *BasicBlock) *BasicBlock {
	if stmt.Value != nil {
		var flatRet hir.Expr
		flatRet, current = b.flattenExpr(stmt.Value, current)
		// Store the evaluated return value in an explicit return register
		current.Statements = append(current.Statements, &AssignInst{Dst: "_ret", Expr: flatRet})
	}

	current.Terminator = &ReturnTerminator{}
	return nil
}

func (b *Builder) flattenLoopStmt(stmt *hir.LoopStmt, current *BasicBlock) *BasicBlock {
	// 1. Provision the loop's structural blocks
	headerBlock := b.newBlock()
	exitBlock := b.newBlock()

	// 2. Jump from current execution into the loop header
	current.Terminator = &JumpTerminator{Target: headerBlock.ID}

	// 3. Track the loop so nested Breaks/Continues can resolve their targets
	b.loops = append(b.loops, LoopTracker{
		Header: headerBlock.ID,
		Exit:   exitBlock.ID,
	})

	// 4. Build the loop body starting inside the header
	bodyEnd := b.buildBlockStmt(stmt.Body, headerBlock)

	// 5. Pop the loop tracker (we have exited the loop's lexical scope)
	b.loops = b.loops[:len(b.loops)-1]

	// 6. If the body doesn't terminate early (e.g., via return), cycle back to the header
	if bodyEnd != nil && bodyEnd.Terminator == nil {
		bodyEnd.Terminator = &JumpTerminator{Target: headerBlock.ID}
	}

	return exitBlock
}

// buildDeclareStmt translates a local variable declaration into explicit memory ops.
func (b *Builder) buildDeclareStmt(stmt *hir.DeclareStmt, current *BasicBlock) *BasicBlock {
	flatRHS, current := b.flattenExpr(stmt.Value, current)
	b.emitMemoryTransfer(stmt.Symbol.Name, flatRHS, stmt.Symbol, current)
	return current
}

// buildAssignStmt translates a reassignment into explicit memory ops.
func (b *Builder) buildAssignStmt(stmt *hir.AssignStmt, current *BasicBlock) *BasicBlock {
	flatLHS, current := b.flattenExpr(stmt.LValue, current)
	flatRHS, current := b.flattenExpr(stmt.RValue, current)

	dstName := ""
	if id, ok := flatLHS.(*hir.Identifier); ok {
		dstName = id.Value
	} else {
		dstName = flatLHS.String() // Fallback for complex lowered pointers
	}

	b.emitMemoryTransfer(dstName, flatRHS, nil, current)
	return current
}

func (b *Builder) emitMemoryTransfer(dst string, flatRHS hir.Expr, dstSym *types.Symbol, current *BasicBlock) {
	// Emit an allocation-agnostic temporary declaration
	var t types.Type = types.UnknownType{}
	if dstSym != nil {
		t = dstSym.Type
	}

	current.Statements = append(current.Statements, &TempDeclInst{
		Name: dst,
		Type: t,
	})

	if ident, isIdent := flatRHS.(*hir.Identifier); isIdent {
		if t.IsReferenceType() {
			current.Statements = append(current.Statements, &MoveInst{Dst: dst, Src: ident.Value})
		} else {
			current.Statements = append(current.Statements, &CopyInst{Dst: dst, Src: ident.Value})
		}
	} else {
		current.Statements = append(current.Statements, &AssignInst{Dst: dst, Expr: flatRHS})
	}
}
