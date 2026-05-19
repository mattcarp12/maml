package sema

type Scope struct {
	parent  *Scope
	symbols map[string]*Symbol
	types   map[string]Type
}

func newGlobalScope() *Scope {
	global := &Scope{
		symbols: make(map[string]*Symbol),
		types:   make(map[string]Type),
	}

	global.symbols["puts"] = &Symbol{
		Kind:    FuncSymbol,
		Name:    "puts",
		Mutable: false,
		Type: &FunctionType{
			Params:     []Type{StringType{}},
			ParamModes: []ParamMode{ParamBorrow},
			Return:     IntType{},
		},
	}

	return global
}

func (a *Analyzer) pushScope() {
	a.scope = &Scope{
		parent:  a.scope,
		symbols: make(map[string]*Symbol),
		types:   make(map[string]Type),
	}
}

func (a *Analyzer) popScope() {
	if a.scope == nil || a.scope.parent == nil {
		return
	}
	a.scope = a.scope.parent
}

func (a *Analyzer) resolve(name string) *Symbol {
	for s := a.scope; s != nil; s = s.parent {
		if sym, ok := s.symbols[name]; ok {
			return sym
		}
	}
	return nil
}
