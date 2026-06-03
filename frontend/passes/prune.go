package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
)

// Reachable performs a BFS to find all accessible blocks.
func Reachable(g *mir.Graph) map[mir.BlockID]bool {
	visited := make(map[mir.BlockID]bool)
	var queue []mir.BlockID

	if g == nil || g.Blocks[g.Entry] == nil {
		return visited
	}

	queue = append(queue, g.Entry)
	visited[g.Entry] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		block := g.Blocks[curr]
		if block == nil || block.Terminator == nil {
			continue
		}

		var succs []mir.BlockID
		switch t := block.Terminator.(type) {
		case *mir.JumpTerminator:
			succs = append(succs, t.Target)
		case *mir.BranchTerminator:
			succs = append(succs, t.TrueTarget, t.FalseTarget)
		}

		for _, succ := range succs {
			if !visited[succ] {
				visited[succ] = true
				queue = append(queue, succ)
			}
		}
	}
	return visited
}

// Graph actively deletes all unreachable Basic Blocks from the MIR (Dead Code Elimination).
func Graph(g *mir.Graph) {
	if g == nil {
		return
	}

	reachable := Reachable(g)

	// Physically delete dead blocks from the map
	for _, block := range g.SortedBlocks() {
		if !reachable[block.ID] {
			delete(g.Blocks, block.ID)
		}
	}
}
