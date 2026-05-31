package mir

import (
	"github.com/mattcarp12/maml/frontend/tast"

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

// BuildProgram translates an entire HIR program into a flattened MIR program.
func BuildProgram(hirProg *tast.Program) *Program {
	if hirProg == nil {
		return nil
	}

	mirProg := &Program{
		TypeDecls: make([]*tast.TypeDecl, 0),
		Functions: make([]Function, 0),
	}

	for _, decl := range hirProg.Decls {
		switch d := decl.(type) {
		case *tast.TypeDecl:
			mirProg.TypeDecls = append(mirProg.TypeDecls, d)

		case *tast.FnDecl:
			// Generate the CFG for the function body
			graph := Build(d)

			// Bundle the CFG with the function's static signature
			mirProg.Functions = append(mirProg.Functions, Function{
				Name:       d.Name,
				Params:     d.Params,
				ReturnType: d.ReturnType,
				IsAsync:    d.IsAsync,
				Graph:      graph,
				Warnings:   []string{},
			})
		}
	}

	return mirProg
}

// Build translates a hierarchical HIR function into a flat MIR Control Flow Graph.
func Build(fn *tast.FnDecl) *Graph {
	b := &Builder{
		graph: NewGraph(),
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

	// FIXED: If the trailing basic block was left completely open/unterminated,
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

func (b *Builder) buildBlockStmt(body *tast.BlockStmt, current *BasicBlock) *BasicBlock {
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

func (b *Builder) buildStmt(stmt tast.Stmt, current *BasicBlock) *BasicBlock {
	switch s := stmt.(type) {
	case *tast.DeclareStmt:
		current = b.buildDeclareStmt(s, current)
		return current
	case *tast.AssignStmt:
		return b.buildAssignStmt(s, current)
	case *tast.ExprStmt:
		_, current = b.flattenExpr(s.Value, current)
		return current
	case *tast.ReturnStmt:
		return b.buildReturnStmt(s, current)
	case *tast.ForStmt:
		return b.buildForStmt(s, current)
	case *tast.BreakStmt:
		if len(b.loops) == 0 {
			// Parser/Sema guarantees we only see breaks inside loops, but guard for safety.
			return current
		}
		activeLoop := b.loops[len(b.loops)-1]
		current.Terminator = &JumpTerminator{Target: activeLoop.Exit}
		// Return nil because any statements following a break are dead code
		return nil
	case *tast.ContinueStmt:
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

func (b *Builder) buildReturnStmt(stmt *tast.ReturnStmt, current *BasicBlock) *BasicBlock {
	if stmt.Value != nil {
		var flatRet tast.Expr
		flatRet, current = b.flattenExpr(stmt.Value, current)
		// Store the evaluated return value in an explicit return register
		current.Statements = append(current.Statements, &AssignInst{Dst: "_ret", RValue: flatRet})
	}

	current.Terminator = &ReturnTerminator{}
	return nil
}

func (b *Builder) buildForStmt(stmt *tast.ForStmt, current *BasicBlock) *BasicBlock {
	// 1. Evaluate the Init statement in the current block
	if stmt.Init != nil {
		current = b.buildStmt(stmt.Init, current)
	}

	// 2. Allocate the three distinct loop phases
	condBlock := b.newBlock()
	bodyBlock := b.newBlock()
	exitBlock := b.newBlock()

	// 3. Terminate the current block by jumping into the condition block
	current.Terminator = &JumpTerminator{Target: condBlock.ID}

	// 4. Flatten and evaluate the Loop Condition
	var flatCond tast.Expr
	condEvalBlock := condBlock

	if stmt.Condition != nil {
		flatCond, condEvalBlock = b.flattenExpr(stmt.Condition, condBlock)
	} else {
		// Infinite loops implicitly evaluate to true
		flatCond = &tast.BoolLiteral{Value: true, Type: types.BoolType{}}
	}

	// 5. Branch based on the condition evaluation
	condEvalBlock.Terminator = &BranchTerminator{
		Condition:   flatCond,
		TrueTarget:  bodyBlock.ID,
		FalseTarget: exitBlock.ID,
	}

	// 6. Push the loop tracker for break/continue support
	b.loops = append(b.loops, LoopTracker{Header: condBlock.ID, Exit: exitBlock.ID})

	// 7. Build the Body block
	bodyEndBlock := b.buildBlockStmt(stmt.Body, bodyBlock)

	// 8. Evaluate the Post statement (e.g., i = i + 1)
	if stmt.Post != nil {
		bodyEndBlock = b.buildStmt(stmt.Post, bodyEndBlock)
	}

	// 9. Jump back to the condition block to repeat
	if bodyEndBlock.Terminator == nil {
		bodyEndBlock.Terminator = &JumpTerminator{Target: condBlock.ID}
	}

	// 10. Pop the loop tracker
	b.loops = b.loops[:len(b.loops)-1]

	// 11. Return the exit block so the rest of the function continues from here
	return exitBlock
}

// buildDeclareStmt translates a local variable declaration into explicit memory ops.
func (b *Builder) buildDeclareStmt(stmt *tast.DeclareStmt, current *BasicBlock) *BasicBlock {
	flatRHS, current := b.flattenExpr(stmt.Value, current)
	b.emitMemoryTransfer(stmt.Symbol.Name, flatRHS, stmt.Symbol, current)
	return current
}

// buildAssignStmt translates a reassignment into explicit memory ops without re-allocating.
func (b *Builder) buildAssignStmt(stmt *tast.AssignStmt, current *BasicBlock) *BasicBlock {
	// Guard against uninitialized or partially empty statements
	if stmt == nil || stmt.LValue == nil || stmt.RValue == nil {
		return current
	}

	// Intercept array/slice index mutations before they are flattened into read evaluations
	if idx, ok := stmt.LValue.(*tast.IndexExpr); ok {
		flatTarget, current := b.flattenExpr(idx.Left, current)
		flatIdx, current := b.flattenExpr(idx.Index, current)
		flatRHS, current := b.flattenExpr(stmt.RValue, current)

		targetName := ""
		if id, isId := flatTarget.(*tast.Identifier); isId {
			targetName = id.Value
		} else {
			targetName = flatTarget.String()
		}

		current.Statements = append(current.Statements, &IndexAssignInst{
			Target:     targetName,
			TargetType: flatTarget.(*tast.Identifier).Type,
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
		if id, ok := flatLHS.(*tast.Identifier); ok && id != nil {
			dstName = id.Value
			dstType = id.Type // Preserves exact structural type (Struct, SumType, etc.)
		} else {
			dstName = flatLHS.String() // Fallback for complex lowered pointers or stringified symbols
			dstType = types.UnknownType{}
		}
	}

	// Failsafe guard if destination evaluation completely dropped out
	if dstName == "" {
		return current
	}

	// 2. ABI Routing: Perform direct transfers instead of wrapping in a TempDeclInst
	if ident, isIdent := flatRHS.(*tast.Identifier); isIdent && ident != nil && ident.Symbol != nil && ident.Symbol.Kind == types.VarSymbol {
		if dstType != nil && dstType.IsReferenceType() {
			// Affine transfer of ownership
			current.Statements = append(current.Statements, &MoveInst{Dst: dstName, Src: ident.Value})
		} else {
			// Primitive duplication
			current.Statements = append(current.Statements, &CopyInst{Dst: dstName, Src: ident.Value})
		}
	} else if flatRHS != nil {
		// Pure flat expression evaluation assignment
		current.Statements = append(current.Statements, &AssignInst{Dst: dstName, RValue: flatRHS})
	}

	return current
}

func (b *Builder) emitMemoryTransfer(dst string, flatRHS tast.Expr, dstSym *types.Symbol, current *BasicBlock) {
	// Emit an allocation-agnostic temporary declaration
	var t types.Type = types.UnknownType{}
	if dstSym != nil {
		t = dstSym.Type
	}

	current.Statements = append(current.Statements, &TempDeclInst{
		Name: dst,
		Type: t,
	})

	if ident, isIdent := flatRHS.(*tast.Identifier); isIdent && ident.Symbol != nil && ident.Symbol.Kind == types.VarSymbol {
		if t.IsReferenceType() {
			current.Statements = append(current.Statements, &MoveInst{Dst: dst, Src: ident.Value})
		} else {
			current.Statements = append(current.Statements, &CopyInst{Dst: dst, Src: ident.Value})
		}
	} else {
		current.Statements = append(current.Statements, &AssignInst{Dst: dst, RValue: flatRHS})
	}
}
