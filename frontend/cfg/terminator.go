package cfg

import "github.com/mattcarp12/maml/frontend/ast"

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
	Condition   ast.Expr
	TrueTarget  BlockID
	FalseTarget BlockID
}

func (BranchTerminator) isTerminator() {}

type UnreachableTerminator struct{}

func (UnreachableTerminator) isTerminator() {}
