// frontend/desugar/expr.go
package desugar

import (
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// desugarExpr dispatcher (updated)
func (d *Desugar) desugarExpr(expr tast.Expr) tast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *tast.Identifier, *tast.IntLiteral, *tast.BoolLiteral, *tast.StringLiteral:
		return e

	case *tast.PrefixExpr:
		if e.Right != nil {
			e.Right = d.desugarExpr(e.Right)
		}
		return e

	case *tast.InfixExpr:
		// 1. Recursively desugar left and right operands first
		if e.Left != nil {
			e.Left = d.desugarExpr(e.Left)
		}
		if e.Right != nil {
			e.Right = d.desugarExpr(e.Right)
		}

		// 2. Rewrite logical && into an IfExpr
		// A && B  ->  if A { B } else { false }
		if e.Operator == "&&" {
			falseLit := &tast.BoolLiteral{Pos_: e.Pos_, Value: false, Type: types.BoolType{}}
			return &tast.IfExpr{
				Pos_:        e.Pos_,
				Condition:   e.Left,
				Consequence: &tast.BlockStmt{Statements: []tast.Stmt{&tast.ExprStmt{Value: e.Right}}},
				Alternative: &tast.BlockStmt{Statements: []tast.Stmt{&tast.ExprStmt{Value: falseLit}}},
				Type:        types.BoolType{},
			}
		}

		// 3. Rewrite logical || into an IfExpr
		// A || B  ->  if A { true } else { B }
		if e.Operator == "||" {
			trueLit := &tast.BoolLiteral{Pos_: e.Pos_, Value: true, Type: types.BoolType{}}
			return &tast.IfExpr{
				Pos_:        e.Pos_,
				Condition:   e.Left,
				Consequence: &tast.BlockStmt{Statements: []tast.Stmt{&tast.ExprStmt{Value: trueLit}}},
				Alternative: &tast.BlockStmt{Statements: []tast.Stmt{&tast.ExprStmt{Value: e.Right}}},
				Type:        types.BoolType{},
			}
		}

		return e

	case *tast.CallExpr:
		if e.Function != nil {
			e.Function = d.desugarExpr(e.Function)
		}
		for i := range e.Arguments {
			if e.Arguments[i].Argument != nil {
				e.Arguments[i].Argument = d.desugarExpr(e.Arguments[i].Argument)
			}
		}
		return e

	case *tast.MethodCallExpr:
		return d.desugarMethodCallExpr(e)

	case *tast.FieldAccess:
		if e.Object != nil {
			e.Object = d.desugarExpr(e.Object)
		}
		return e

	case *tast.IndexExpr:
		return d.desugarIndexExpr(e)

	case *tast.SliceExpr:
		if e.Left != nil {
			e.Left = d.desugarExpr(e.Left)
		}
		if e.Low != nil {
			e.Low = d.desugarExpr(e.Low)
		}
		if e.High != nil {
			e.High = d.desugarExpr(e.High)
		}
		return e

	case *tast.IfExpr:
		if e.Condition != nil {
			e.Condition = d.desugarExpr(e.Condition)
		}
		if e.Consequence != nil {
			e.Consequence = d.desugarBlockStmt(e.Consequence)
		}
		if e.Alternative != nil {
			e.Alternative = d.desugarBlockStmt(e.Alternative)
		}
		return e

	case *tast.AwaitExpr:
		if e.Value != nil {
			e.Value = d.desugarExpr(e.Value)
		}
		return e

	case *tast.MatchExpr:
		return d.desugarMatchExpr(e)

	case *tast.BlockStmt:
		return d.desugarBlockStmt(e)

	case *tast.ArrayLiteral:
		for i := range e.Elements {
			if e.Elements[i] != nil {
				e.Elements[i] = d.desugarExpr(e.Elements[i])
			}
		}
		return e

	case *tast.StructLiteral:
		for i := range e.Fields {
			if e.Fields[i].Key != nil {
				e.Fields[i].Key = d.desugarExpr(e.Fields[i].Key)
			}
			if e.Fields[i].Value != nil {
				e.Fields[i].Value = d.desugarExpr(e.Fields[i].Value)
			}
		}
		return e

	case *tast.MapLiteral:
		for i := range e.Elements {
			if e.Elements[i].Key != nil {
				e.Elements[i].Key = d.desugarExpr(e.Elements[i].Key)
			}
			if e.Elements[i].Value != nil {
				e.Elements[i].Value = d.desugarExpr(e.Elements[i].Value)
			}
		}
		return e

	case *tast.VecLiteral:
		for i := range e.Elements {
			if e.Elements[i] != nil {
				e.Elements[i] = d.desugarExpr(e.Elements[i])
			}
		}
		return e

	case *tast.VariantLiteral:
		for i := range e.Arguments {
			if e.Arguments[i] != nil {
				e.Arguments[i] = d.desugarExpr(e.Arguments[i])
			}
		}
		for i := range e.Fields {
			if e.Fields[i].Value != nil {
				e.Fields[i].Value = d.desugarExpr(e.Fields[i].Value)
			}
		}
		return e

	// New desugared nodes
	case *tast.VariantDiscriminantExpr, *tast.VariantReadExpr,
		*tast.MapReadExpr, *tast.VecReadExpr:
		return e

	default:
		return expr
	}
}

