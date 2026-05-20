package cfg

import "github.com/mattcarp12/maml/frontend/ast"

type Result struct {
	AlwaysReturns bool

	ReachableBlocks map[BlockID]bool

	DeadBlocks []*Block

	DeadStatements []ast.Node
}