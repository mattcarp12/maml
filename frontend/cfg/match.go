package cfg

import "github.com/mattcarp12/maml/frontend/ast"

func MatchHasWildcard(expr *ast.MatchExpr) bool {
	for _, arm := range expr.Arms {
		if _, ok := arm.Pattern.(*ast.WildcardPattern); ok {
			return true
		}
	}

	return false
}