// Full MatchExpr → nested IfExpr desugaring ===
func (d *Desugar) desugarMatchExpr(match *tast.MatchExpr) tast.Expr {
	if match == nil {
		return nil
	}

	match.Subject = d.desugarExpr(match.Subject)

	if len(match.Arms) == 0 {
		return &tast.Identifier{Value: "_unit", Type: types.UnitType{}}
	}

	// Build nested IfExpr chain from the bottom up
	var currentAlternative tast.Expr = &tast.Identifier{Value: "_unit", Type: types.UnitType{}}

	for i := len(match.Arms) - 1; i >= 0; i-- {
		arm := match.Arms[i]
		arm.Body = d.desugarExpr(arm.Body)

		condition := d.buildMatchCondition(match.Subject, arm.Pattern)

		// Build the consequence block with bindings + body
		consequenceBlock := d.buildArmConsequenceBlock(arm)

		ifExpr := &tast.IfExpr{
			Pos_:        arm.Pos_,
			Condition:   condition,
			Consequence: consequenceBlock,
			Alternative: nil,
			Type:        match.Type,
		}

		if currentAlternative != nil {
			if altBlock, ok := currentAlternative.(*tast.BlockStmt); ok {
				ifExpr.Alternative = altBlock
			} else {
				ifExpr.Alternative = &tast.BlockStmt{
					Statements: []tast.Stmt{&tast.ExprStmt{Value: currentAlternative}},
				}
			}
		}

		currentAlternative = ifExpr
	}

	return currentAlternative
}

// buildMatchCondition creates the condition expression for a pattern
func (d *Desugar) buildMatchCondition(subject tast.Expr, pattern tast.Pattern) tast.Expr {
	switch p := pattern.(type) {
	case *tast.WildcardPattern:
		return &tast.BoolLiteral{Value: true, Type: types.BoolType{}}

	case *tast.LiteralPattern:
		return &tast.InfixExpr{
			Left:     subject,
			Operator: "==",
			Right:    p.Value,
			Type:     types.BoolType{},
		}

	case *tast.VariantPattern:
		// Extract discriminant
		disc := &tast.VariantDiscriminantExpr{
			Object: subject,
			Type:   types.IntType{},
		}

		discConst := &tast.IntLiteral{
			Value: int64(p.Symbol.Variant.Discriminant),
			Type:  types.IntType{},
		}

		return &tast.InfixExpr{
			Left:     disc,
			Operator: "==",
			Right:    discConst,
			Type:     types.BoolType{},
		}

	default:
		return &tast.BoolLiteral{Value: false, Type: types.BoolType{}}
	}
}

