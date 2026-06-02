package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Helper: AST -> Semantic Type Translation
// =============================================================================

func (a *Analyzer) resolveAstType(expr ast.TypeExpr) types.Type {
	if expr == nil {
		return types.UnknownType{}
	}
	switch e := expr.(type) {
	case *ast.NamedTypeExpr:
		switch e.Name.Value {
		case "int":
			return types.IntType{}
		case "bool":
			return types.BoolType{}
		case "string":
			return types.StringType{}
		case "unit":
			return types.UnitType{}
		case "any":
			return types.AnyType{}
		default:
			if custom := a.lookupCustomType(e.Name.Value); custom != nil {
				return custom
			}
			a.errorf(e.Pos(), "unknown type %s", e.Name.Value)
			return types.UnknownType{}
		}
	case *ast.ArrayTypeExpr:
		return types.ArrayType{
			Base: a.resolveAstType(e.Base),
			Size: int(e.Size),
		}
	case *ast.GenericTypeExpr:
		return a.resolveBuiltinGeneric(e)
	}

	return types.UnknownType{}
}

func (a *Analyzer) lookupCustomType(name string) types.Type {
	for s := a.scope; s != nil; s = s.parent {
		if custom, ok := s.types[name]; ok {
			return custom
		}
	}
	return nil
}
