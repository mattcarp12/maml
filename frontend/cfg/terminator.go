package cfg

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
	TrueTarget  BlockID
	FalseTarget BlockID
}

func (BranchTerminator) isTerminator() {}

type UnreachableTerminator struct{}

func (UnreachableTerminator) isTerminator() {}
