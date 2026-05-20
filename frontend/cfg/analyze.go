package cfg

import "github.com/mattcarp12/maml/frontend/ast"

func Analyze(g *CFG) *Result {
	reachable := Reachable(g)

	var deadBlocks []*Block

	for id, block := range g.Blocks {
		if !reachable[id] {
			deadBlocks = append(deadBlocks, block)
		}
	}

	var deadStmts []ast.Node

	for _, block := range deadBlocks {
		deadStmts = append(deadStmts, block.Statements...)
	}

	return &Result{
		AlwaysReturns: !reachable[g.Fallthrough],

		ReachableBlocks: reachable,

		DeadBlocks: deadBlocks,

		DeadStatements: deadStmts,
	}
}
