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
	"Future": {
		Arity: 1,
		Build: func(args []types.Type) types.Type {
			return types.FutureType{
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

func (a *Analyzer) mapMapLiteral(e *ast.CompositeLiteral, mapTy types.MapType) *tast.MapLiteral {
	var tastElements []tast.MapElement
	for _, elems := range e.Elements {
		if elems.Key == nil {
			// Structural requirement, safe to keep as a direct error
			a.errorf(elems.Pos_, "map literals require explicit key-value pairs")
			continue
		}

		keyNode := a.mapExpr(elems.Key)
		valNode := a.mapExpr(elems.Value)

		tastElements = append(tastElements, tast.MapElement{
			Key:   keyNode,
			Value: valNode,
		})
	}

	node := &tast.MapLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: tastElements,
		Type:     mapTy,
	}

	// Fire the rules!
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}

func (a *Analyzer) mapVecLiteral(e *ast.CompositeLiteral, vecTy types.VectorType) *tast.VecLiteral {
	var tastElements []tast.Expr
	for _, elems := range e.Elements {
		if elems.Key != nil {
			// Structural requirement
			a.errorf(elems.Key.Pos(), "vector literals should only contain values, not key-value pairs")
		}
		valNode := a.mapExpr(elems.Value)
		tastElements = append(tastElements, valNode)
	}

	node := &tast.VecLiteral{
		Pos_:     e.Pos_,
		End_:     e.End_,
		Elements: tastElements,
		Type:     vecTy,
	}

	// Fire the rules!
	a.drainViolations(a.registry.Check(node, a.ctx()))
	return node
}
