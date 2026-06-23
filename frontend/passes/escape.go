package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/types"
)

type EscapeState int

const (
	OnStack EscapeState = iota
	OnHeap
)

type ConnectionGraph struct {
	// Tracks which variables flow to which variables
	FlowsTo map[string][]string
	// Tracks the final state of each variable
	States map[string]EscapeState
	// Fields maps RootStruct -> []FieldName
	Fields map[string][]string
}

func newConnectionGraph() *ConnectionGraph {
	return &ConnectionGraph{
		FlowsTo: make(map[string][]string),
		States:  make(map[string]EscapeState),
		Fields:  make(map[string][]string),
	}
}

func (g *ConnectionGraph) markEscaped(name string) {
	if g.States[name] == OnHeap {
		return
	}
	g.States[name] = OnHeap

	// 1. Transitive propagation: everything that flows to me also escapes
	for _, pred := range g.FlowsTo[name] {
		g.markEscaped(pred)
	}

	// 2. Hierarchical propagation: if a field escapes, the parent struct must escape
	for root, fields := range g.Fields {
		for _, f := range fields {
			if f == name {
				g.markEscaped(root)
			}
		}
	}
}

func (g *ConnectionGraph) addFlow(dst, src string) {
	g.FlowsTo[dst] = append(g.FlowsTo[dst], src)
	// If the destination is already escaped, the source must also escape
	if g.States[dst] == OnHeap {
		g.markEscaped(src)
	}
}

// isHeapEligible returns true only if the type contains internal 
// references that require heap management (like Vectors/Maps).
func isHeapEligible(t types.Type) bool {
    if t == nil {
        return false
    }

	// Views are fat pointers. They NEVER heap allocate themselves!
    if _, isView := t.(*types.ViewType); isView {
        return false
    }
    
    // If the type is not a reference type (like a Struct or Int/Bool), 
    // it can live on the stack and doesn't need to escape.
    if !t.IsReferenceType() {
        return false
    }
    
    // If it is a reference type, it needs to be on the heap.
    return true
}

func AnalyzeEscape(mirGraph *mir.Graph) map[string]EscapeState {
	graph := newConnectionGraph()

	for _, block := range mirGraph.Blocks {
		// 1. Process linear instructions
		for _, inst := range block.Statements {
			switch i := inst.(type) {
			case *mir.AssignInst:
				if reg, ok := i.RValue.(*mir.Register); ok {
					graph.addFlow(i.Dst, reg.Name)
				}
			case *mir.StructInitInst:
				// Link the root struct to its field
				root := i.Dst
				field := i.FieldName
				graph.Fields[root] = append(graph.Fields[root], root+"."+field)
				// Field assignment links the field to the source
				if reg, ok := i.Value.(*mir.Register); ok {
					graph.addFlow(root+"."+field, reg.Name)
				}
			case *mir.FieldReadInst:
				// Link field to the destination
				root := i.Object // This is a Value (could be register)
				if reg, ok := root.(*mir.Register); ok {
					graph.addFlow(i.Dst, reg.Name+"."+i.FieldName)
				}
			case *mir.CallInst:
				for _, arg := range i.Arguments {
					if reg, ok := arg.Argument.(*mir.Register); ok {
						// FIX: Only escape arguments if they are heap-eligible types
						if isHeapEligible(reg.Type) {
							graph.markEscaped(reg.Name)
						}
					}
				}
			}
		}

		// 2. Process the block terminator
		if block.Terminator != nil {
			switch t := block.Terminator.(type) {
			case *mir.ReturnTerminator:
				// FIX: Return values only escape the local frame if they are heap-eligible
				if reg, ok := t.Value.(*mir.Register); ok {
					if isHeapEligible(reg.Type) {
						graph.markEscaped(reg.Name)
					}
				}
			case *mir.BranchTerminator:
				// Branch conditions are just boolean checks, they don't escape.
			case *mir.JumpTerminator:
				// Jumps do not escape.
			}
		}
	}

	return graph.States
}
