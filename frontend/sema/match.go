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
	subjectType := tast.TypeOf(tastSubject)

	// 2. Validate that all cases are covered
	a.checkMatchExhaustiveness(e, subjectType)

	// 3. Build the arms and unify their return types
	var tastArms []tast.MatchArm
	var resultType types.Type

	for i, arm := range e.Arms {
		tastArm := a.buildMatchArm(arm, subjectType)
		tastArms = append(tastArms, tastArm)

		armType := tast.TypeOf(tastArm.Body)
		if i == 0 {
			resultType = armType
		} else {
			resultType = types.MergeTypes(resultType, armType)
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
		valType := tast.TypeOf(tastVal)

		if !valType.Equals(subjectType) && !types.IsUnknown(valType) && !types.IsUnknown(subjectType) {
			a.errorf(p.Pos(), "pattern type mismatch: expected '%s', got '%s'", subjectType.String(), valType.String())
		}
		return &tast.LiteralPattern{Pos_: p.Pos_, End_: p.End_, Value: tastVal}

	case *ast.CompositePattern:
		return a.buildVariantPattern(p, subjectType)

	case *ast.IdentifierPattern:
		// 1. Check if the bare identifier matches a Unit Variant on the Sum Type
		if sumType, ok := subjectType.(*types.SumType); ok {
			if variant := sumType.GetVariant(p.Name); variant != nil {
				// It's a Unit Variant! Make sure it doesn't expect a payload
				if len(variant.TupleTypes) > 0 || len(variant.Fields) > 0 {
					a.errorf(p.Pos_, "variant '%s' requires a payload layout constructor", p.Name)
				}

				variantIdent := &tast.Identifier{Pos_: p.Pos_, Value: p.Name, Type: sumType}
				sym := &types.Symbol{
					Kind:    types.VariantSymbol,
					Name:    variant.Name,
					Type:    sumType,
					SumType: sumType,
					Variant: variant,
				}
				variantIdent.Symbol = sym

				return &tast.VariantPattern{
					Pos_:    p.Pos_,
					End_:    p.Pos_,
					Variant: variantIdent,
					Type:    sumType,
					Symbol:  sym,
				}
			}
		}

		// 2. If it's not a unit variant, it falls through to a catch-all variable binding assignment!
		if _, exists := a.scope.symbols[p.Name]; exists {
			a.errorf(p.Pos_, "variable '%s' is already bound in this pattern", p.Name)
		}

		bindingSym := &types.Symbol{
			Kind:    types.VarSymbol,
			Name:    p.Name,
			Type:    subjectType, // The variable captures the whole outer type
			Mutable: false,
		}
		a.scope.symbols[p.Name] = bindingSym

		return &tast.IdentifierPattern{Pos_: p.Pos_, Name: p.Name}
	}

	return nil
}

func (a *Analyzer) buildVariantPattern(p *ast.CompositePattern, subjectType types.Type) *tast.VariantPattern {
	if p == nil {
		return nil
	}

	namedType, ok := p.TypeExpr.(*ast.NamedTypeExpr)
	if !ok {
		a.errorf(p.Pos_, "invalid type layout in composite pattern")
		return nil
	}
	variantName := namedType.Name.Value
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

	var tastTupleBindings []*tast.Identifier
	var tastFields []tast.VariantPatternField

	tupleIdx := 0

	for _, elem := range p.Elements {
		if elem.Key == nil {
			// --- Tuple Variant Payload Destructuring ---
			var fieldType types.Type = types.UnknownType{}
			if tupleIdx < len(variant.TupleTypes) {
				fieldType = variant.TupleTypes[tupleIdx]
			} else {
				a.errorf(elem.Pos_, "extra positional argument in tuple variant '%s'", variantName)
			}

			if identPat, isIdent := elem.Pattern.(*ast.IdentifierPattern); isIdent {
				bindingName := identPat.Name

				if _, exists := a.scope.symbols[bindingName]; exists {
					a.errorf(elem.Pos_, "variable '%s' is already bound in this pattern", bindingName)
				}

				bindingSym := &types.Symbol{
					Kind:    types.VarSymbol,
					Name:    bindingName,
					Type:    fieldType,
					Mutable: false,
				}
				a.scope.symbols[bindingName] = bindingSym

				tastTupleBindings = append(tastTupleBindings, &tast.Identifier{
					Pos_:   elem.Pos_,
					Value:  bindingName,
					Type:   fieldType,
					Symbol: bindingSym,
				})
			} else {
				a.errorf(elem.Pos_, "nested complex sub-patterns are currently unsupported inside tuple variants")
			}
			tupleIdx++
		} else {
			// --- Struct Variant Payload Destructuring ---
			keyIdent, ok := elem.Key.(*ast.Identifier)
			if !ok {
				a.errorf(elem.Pos_, "struct pattern key must be a valid identifier")
				continue
			}
			fieldName := keyIdent.Value

			var fieldType types.Type = types.UnknownType{}
			for _, vf := range variant.Fields {
				if vf.Name == fieldName {
					fieldType = vf.Type
					break
				}
			}

			if types.IsUnknown(fieldType) {
				a.errorf(elem.Pos_, "field '%s' does not exist on variant '%s'", fieldName, variant.Name)
			}

			var fieldBindingIdent *tast.Identifier
			if identPat, isIdent := elem.Pattern.(*ast.IdentifierPattern); isIdent {
				bindingName := identPat.Name

				if _, exists := a.scope.symbols[bindingName]; exists {
					a.errorf(elem.Pos_, "variable '%s' is already bound in this pattern", bindingName)
				}

				bindingSym := &types.Symbol{
					Kind:    types.VarSymbol,
					Name:    bindingName,
					Type:    fieldType,
					Mutable: false,
				}
				a.scope.symbols[bindingName] = bindingSym

				fieldBindingIdent = &tast.Identifier{
					Pos_:   elem.Pos_,
					Value:  bindingName,
					Type:   fieldType,
					Symbol: bindingSym,
				}
			} else {
				a.errorf(elem.Pos_, "nested complex sub-patterns are currently unsupported inside struct fields")
			}

			tastFields = append(tastFields, tast.VariantPatternField{
				Field:   fieldName,
				Binding: fieldBindingIdent,
			})
		}
	}

	if len(variant.TupleTypes) > 0 && tupleIdx != len(variant.TupleTypes) {
		a.errorf(p.Pos_, "wrong number of bindings for tuple variant '%s': expected %d, got %d",
			variantName, len(variant.TupleTypes), tupleIdx)
	}

	return &tast.VariantPattern{
		Pos_:          p.Pos_,
		End_:          p.End_,
		Variant:       variantIdent,
		TupleBindings: tastTupleBindings,
		Fields:        tastFields,
		Type:          sumType,
		Symbol:        sym,
	}
}

// =============================================================================
// Exhaustiveness Checking
// =============================================================================

func (a *Analyzer) checkMatchExhaustiveness(e *ast.MatchExpr, subjectType types.Type) {
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
		switch pat := arm.Pattern.(type) {

		case *ast.CompositePattern:
			namedType, ok := pat.TypeExpr.(*ast.NamedTypeExpr)
			if !ok {
				continue
			}
			if variantName := namedType.Name.Value; variantName != "" {
				seen[variantName] = true
			}

		case *ast.IdentifierPattern:
			if sumTy.GetVariant(pat.Name) != nil {
				seen[pat.Name] = true
			} else {
				return // catch-all binding covers all remaining variants
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
