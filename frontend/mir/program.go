package mir

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Function combines a function's static signature with its executable Control Flow Graph.
type Function struct {
	Name       string
	Params     []*hir.Param
	ReturnType types.Type
	IsAsync    bool
	Graph      *Graph
	Warnings   []string // e.g., "unreachable code detected"
}

// Program is the perfectly flat handover structure for the backend.
type Program struct {
	TypeDecls []*hir.TypeDecl
	Functions []Function
}