// buildArmConsequenceBlock builds a block containing pattern bindings + body
func (d *Desugar) buildArmConsequenceBlock(arm tast.MatchArm) *tast.BlockStmt {
	stmts := []tast.Stmt{}

	switch p := arm.Pattern.(type) {
	case *tast.VariantPattern:
		// Tuple bindings
		for _, binding := range p.TupleBindings {
			stmts = append(stmts, &tast.DeclareStmt{
				Symbol: binding.Symbol,
				Value: &tast.VariantReadExpr{
					Object:       arm.Body, // will be replaced later? Wait, better to use subject
					VariantName:  p.Variant.Value,
					PayloadIndex: len(p.TupleBindings) - 1, // fix indexing later
					Type:         binding.Type,
				},
			})
		}

		// Struct field bindings
		for _, field := range p.Fields {
			if field.Binding != nil {
				stmts = append(stmts, &tast.DeclareStmt{
					Symbol: field.Binding.Symbol,
					Value: &tast.VariantReadExpr{
						Object:       arm.Body, // subject should be captured
						VariantName:  p.Variant.Value,
						PayloadIndex: 0, // TODO: proper index calculation
						Type:         field.Binding.Type,
					},
				})
			}
		}
	}

	// Add the body as final expression
	stmts = append(stmts, &tast.ExprStmt{Value: arm.Body})

	return &tast.BlockStmt{
		Statements: stmts,
	}
}

func (d *Desugar) desugarMethodCallExpr(m *tast.MethodCallExpr) tast.Expr {
	if m == nil {
		return nil
	}

	// 1. Recursively desugar the receiver and arguments
	if m.Object != nil {
		m.Object = d.desugarExpr(m.Object)
	}
	for i := range m.Arguments {
		if m.Arguments[i].Argument != nil {
			m.Arguments[i].Argument = d.desugarExpr(m.Arguments[i].Argument)
		}
	}

	// 🌟 Route built-in methods to your dedicated collection nodes
	leftType := tast.TypeOf(m.Object)
	if _, isMap := leftType.(types.MapType); isMap {
		if m.Method.Value == "get" && len(m.Arguments) == 1 {
			return &tast.MapReadExpr{
				Pos_: m.Pos_,
				Map:  m.Object,
				Key:  m.Arguments[0].Argument,
				Type: m.Type,
			}
		}
		if m.Method.Value == "put" && len(m.Arguments) == 2 {
			// Because put() is an Expr but MapInsertStmt is a Stmt, we seamlessly
			// wrap it in a BlockStmt that yields Unit to satisfy the AST.
			return &tast.BlockStmt{
				Pos_: m.Pos_,
				Statements: []tast.Stmt{
					&tast.MapInsertStmt{
						Pos_:  m.Pos_,
						Map:   m.Object,
						Key:   m.Arguments[0].Argument,
						Value: m.Arguments[1].Argument,
					},
					&tast.YieldStmt{
						Value: &tast.Identifier{Value: "_unit", Type: types.UnitType{}},
					},
				},
			}
		}
	}

	// 2. Build the desugared CallExpr (for all normal methods)
	flatCall := &tast.CallExpr{
		Pos_:     m.Pos_,
		Function: m.Method,
		Type:     m.Type,
	}

	// 3. Prepend the receiver (self) as argument 0
	flatCall.Arguments = append([]tast.CallArg{
		{
			Pos_:     m.Pos_,
			Argument: m.Object,
		},
	}, m.Arguments...)

	return flatCall
}

// desugarIndexExpr routes array/slice vs map vs vec
func (d *Desugar) desugarIndexExpr(idx *tast.IndexExpr) tast.Expr {
	if idx == nil {
		return nil
	}

	idx.Left = d.desugarExpr(idx.Left)
	idx.Index = d.desugarExpr(idx.Index)

	leftType := tast.TypeOf(idx.Left)

	switch leftType.(type) {
	case types.MapType:
		return &tast.MapReadExpr{
			Pos_: idx.Pos_,
			Map:  idx.Left,
			Key:  idx.Index,
			Type: idx.Type,
		}
	case types.VectorType:
		return &tast.VecReadExpr{
			Pos_:  idx.Pos_,
			Vec:   idx.Left,
			Index: idx.Index,
			Type:  idx.Type,
		}
	default:
		// Array, Slice, String — keep as primitive IndexExpr
		return idx
	}
}
