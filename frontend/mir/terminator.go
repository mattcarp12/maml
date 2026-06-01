package mir

type Terminator interface {
	isTerminator()
}

type ReturnTerminator struct{}

func (ReturnTerminator) isTerminator() {}

type JumpTerminator struct {
	Target BlockID
}

func (JumpTerminator) isTerminator() {}

type BranchTerminator struct {
	Condition   string
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
