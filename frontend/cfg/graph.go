package cfg

import "github.com/mattcarp12/maml/frontend/ast"

type BlockID int

type Block struct {
	ID BlockID

	Statements []ast.Node

	Succs []BlockID
	Preds []BlockID

	Terminator Terminator

	Reachable bool
}

type CFG struct {
	Entry BlockID

	Exit BlockID

	Fallthrough BlockID

	Blocks map[BlockID]*Block
}

func New() *CFG {
	return &CFG{
		Blocks: make(map[BlockID]*Block),
	}
}
