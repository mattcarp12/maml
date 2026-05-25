package sema

import "github.com/mattcarp12/maml/frontend/types"

type Scope struct {
	parent  *Scope
	symbols map[string]*types.Symbol
	types   map[string]types.Type
}

func newGlobalScope() *Scope {
	global := &Scope{
		symbols: make(map[string]*types.Symbol),
		types:   make(map[string]types.Type),
	}

	global.symbols["puts"] = &types.Symbol{
		Kind:    types.FuncSymbol,
		Name:    "puts",
		Mutable: false,
		Type: &types.FunctionType{
			Params:     []types.Type{types.StringType{}},
			ParamModes: []types.ParamMode{types.ParamBorrow},
			Return:     types.IntType{},
		},
	}

	return global
}

func (a *Analyzer) pushScope() {
	a.scope = &Scope{
		parent:  a.scope,
		symbols: make(map[string]*types.Symbol),
		types:   make(map[string]types.Type),
	}
}

func (a *Analyzer) popScope() {
	if a.scope == nil || a.scope.parent == nil {
		return
	}
	a.scope = a.scope.parent
}

func (a *Analyzer) resolve(name string) *types.Symbol {
	for s := a.scope; s != nil; s = s.parent {
		if sym, ok := s.symbols[name]; ok {
			return sym
		}
	}
	return nil
}
