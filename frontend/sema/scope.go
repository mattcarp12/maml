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

	// 1. Inject polymorphic Option variants
	optType := types.NewOptionType(types.AnyType{})
	global.symbols["Some"] = &types.Symbol{
		Kind: types.VariantSymbol, Name: "Some", Type: optType, SumType: optType, Variant: optType.GetVariant("Some"),
	}
	global.symbols["None"] = &types.Symbol{
		Kind: types.VariantSymbol, Name: "None", Type: optType, SumType: optType, Variant: optType.GetVariant("None"),
	}

	// 2. Inject polymorphic Result variants
	resType := types.NewResultType(types.AnyType{}, types.AnyType{})
	global.symbols["Ok"] = &types.Symbol{
		Kind: types.VariantSymbol, Name: "Ok", Type: resType, SumType: resType, Variant: resType.GetVariant("Ok"),
	}
	global.symbols["Err"] = &types.Symbol{
		Kind: types.VariantSymbol, Name: "Err", Type: resType, SumType: resType, Variant: resType.GetVariant("Err"),
	}

	// Standard Output
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

	// FIXED: Register Async Runtime Hooks
	global.symbols["spawn"] = &types.Symbol{
		Kind: types.FuncSymbol,
		Name: "spawn",
		Type: &types.FunctionType{
			Params:     []types.Type{types.TaskType{Base: types.AnyType{}}},
			ParamModes: []types.ParamMode{types.ParamBorrow},
			Return:     types.UnitType{},
		},
	}
	global.symbols["run_executor"] = &types.Symbol{
		Kind: types.FuncSymbol,
		Name: "run_executor",
		Type: &types.FunctionType{
			Params:     []types.Type{},
			ParamModes: []types.ParamMode{},
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
