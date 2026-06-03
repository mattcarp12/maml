package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/types"
)

// resolveBuiltinMethod looks up and synthesizes a concrete built-in method symbol
// based on the underlying structural type of the receiver without using reflection.
func (a *Analyzer) resolveBuiltinMethod(receiver types.Type, methodName string) (*types.Symbol, error) {
	if receiver == nil || types.IsUnknown(receiver) {
		return nil, fmt.Errorf("cannot resolve method on unknown type")
	}

	switch ty := receiver.(type) {
	case types.VectorType:
		return resolveVectorMethod(ty, methodName)

	case types.MapType:
		return resolveMapMethod(ty, methodName)

	default:
		return nil, fmt.Errorf("type '%s' does not support methods", receiver.String())
	}
}

// -------------------------------------------------------------------------
// Vector Method Synthesis
// -------------------------------------------------------------------------
func resolveVectorMethod(vecTy types.VectorType, name string) (*types.Symbol, error) {
	switch name {
	case "push":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "push",
			Type: &types.FunctionType{
				Params:     []types.Type{vecTy.Base},
				ParamModes: []types.ParamMode{types.ParamBorrow},
				Return:     types.UnitType{},
			},
			Mutable:    true,
			MethodKind: types.MethodVecPush,
		}, nil

	case "pop":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "pop",
			Type: &types.FunctionType{
				Params:     []types.Type{},
				ParamModes: []types.ParamMode{},
				Return:     vecTy.Base,
			},
			Mutable:    true,
			MethodKind: types.MethodVecPop,
		}, nil

	case "len":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "len",
			Type: &types.FunctionType{
				Params:     []types.Type{},
				ParamModes: []types.ParamMode{},
				Return:     types.IntType{},
			},
			Mutable:    false,
			MethodKind: types.MethodVecLen,
		}, nil

	default:
		return nil, fmt.Errorf("method '%s' does not exist on vector types", name)
	}
}

// -------------------------------------------------------------------------
// Map Method Synthesis
// -------------------------------------------------------------------------
func resolveMapMethod(mapTy types.MapType, name string) (*types.Symbol, error) {
	switch name {
	case "put":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "put",
			Type: &types.FunctionType{
				Params:     []types.Type{mapTy.Key, mapTy.Value},
				ParamModes: []types.ParamMode{types.ParamBorrow, types.ParamBorrow},
				Return:     types.UnitType{},
			},
			Mutable:    true,
			MethodKind: types.MethodMapPut,
		}, nil

	case "get":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "get",
			Type: &types.FunctionType{
				Params:     []types.Type{mapTy.Key},
				ParamModes: []types.ParamMode{types.ParamBorrow},
				Return:     mapTy.Value, // Can wrap in Option<V> later if desired
			},
			Mutable:    false,
			MethodKind: types.MethodMapGet,
		}, nil

	case "remove":
		return &types.Symbol{
			Kind: types.FuncSymbol,
			Name: "remove",
			Type: &types.FunctionType{
				Params:     []types.Type{mapTy.Key},
				ParamModes: []types.ParamMode{types.ParamBorrow},
				Return:     types.UnitType{},
			},
			Mutable:    true,
			MethodKind: types.MethodMapRemove,
		}, nil

	default:
		return nil, fmt.Errorf("method '%s' does not exist on map types", name)
	}
}
