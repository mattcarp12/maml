package mir

// BlockID is a unique identifier for a basic block within a function.
type BlockID int

// BasicBlock represents a flat, linear sequence of executable instructions
// that ends with exactly one control-flow terminator.
type BasicBlock struct {
	ID         BlockID
	Statements []Instruction // Strictly flat, desugared HIR statements
	Terminator Terminator    // The branch, jump, or return exiting the block
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
