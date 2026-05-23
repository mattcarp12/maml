package cfg

import "github.com/mattcarp12/maml/frontend/ast"

func Reachable(g *CFG) map[BlockID]bool {
	reachable := make(map[BlockID]bool)
	var queue []BlockID

	queue = append(queue, g.Entry)

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		if reachable[id] {
			continue
		}
		reachable[id] = true

		block := g.Blocks[id]

		// Check if this block ends in a conditional branch
		if branch, ok := block.Terminator.(BranchTerminator); ok {
			cb := EvalBool(branch.Condition)

			// If we know the boolean evaluates to a constant at compile-time:
			if cb.Known {
				if cb.Value {
					queue = append(queue, branch.TrueTarget)
				} else {
					queue = append(queue, branch.FalseTarget)
				}
				// Skip adding all successors blindly!
				continue
			}
		}

		// Default fallback: add all successors to the queue
		queue = append(queue, block.Succs...)
	}

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
		nodes = append(nodes, block.Statements...)
	}
	return nodes
}
