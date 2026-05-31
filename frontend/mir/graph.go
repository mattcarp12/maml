package mir

import "sort"

// BlockID is a unique identifier for a basic block within a function.
type BlockID int

// BasicBlock represents a flat, linear sequence of executable instructions
// that ends with exactly one control-flow terminator.
type BasicBlock struct {
	ID         BlockID
	Statements []Instruction
	Terminator Terminator
}

// Graph represents the Mid-Level IR (Control Flow Graph) for a single function.
type Graph struct {
	Entry  BlockID
	Blocks map[BlockID]*BasicBlock
}

// NewGraph initializes a new, empty Control Flow Graph.
func NewGraph() *Graph {
	return &Graph{
		Blocks: make(map[BlockID]*BasicBlock),
	}
}

func (g *Graph) SortedBlocks() []*BasicBlock {
	if g == nil || len(g.Blocks) == 0 {
		return nil
	}

	// 1. Extract and sort the keys
	var ids []int
	for id := range g.Blocks {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)

	// 2. Build the deterministic slice of blocks
	var blocks []*BasicBlock
	for _, id := range ids {
		blocks = append(blocks, g.Blocks[BlockID(id)])
	}

	return blocks
}
