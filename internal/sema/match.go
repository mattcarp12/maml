package sema

import "github.com/mattcarp12/maml/internal/ast"

// internal/sema/expression.go

func (a *Analyzer) analyzeMatchExpr(e *ast.MatchExpr) Type {
	subjectType := a.analyzeExpr(e.Subject)

	if len(e.Arms) == 0 {
		a.errorf(e.Pos(), "match expression has no arms")
		return UnknownType{}
	}

	// Check exhaustiveness and collect arm yield types.
	a.checkMatchExhaustiveness(e, subjectType)

	var yieldType Type = UnknownType{}
	for _, arm := range e.Arms {
		// Each arm body gets its own scope with any pattern bindings injected.
		armType := a.analyzeMatchArm(arm, subjectType)
		if yieldType.Equals(UnknownType{}) {
			yieldType = armType
		} else if !armType.Equals(UnknownType{}) && !armType.Equals(yieldType) {
			a.errorf(arm.Body.Pos(), "match arm yields type '%s' but previous arms yield '%s'",
				armType.String(), yieldType.String())
		}
	}

	return yieldType
}

func (a *Analyzer) analyzeMatchArm(arm ast.MatchArm, subjectType Type) Type {
	a.pushScope()
	defer a.popScope()

	// Inject pattern bindings into the arm's scope.
	a.injectPatternBindings(arm.Pattern, subjectType)

	// Analyze arm body — re-use analyzeBlockStmt but without pushing another scope
	// since we already pushed one above for the bindings.
	alwaysReturns := false
	for _, stmt := range arm.Body.Statements {
		if a.analyzeStmt(stmt) {
			alwaysReturns = true
		}
	}
	_ = alwaysReturns

	return a.extractYieldType(arm.Body)
}

func (a *Analyzer) analyzeMatchExprAsStmt(e *ast.MatchExpr) bool {
	subjectType := a.analyzeExpr(e.Subject)

	if len(e.Arms) == 0 {
		a.errorf(e.Pos(), "match expression has no arms")
		return false
	}

	a.checkMatchExhaustiveness(e, subjectType)

	allReturn := true
	for _, arm := range e.Arms {
		a.pushScope()
		a.injectPatternBindings(arm.Pattern, subjectType)
		armReturns := false
		for _, stmt := range arm.Body.Statements {
			if a.analyzeStmt(stmt) {
				armReturns = true
			}
		}
		a.popScope()
		if !armReturns {
			allReturn = false
		}
	}

	// A match guarantees a return only if ALL arms guarantee a return
	// AND the match is exhaustive (no missing cases means no error was emitted above).
	return allReturn && a.isExhaustive(e, subjectType)
}

func (a *Analyzer) checkMatchExhaustiveness(e *ast.MatchExpr, subjectType Type) {
	// Wildcard makes any match exhaustive.
	for _, arm := range e.Arms {
		if _, isWild := arm.Pattern.(*ast.WildcardPattern); isWild {
			return
		}
	}

	presentVariants := make(map[string]bool)
	for _, arm := range e.Arms {
		if vp, ok := arm.Pattern.(*ast.VariantPattern); ok {
			presentVariants[vp.Name] = true
		}
	}

	switch st := subjectType.(type) {
	case *SumType:
		for _, v := range st.Variants {
			if !presentVariants[v.Name] {
				a.errorf(e.Pos(),
					"non-exhaustive match on '%s': missing case '%s'",
					st.Name, v.Name)
			}
		}
	case IntType, BoolType:
		a.errorf(e.Pos(),
			"non-exhaustive match: add a wildcard '_' arm to cover remaining cases")
	}
}

func (a *Analyzer) isExhaustive(e *ast.MatchExpr, subjectType Type) bool {
	for _, arm := range e.Arms {
		if _, isWild := arm.Pattern.(*ast.WildcardPattern); isWild {
			return true
		}
	}
	presentVariants := make(map[string]bool)
	for _, arm := range e.Arms {
		if vp, ok := arm.Pattern.(*ast.VariantPattern); ok {
			presentVariants[vp.Name] = true
		}
	}
	switch st := subjectType.(type) {
	case *SumType:
		for _, v := range st.Variants {
			if !presentVariants[v.Name] {
				return false
			}
		}
		return true
	}
	return false
}

func (a *Analyzer) injectPatternBindings(pat ast.Pattern, subjectType Type) {
	vp, ok := pat.(*ast.VariantPattern)
	if !ok {
		return
	}

	st, ok := subjectType.(*SumType)
	if !ok {
		return
	}

	variant := st.GetVariant(vp.Name)
	if variant == nil {
		return
	}

	// Single binding: case Circle(c) — binds whole payload as a pseudo-struct.
	// For now we inject each field individually using FieldBindings.
	// If only Binding is set (old Option/Result style), bind the first field.
	if vp.Binding != nil && len(variant.Fields) == 1 {
		a.scope.symbols[vp.Binding.Value] = &Symbol{
			Kind:    VarSymbol,
			Name:    vp.Binding.Value,
			Mutable: false,
			Type:    variant.Fields[0].Type,
		}
		return
	}

	// Field bindings: case Circle{radius: r}
	for _, fb := range vp.Fields {
		var fieldType Type = UnknownType{}
		for _, vf := range variant.Fields {
			if vf.Name == fb.Field {
				fieldType = vf.Type
				break
			}
		}
		if fb.Binding != nil {
			a.scope.symbols[fb.Binding.Value] = &Symbol{
				Kind:    VarSymbol,
				Name:    fb.Binding.Value,
				Mutable: false,
				Type:    fieldType,
			}
		}
	}
}
