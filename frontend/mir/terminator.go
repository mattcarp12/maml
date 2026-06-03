package mir

import "github.com/mattcarp12/maml/frontend/hir"

type Terminator interface {
	isTerminator()
}

type ReturnTerminator struct {
	Value hir.Operand
}

func (ReturnTerminator) isTerminator() {}

type JumpTerminator struct {
	Target BlockID
}

func (JumpTerminator) isTerminator() {}

type BranchTerminator struct {
	Condition   hir.Operand
	TrueTarget  BlockID
	FalseTarget BlockID
}

func (BranchTerminator) isTerminator() {}

type UnreachableTerminator struct{}

func (UnreachableTerminator) isTerminator() {}

type CoroSuspendTerminator struct {
	ResumeBlock  BlockID
	CleanupBlock BlockID
	SuspendBlock BlockID
}

func (CoroSuspendTerminator) isTerminator() {}
