package sema

import "github.com/mattcarp12/maml/frontend/ast"

// internal/sema/expression.go

func (a *Analyzer) analyzeMatchExpr(e *ast.MatchExpr) Type {
	subjectType := a.analyzeExpr(e.Subject)

	if len(e.Arms) == 0 {
		a.errorf(e.Pos(), "match expression has no arms")
		return UnknownType{}
	}

	a.checkMatchExhaustiveness(e, subjectType)

	var yieldType Type = UnknownType{}
	for _, arm := range e.Arms {
		armType := a.analyzeMatchArm(arm, subjectType)
		if yieldType.Equals(UnknownType{}) {
			yieldType = armType
		} else if !armType.Equals(UnknownType{}) {
			if !armType.Equals(yieldType) {
				if merged := mergeTypes(yieldType, armType); merged != nil {
					yieldType = merged
				} else {
					a.errorf(arm.Body.Pos(), "match arm yields type '%s' but previous arms yield '%s'",
						armType.String(), yieldType.String())
				}
			}
		}
	}

	// NEW: Back-propagate the fully resolved yield type to all arms
	a.TypeMap[e] = yieldType
	for _, arm := range e.Arms {
		a.updateBlockYield(arm.Body, yieldType)
	}

	return yieldType
}

func (a *Analyzer) analyzeMatchArm(arm ast.MatchArm, subjectType Type) Type {
	a.pushScope()
	defer a.popScope()

	a.injectPatternBindings(arm.Pattern, subjectType)

	a.analyzeBlockStmt(arm.Body)

	return a.extractYieldType(arm.Body)
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
					st.BaseName, v.Name)
			}
		}
	case IntType, BoolType:
		a.errorf(e.Pos(),
			"non-exhaustive match: add a wildcard '_' arm to cover remaining cases")
	}
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
