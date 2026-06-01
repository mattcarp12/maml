package mir

import (
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// Function combines a function's static signature with its executable Control Flow Graph.
type Function struct {
	Name       string
	Params     []*tast.Param
	ReturnType types.Type
	IsAsync    bool
	Graph      *Graph
}

// Program is the perfectly flat handover structure for the backend.
type Program struct {
	TypeDecls []*tast.TypeDecl
	Functions []Function
}
