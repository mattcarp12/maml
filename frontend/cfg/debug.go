package cfg

import (
	"fmt"
	"sort"
)

func Dump(g *CFG) {
	var ids []int

	for id := range g.Blocks {
		ids = append(ids, int(id))
	}

	sort.Ints(ids)

	for _, raw := range ids {
		id := BlockID(raw)

		b := g.Blocks[id]

		fmt.Printf("Block %d\n", id)

		fmt.Printf("  Succs: %v\n", b.Succs)
		fmt.Printf("  Preds: %v\n", b.Preds)
		fmt.Printf("  Terminal: %v\n", b.Terminator)

		for _, stmt := range b.Statements {
			fmt.Printf("    %T\n", stmt)
		}

		fmt.Println()
	}
}

func DumpReachability(g *CFG) {
	for id, block := range g.Blocks {
		fmt.Printf(
			"Block %d reachable=%v\n",
			id,
			block.Reachable,
		)
	}
}
