package cfg

import "github.com/mattcarp12/maml/frontend/ast"

type LoopContext struct {
	Header BlockID
	Exit   BlockID
}

type Builder struct {
	cfg *CFG

	nextID BlockID

	loops []LoopContext
}

func Build(fn *ast.FnDecl) *CFG {
	b := &Builder{
		cfg: New(),
	}

	entry := b.newBlock()
	exit := b.newBlock()
	fallthrou := b.newBlock()

	b.cfg.Entry = entry.ID
	b.cfg.Exit = exit.ID
	b.cfg.Fallthrough = fallthrou.ID

	last := b.buildBlockStmt(fn.Body, entry.ID)

	if last != nil && last.Terminator == nil {
		b.addEdge(last.ID, fallthrou.ID)
	}

	return b.cfg
}

func (b *Builder) newBlock() *Block {
	id := b.nextID
	b.nextID++

	block := &Block{
		ID:         id,
		Statements: []ast.Node{},
		Succs:      []BlockID{},
		Preds:      []BlockID{},
	}

	b.cfg.Blocks[id] = block

	return block
}

func (b *Builder) block(id BlockID) *Block {
	return b.cfg.Blocks[id]
}

func (b *Builder) addEdge(from, to BlockID) {
	src := b.block(from)
	dst := b.block(to)

	src.Succs = append(src.Succs, to)
	dst.Preds = append(dst.Preds, from)
}

func (b *Builder) buildBlockStmt(
	body *ast.BlockStmt,
	current BlockID,
) *Block {

	curr := b.block(current)

	for _, stmt := range body.Statements {
		curr = b.buildStmt(stmt, curr)
	}

	return curr
}

func (b *Builder) buildStmt(
	stmt ast.Stmt,
	current *Block,
) *Block {

	if current == nil {
		return nil
	}

	current.Statements = append(current.Statements, stmt)

	switch s := stmt.(type) {

	case *ast.ReturnStmt:
		current.Terminator = ReturnTerminator{}

		b.addEdge(current.ID, b.cfg.Exit)

		return b.newBlock()

	case *ast.BreakStmt:
		current.Statements = append(current.Statements, s)
		if loop := b.currentLoop(); loop != nil {
			current.Terminator = JumpTerminator{Target: loop.Exit}
			b.addEdge(current.ID, loop.Exit)
		}
		// Return a fresh, unattached block for any subsequent dead statements
		return b.newBlock()

	case *ast.ContinueStmt:
		current.Statements = append(current.Statements, s)
		if loop := b.currentLoop(); loop != nil {
			current.Terminator = JumpTerminator{Target: loop.Header}
			b.addEdge(current.ID, loop.Header)
		}
		// Return a fresh, unattached block for any subsequent dead statements
		return b.newBlock()

	case *ast.ExprStmt:
		return b.buildExprStmt(s, current)

	case *ast.ForStmt:
		return b.buildForStmt(s, current)

	default:
		return current
	}
}

func (b *Builder) buildExprStmt(
	stmt *ast.ExprStmt,
	current *Block,
) *Block {

	switch e := stmt.Value.(type) {

	case *ast.IfExpr:
		return b.buildIfExpr(e, current)

	case *ast.MatchExpr:
		return b.buildMatchExpr(e, current)

	default:
		return current
	}
}

