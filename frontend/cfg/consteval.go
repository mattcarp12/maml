package cfg

import "github.com/mattcarp12/maml/frontend/ast"

type ConstBool struct {
	Known bool
	Value bool
}

func EvalBool(expr ast.Expr) ConstBool {
	switch e := expr.(type) {

	case *ast.BoolLiteral:
		return ConstBool{
			Known: true,
			Value: e.Value,
		}

	case *ast.PrefixExpr:
		if e.Operator == "!" {
			inner := EvalBool(e.Right)

			if inner.Known {
				return ConstBool{
					Known: true,
					Value: !inner.Value,
				}
			}
		}

	case *ast.InfixExpr:
		left := EvalBool(e.Left)
		right := EvalBool(e.Right)

		if left.Known && right.Known {
			switch e.Operator {

			case "&&":
				return ConstBool{
					Known: true,
					Value: left.Value && right.Value,
				}

			case "||":
				return ConstBool{
					Known: true,
					Value: left.Value || right.Value,
				}

			case "==":
				return ConstBool{
					Known: true,
					Value: left.Value == right.Value,
				}

			case "!=":
				return ConstBool{
					Known: true,
					Value: left.Value != right.Value,
				}
			}
		}
	}

	return ConstBool{}
}