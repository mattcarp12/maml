package mir

import (
	"github.com/mattcarp12/maml/frontend/tast"
)

// Terminator is the required final instruction of every BasicBlock.
// It dictates where the control flow goes next.
type Terminator interface {
	isTerminator()
}

// ReturnTerminator signifies that the function exits at this block.
// The actual value being returned is captured by the preceding hir.ReturnStmt.
type ReturnTerminator struct{}

func (ReturnTerminator) isTerminator() {}

// JumpTerminator is an unconditional jump to another block.
// Used heavily by our desugared hir.LoopStmt and hir.ContinueStmt.
type JumpTerminator struct {
	Target BlockID
}

func (JumpTerminator) isTerminator() {}

// BranchTerminator is a conditional jump.
// Used exclusively by our desugared hir.IfExpr chains.
type BranchTerminator struct {
	Condition   tast.Expr
	TrueTarget  BlockID
	FalseTarget BlockID
}

func (BranchTerminator) isTerminator() {}

// UnreachableTerminator represents a block that logically cannot proceed.
// Often used after infinite loops or panics.
type UnreachableTerminator struct{}

func (UnreachableTerminator) isTerminator() {}

// CoroSuspendTerminator suspends the current task and yields control back
// to the executor. It provides three explicit continuation blocks for the
// LLVM coroutine state machine to safely manage the task lifecycle.
type CoroSuspendTerminator struct {
	ResumeBlock  BlockID
	CleanupBlock BlockID
	SuspendBlock BlockID
}

func (CoroSuspendTerminator) isTerminator() {}
