package ownership

import "github.com/mattcarp12/maml/internal/sema"

// BindingState manages the transient lifecycle information of an in-scope variable binding.
type BindingState struct {
	Symbol      *sema.Symbol // Read-only reference back to the original type symbol
	Invalidated bool         // Tracks if the variable's value has been moved (use-after-move guard)
}

// Scope tracks the ownership states of variables within a specific lexical block.
type Scope struct {
	Parent   *Scope
	Bindings map[string]*BindingState
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:   parent,
		Bindings: make(map[string]*BindingState),
	}
}

func (s *Scope) Lookup(name string) (*BindingState, bool) {
	for curr := s; curr != nil; curr = curr.Parent {
		if state, exists := curr.Bindings[name]; exists {
			return state, true
		}
	}
	return nil, false
}