func (b *Builder) buildIfExpr(expr *ast.IfExpr, current *Block) *Block {
	// 1. Create the fundamental blocks we know we'll need
	trueBlock := b.newBlock()
	mergeBlock := b.newBlock()

	var falseBlock *Block
	var falseTarget BlockID

	// 2. Determine the false target (either an else block or the merge block)
	if expr.Alternative != nil {
		falseBlock = b.newBlock()
		falseTarget = falseBlock.ID
	} else {
		falseTarget = mergeBlock.ID
	}

	// 3. Attach the terminator to the block evaluating the condition
	current.Terminator = BranchTerminator{
		Condition:   expr.Condition,
		TrueTarget:  trueBlock.ID,
		FalseTarget: falseTarget,
	}

	// 4. Wire up the edges from the current conditional block
	b.addEdge(current.ID, trueBlock.ID)
	b.addEdge(current.ID, falseTarget)

	// 5. Build the true branch
	// Note: We pass trueBlock.ID here based on your buildBlockStmt signature
	trueEnd := b.buildBlockStmt(expr.Consequence, trueBlock.ID)

	// If the branch doesn't end in a terminal (like `return` or `break`), it falls through to the merge block
	if trueEnd.Terminator == nil {
		b.addEdge(trueEnd.ID, mergeBlock.ID)
	}

	// 6. Build the false/else branch (if it exists)
	if expr.Alternative != nil {
		falseEnd := b.buildBlockStmt(expr.Alternative, falseBlock.ID)
		if falseEnd.Terminator == nil {
			b.addEdge(falseEnd.ID, mergeBlock.ID)
		}
	}

	// 7. Return the merge block so subsequent statements continue from there
	return mergeBlock
}

func (b *Builder) buildMatchExpr(
	expr *ast.MatchExpr,
	current *Block,
) *Block {

	merge := b.newBlock()

	for _, arm := range expr.Arms {
		armBlock := b.newBlock()

		b.addEdge(current.ID, armBlock.ID)

		armExit := b.buildBlockStmt(
			arm.Body.(*ast.BlockStmt),
			armBlock.ID,
		)

		if armExit != nil && armExit.Terminator == nil {
			b.addEdge(armExit.ID, merge.ID)
		}
	}

	// Non-exhaustive matches may fall through.
	if !MatchHasWildcard(expr) {
		b.addEdge(current.ID, merge.ID)
	}

	return merge
}

func (b *Builder) buildForStmt(stmt *ast.ForStmt, current *Block) *Block {
	// 1. Build Init if present
	if stmt.Init != nil {
		current = b.buildStmt(stmt.Init, current)
	}

	headerBlock := b.newBlock()
	bodyBlock := b.newBlock()
	postBlock := b.newBlock()
	exitBlock := b.newBlock()

	// Connect current flow into the loop header
	if current.Terminator == nil {
		b.addEdge(current.ID, headerBlock.ID)
	}

	// 2. Setup Loop Header (Condition)
	if stmt.Condition != nil {
		headerBlock.Terminator = BranchTerminator{
			Condition:   stmt.Condition,
			TrueTarget:  bodyBlock.ID,
			FalseTarget: exitBlock.ID,
		}
		b.addEdge(headerBlock.ID, bodyBlock.ID)
		b.addEdge(headerBlock.ID, exitBlock.ID)
	} else {
		b.addEdge(headerBlock.ID, bodyBlock.ID)
	}

	// 3. Build Body with Loop Context
	// Note: 'continue' should jump to the postBlock so the post-statement executes
	b.pushLoop(postBlock.ID, exitBlock.ID)
	bodyEnd := b.buildBlockStmt(stmt.Body, bodyBlock.ID)
	b.popLoop()

	// Connect the end of the body to the post block
	if bodyEnd.Terminator == nil {
		b.addEdge(bodyEnd.ID, postBlock.ID)
	}

	// 4. Build Post statement
	postEnd := postBlock
	if stmt.Post != nil {
		postEnd = b.buildStmt(stmt.Post, postBlock)
	}

	// Connect the end of the post block back to the condition header
	if postEnd.Terminator == nil {
		b.addEdge(postEnd.ID, headerBlock.ID)
	}

	return exitBlock
}

func (b *Builder) pushLoop(header, exit BlockID) {
	b.loops = append(b.loops, LoopContext{
		Header: header,
		Exit:   exit,
	})
}

func (b *Builder) popLoop() {
	b.loops = b.loops[:len(b.loops)-1]
}

func (b *Builder) currentLoop() *LoopContext {
	if len(b.loops) == 0 {
		return nil
	}

	return &b.loops[len(b.loops)-1]
}
