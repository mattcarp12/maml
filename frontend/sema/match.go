package sema

import (
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Match Expression Builder
// =============================================================================

func (a *Analyzer) buildMatchExpr(e *ast.MatchExpr) *tast.MatchExpr {
	// 1. Evaluate the subject
	tastSubject := a.buildExpr(e.Subject)
	subjectType := typeOf(tastSubject)

	// 2. Validate that all cases are covered
	a.checkMatchExhaustiveness(e, subjectType)

	// 3. Build the arms and unify their return types
	var tastArms []tast.MatchArm
	var resultType types.Type

	for i, arm := range e.Arms {
		tastArm := a.buildMatchArm(arm, subjectType)
		tastArms = append(tastArms, tastArm)

		armType := typeOf(tastArm.Body)
		if i == 0 {
			resultType = armType
		} else {
			resultType = mergeTypes(resultType, armType)
		}
	}

	return &tast.MatchExpr{
		Pos_:    e.Pos_,
		End_:    e.End_,
		Subject: tastSubject,
		Arms:    tastArms,
		Type:    resultType,
	}
}

func (a *Analyzer) buildMatchArm(arm ast.MatchArm, subjectType types.Type) tast.MatchArm {
	// Isolate the scope so bindings don't leak to other arms
	a.pushScope()
	defer a.popScope()

	// buildPattern automatically injects destructured bindings into the local scope
	tastPattern := a.buildPattern(arm.Pattern, subjectType)
	tastBody := a.buildExpr(arm.Body)

	return tast.MatchArm{
		Pos_:    arm.Pos_,
		End_:    arm.End_,
		Pattern: tastPattern,
		Body:    tastBody,
	}
}

// =============================================================================
// Pattern Builders & Binding Injection
// =============================================================================

func (a *Analyzer) buildPattern(pat ast.Pattern, subjectType types.Type) tast.Pattern {
	if pat == nil {
		return nil
	}

	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return &tast.WildcardPattern{Pos_: p.Pos_}

	case *ast.LiteralPattern:
		tastVal := a.buildExpr(p.Value)
		valType := typeOf(tastVal)

		if !valType.Equals(subjectType) && !types.IsUnknown(valType) && !types.IsUnknown(subjectType) {
			a.errorf(p.Pos(), "pattern type mismatch: expected '%s', got '%s'", subjectType.String(), valType.String())
		}
		return &tast.LiteralPattern{Pos_: p.Pos_, End_: p.End_, Value: tastVal}

	case *ast.VariantPattern:
		return a.buildVariantPattern(p, subjectType)
	}

	return nil
}

func (a *Analyzer) buildVariantPattern(p *ast.VariantPattern, subjectType types.Type) *tast.VariantPattern {
	sumType, ok := subjectType.(*types.SumType)
	if !ok {
		if !types.IsUnknown(subjectType) {
			a.errorf(p.Pos(), "cannot match variant pattern on non-sum type '%s'", subjectType.String())
		}
		return &tast.VariantPattern{
			Pos_:    p.Pos_,
			End_:    p.End_,
			Variant: &tast.Identifier{Pos_: p.Pos_, Value: p.Binding.Value},
			Type:    types.UnknownType{},
		}
	}

	variant := sumType.GetVariant(p.Binding.Value)
	if variant == nil {
		a.errorf(p.Pos(), "variant '%s' does not exist on sum type '%s'", p.Binding.Value, sumType.BaseName)
		return &tast.VariantPattern{
			Pos_:    p.Pos_,
			End_:    p.End_,
			Variant: &tast.Identifier{Pos_: p.Pos_, Value: p.Binding.Value},
			Type:    sumType,
		}
	}

	// This symbol is permanently bound to the pattern for the backend
	sym := &types.Symbol{
		Kind:    types.VariantSymbol,
		Name:    variant.Name,
		Type:    sumType,
		SumType: sumType,
		Variant: variant,
	}

	var tastFields []tast.VariantPatternField
	for _, f := range p.Fields {
		var fieldType types.Type = types.UnknownType{}
		for _, vf := range variant.Fields {
			if vf.Name == f.Field {
				fieldType = vf.Type
				break
			}
		}

		if types.IsUnknown(fieldType) {
			a.errorf(p.Pos(), "field '%s' does not exist on variant '%s'", f.Field, variant.Name)
		}

		// CREATE THE BINDING AND INJECT IT DIRECTLY INTO THE LOCAL SCOPE
		bindingSym := &types.Symbol{
			Kind:    types.VarSymbol,
			Name:    f.Binding.Value,
			Type:    fieldType,
			Mutable: false, // Extracted pattern bindings are immutable
		}
		a.scope.symbols[f.Binding.Value] = bindingSym

		tastFields = append(tastFields, tast.VariantPatternField{
			Field: f.Field,
			Binding: &tast.Identifier{
				Pos_:   f.Binding.Pos_,
				Value:  f.Binding.Value,
				Type:   fieldType,
				Symbol: bindingSym,
			},
		})
	}

	return &tast.VariantPattern{
		Pos_:    p.Pos_,
		End_:    p.End_,
		Variant: &tast.Identifier{Pos_: p.Pos_, Value: p.Binding.Value, Type: sumType, Symbol: sym},
		Fields:  tastFields,
		Type:    sumType,
		Symbol:  sym,
	}
}

// =============================================================================
// Exhaustiveness Checking
// =============================================================================

func (a *Analyzer) checkMatchExhaustiveness(e *ast.MatchExpr, subjectType types.Type) {
	// A wildcard gracefully covers all remaining variants or arbitrary types.
	for _, arm := range e.Arms {
		if _, isWild := arm.Pattern.(*ast.WildcardPattern); isWild {
			return
		}
	}

	sumTy, ok := subjectType.(*types.SumType)
	if !ok {
		if !types.IsUnknown(subjectType) {
			a.errorf(e.Pos_, "non-exhaustive match: matching on type '%s' requires a wildcard '_' pattern", subjectType.String())
		}
		return
	}

	seen := make(map[string]bool)
	for _, arm := range e.Arms {
		if vp, ok := arm.Pattern.(*ast.VariantPattern); ok {
			seen[vp.Binding.Value] = true
		}
	}

	var missing []string
	for _, v := range sumTy.Variants {
		if !seen[v.Name] {
			missing = append(missing, v.Name)
		}
	}

	if len(missing) > 0 {
		a.errorf(e.Pos_, "non-exhaustive match: missing cases for %s", strings.Join(missing, ", "))
	}
}
