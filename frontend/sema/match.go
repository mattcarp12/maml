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
	if p == nil {
		return nil
	}

	// 1. FIX THE BUG: Look up the variant using the PATTERN NAME, not the variable binding!
	variantName := p.Name
	variantIdent := &tast.Identifier{Pos_: p.Pos_, Value: variantName}

	sumType, ok := subjectType.(*types.SumType)
	if !ok {
		if !types.IsUnknown(subjectType) {
			a.errorf(p.Pos_, "cannot match variant pattern on non-sum type '%s'", subjectType.String())
		}
		variantIdent.Type = types.UnknownType{}
		return &tast.VariantPattern{Pos_: p.Pos_, End_: p.End_, Variant: variantIdent, Type: types.UnknownType{}}
	}

	variant := sumType.GetVariant(variantName)
	if variant == nil {
		a.errorf(p.Pos_, "variant '%s' does not exist on sum type '%s'", variantName, sumType.BaseName)
		variantIdent.Type = sumType
		return &tast.VariantPattern{Pos_: p.Pos_, End_: p.End_, Variant: variantIdent, Type: sumType}
	}

	sym := &types.Symbol{
		Kind:    types.VariantSymbol,
		Name:    variant.Name,
		Type:    sumType,
		SumType: sumType,
		Variant: variant,
	}
	variantIdent.Symbol = sym
	variantIdent.Type = sumType

	// 2. Handle Tuple Destructuring (NEW!)
	var tastTupleBindings []*tast.Identifier
	if len(p.TupleBindings) > 0 {
		// Arity Check: Did they provide the right number of variables?
		if len(p.TupleBindings) != len(variant.TupleTypes) {
			a.errorf(p.Pos_, "wrong number of bindings for tuple variant '%s': expected %d, got %d",
				variantName, len(variant.TupleTypes), len(p.TupleBindings))
		}

		for i, bindingAST := range p.TupleBindings {
			bindingName := bindingAST.Value
			var fieldType types.Type = types.UnknownType{}

			if i < len(variant.TupleTypes) {
				fieldType = variant.TupleTypes[i]
			}

			// Register the destructured variable into the local scope so the match body can use it!
			bindingSym := &types.Symbol{
				Kind:    types.VarSymbol,
				Name:    bindingName,
				Type:    fieldType,
				Mutable: false, // Pattern bindings are immutable by default in Rust/MAML
			}
			a.scope.symbols[bindingName] = bindingSym

			tastTupleBindings = append(tastTupleBindings, &tast.Identifier{
				Pos_:   bindingAST.Pos(),
				Value:  bindingName,
				Type:   fieldType,
				Symbol: bindingSym,
			})
		}
	}

	// 3. Handle Struct Destructuring (Existing code, updated slightly)
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
			a.errorf(p.Pos_, "field '%s' does not exist on variant '%s'", f.Field, variant.Name)
		}

		var fieldBindingIdent *tast.Identifier
		if f.Binding != nil {
			bindingName := f.Binding.Value
			bindingSym := &types.Symbol{
				Kind:    types.VarSymbol,
				Name:    bindingName,
				Type:    fieldType,
				Mutable: false,
			}
			a.scope.symbols[bindingName] = bindingSym
			fieldBindingIdent = &tast.Identifier{
				Pos_:   f.Binding.Pos_,
				Value:  bindingName,
				Type:   fieldType,
				Symbol: bindingSym,
			}
		}

		tastFields = append(tastFields, tast.VariantPatternField{
			Field:   f.Field,
			Binding: fieldBindingIdent,
		})
	}

	return &tast.VariantPattern{
		Pos_:          p.Pos_,
		End_:          p.End_,
		Variant:       variantIdent,
		TupleBindings: tastTupleBindings, // Attached to TAST!
		Fields:        tastFields,
		Type:          sumType,
		Symbol:        sym,
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
		if vp, ok := arm.Pattern.(*ast.VariantPattern); ok && vp != nil {
			if vp.Name != "" {
				seen[vp.Name] = true
			}
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
