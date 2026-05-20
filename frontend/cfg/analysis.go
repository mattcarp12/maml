package cfg

import "github.com/mattcarp12/maml/frontend/ast"

func Reachable(g *CFG) map[BlockID]bool {
	reachable := make(map[BlockID]bool)

	var visit func(BlockID)

	visit = func(id BlockID) {
		if reachable[id] {
			return
		}

		reachable[id] = true

		block := g.Blocks[id]

		for _, succ := range block.Succs {
			visit(succ)
		}
	}

	visit(g.Entry)

	return reachable
}

func MarkReachable(g *CFG) {
	reachable := Reachable(g)

	for id, block := range g.Blocks {
		block.Reachable = reachable[id]
	}
}

func FallthroughReachable(g *CFG) bool {
	reachable := Reachable(g)
	return reachable[g.Fallthrough]
}

func AlwaysReturns(g *CFG) bool {
	return !FallthroughReachable(g)
}

func UnreachableBlocks(g *CFG) []*Block {
	reachable := Reachable(g)

	var dead []*Block

	for id, block := range g.Blocks {
		if !reachable[id] {
			dead = append(dead, block)
		}
	}

	return dead
}

func UnreachableStatements(g *CFG) []ast.Node {
	reachable := Reachable(g)

	var nodes []ast.Node

	for id, block := range g.Blocks {
		if reachable[id] {
			continue
		}

		for _, stmt := range block.Statements {
			nodes = append(nodes, stmt)
		}
	}

	return nodes
}
