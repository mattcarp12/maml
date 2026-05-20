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
		loop := b.currentLoop()

		if loop != nil {
			current.Terminator = JumpTerminator{
				Target: loop.Exit,
			}

			b.addEdge(current.ID, loop.Exit)

			return b.newBlock()
		}

		return current

	case *ast.ContinueStmt:
		loop := b.currentLoop()

		if loop != nil {
			current.Terminator = JumpTerminator{
				Target: loop.Header,
			}

			b.addEdge(current.ID, loop.Header)

			return b.newBlock()
		}

		return current

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

func (b *Builder) buildIfExpr(
	expr *ast.IfExpr,
	current *Block,
) *Block {

	constCond := EvalBool(expr.Condition)

	merge := b.newBlock()

	// ------------------------------------------------------------
	// condition known TRUE
	// ------------------------------------------------------------

	if constCond.Known && constCond.Value {
		thenBlock := b.newBlock()

		b.addEdge(current.ID, thenBlock.ID)

		thenExit := b.buildBlockStmt(
			expr.Consequence,
			thenBlock.ID,
		)

		if thenExit != nil && thenExit.Terminator == nil {
			b.addEdge(thenExit.ID, merge.ID)
		}

		return merge
	}

	// ------------------------------------------------------------
	// condition known FALSE
	// ------------------------------------------------------------

	if constCond.Known && !constCond.Value {

		if expr.Alternative == nil {
			b.addEdge(current.ID, merge.ID)
			return merge
		}

		elseBlock := b.newBlock()

		b.addEdge(current.ID, elseBlock.ID)

		elseExit := b.buildBlockStmt(
			expr.Alternative,
			elseBlock.ID,
		)

		if elseExit != nil && elseExit.Terminator == nil {
			b.addEdge(elseExit.ID, merge.ID)
		}

		return merge
	}

	// ------------------------------------------------------------
	// dynamic condition
	// ------------------------------------------------------------

	thenBlock := b.newBlock()

	b.addEdge(current.ID, thenBlock.ID)

	thenExit := b.buildBlockStmt(
		expr.Consequence,
		thenBlock.ID,
	)

	if thenExit != nil && thenExit.Terminator == nil {
		b.addEdge(thenExit.ID, merge.ID)
	}

	if expr.Alternative != nil {
		elseBlock := b.newBlock()

		b.addEdge(current.ID, elseBlock.ID)

		elseExit := b.buildBlockStmt(
			expr.Alternative,
			elseBlock.ID,
		)

		if elseExit != nil && elseExit.Terminator == nil {
			b.addEdge(elseExit.ID, merge.ID)
		}

	} else {
		b.addEdge(current.ID, merge.ID)
	}

	return merge
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
			arm.Body,
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

func (b *Builder) buildForStmt(
	stmt *ast.ForStmt,
	current *Block,
) *Block {

	header := b.newBlock()
	body := b.newBlock()
	after := b.newBlock()

	b.addEdge(current.ID, header.ID)

	infinite := false

	if stmt.Condition == nil {
		infinite = true
	} else {
		cond := EvalBool(stmt.Condition)

		if cond.Known && cond.Value {
			infinite = true
		}

		if cond.Known && !cond.Value {
			b.addEdge(header.ID, after.ID)
			return after
		}
	}

	b.addEdge(header.ID, body.ID)

	b.pushLoop(header.ID, after.ID)

	bodyExit := b.buildBlockStmt(stmt.Body, body.ID)

	b.popLoop()

	if bodyExit != nil &&
		bodyExit.Terminator == nil {

		b.addEdge(bodyExit.ID, header.ID)

		bodyExit.Terminator = JumpTerminator{
			Target: header.ID,
		}
	}

	if !infinite {
		b.addEdge(header.ID, after.ID)
	}

	return after
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
