package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

type builtinGeneric struct {
	Arity int
	Build func(args []types.Type) types.Type
}

var builtinGenerics = map[string]builtinGeneric{
	"Vec": {
		Arity: 1,
		Build: func(args []types.Type) types.Type {
			return types.VectorType{
				Base: args[0],
			}
		},
	},
	"Map": {
		Arity: 2,
		Build: func(args []types.Type) types.Type {
			return types.MapType{
				Key:   args[0],
				Value: args[1],
			}
		},
	},
	"Option": {
		Arity: 1,
		Build: func(args []types.Type) types.Type {
			return types.NewOptionType(args[0])
		},
	},
	"Result": {
		Arity: 2,
		Build: func(args []types.Type) types.Type {
			return types.NewResultType(
				args[0],
				args[1],
			)
		},
	},
	"Task": { // TODO - Rename to Future
		Arity: 1,
		Build: func(args []types.Type) types.Type {
			return types.TaskType{
				Base: args[0],
			}
		},
	},
	"View": {
		Arity: 1,
		Build: func(args []types.Type) types.Type {
			return types.ViewType{
				Base: args[0],
			}
		},
	},
	
	// "Ref": {
	// 	Arity: 1,
	// 	Build: func(args []types.Type) types.Type {
	// 		return types.RefType{
	// 			Base: args[0],
	// 		}
	// 	},
	// },
}

func (a *Analyzer) resolveBuiltinGeneric(expr *ast.GenericTypeExpr) types.Type {
	builtin, ok := builtinGenerics[expr.Name.Value]
	if !ok {
		a.errorf(
			expr.Pos(),
			"generic type '%s' is not supported",
			expr.Name,
		)
		return types.UnknownType{}
	}

	if len(expr.Args) != builtin.Arity {
		a.errorf(
			expr.Pos(),
			"generic type '%s' expects %d type argument(s), got %d",
			expr.Name,
			builtin.Arity,
			len(expr.Args),
		)
		return types.UnknownType{}
	}

	args := make([]types.Type, len(expr.Args))
	for i, arg := range expr.Args {
		args[i] = a.resolveAstType(arg)
	}
	return builtin.Build(args)
}

func (a *Analyzer) buildMapLiteral(e *ast.StructLiteral, mapTy types.MapType) *tast.MapLiteral {
	var tastElements []tast.MapElement
	for _, field := range e.Fields {
		if field.Key == nil {
			a.errorf(field.Pos_, "map literals require explicit key-value pairs")
			continue
		}

		// 1. Evaluate and check the Key
		keyNode := a.buildExpr(field.Key)
		keyType := tast.TypeOf(keyNode)
		if !keyType.Equals(mapTy.Key) && !types.IsUnknown(keyType) {
			a.errorf(field.Key.Pos(), "map key type mismatch: expected '%s', got '%s'", mapTy.Key.String(), keyType.String())
		}

		// 2. Evaluate and check the Value
		valNode := a.buildExpr(field.Value)
		valType := tast.TypeOf(valNode)
		if !valType.Equals(mapTy.Value) && !types.IsUnknown(valType) {
			a.errorf(field.Value.Pos(), "map value type mismatch: expected '%s', got '%s'", mapTy.Value.String(), valType.String())
		}
		tastElements = append(tastElements, tast.MapElement{
			Key:   keyNode,
			Value: valNode,
		})
	}
	return &tast.MapLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: tastElements,
		Type:     mapTy,
	}
}

func (a *Analyzer) buildVecLiteral(e *ast.StructLiteral, vecTy types.VectorType) *tast.VecLiteral {
	var tastElements []tast.Expr
	for _, field := range e.Fields {
		// Vectors shouldn't have named keys, just values.
		if field.Key != nil {
			a.errorf(field.Key.Pos(), "vector literals should only contain values, not key-value pairs")
		}
		valNode := a.buildExpr(field.Value)
		valType := tast.TypeOf(valNode)
		if !valType.Equals(vecTy.Base) && !types.IsUnknown(valType) {
			a.errorf(field.Value.Pos(), "vector element type mismatch: expected '%s', got '%s'", vecTy.Base.String(), valType.String())
		}
		tastElements = append(tastElements, valNode)
	}
	return &tast.VecLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: tastElements,
		Type:     vecTy,
	}
}
