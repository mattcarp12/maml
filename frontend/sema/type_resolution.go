package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Pass 1: Type Hoisting
// =============================================================================

func (a *Analyzer) discoverTypes(program *ast.Program) {
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, exists := a.scope.types[td.Name.Value]; exists {
				a.errorf(td.Pos(), "type '%s' already defined", td.Name.Value)
				continue
			}
			switch td.Rhs.(type) {
			case *ast.StructTypeExpr:
				a.scope.types[td.Name.Value] = &types.StructType{Name: td.Name.Value}
			case *ast.SumTypeExpr:
				a.scope.types[td.Name.Value] = &types.SumType{BaseName: td.Name.Value}
			}
		}
	}
}

func (a *Analyzer) resolveTypeBodies(program *ast.Program) {
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			a.resolveTypeBody(td)
		}
	}
}

func (a *Analyzer) resolveTypeBody(v *ast.TypeDecl) {
	existingType := a.scope.types[v.Name.Value]

	switch rhs := v.Rhs.(type) {
	case *ast.StructTypeExpr:
		st := existingType.(*types.StructType)
		for _, f := range rhs.Fields {
			st.Fields = append(st.Fields, types.StructField{
				Name: f.Name,
				Type: a.resolveAstType(f.Type),
			})
		}
	case *ast.SumTypeExpr:
		st := existingType.(*types.SumType)
		for i, v := range rhs.Variants {
			variant := types.SumVariant{
				Name:         v.Name,
				Discriminant: i,
			}
			for _, f := range v.Fields {
				variant.Fields = append(variant.Fields, types.StructField{
					Name: f.Name,
					Type: a.resolveAstType(f.Type),
				})
			}
			for _, tf := range v.TupleFields {
				variant.TupleTypes = append(variant.TupleTypes, a.resolveAstType(tf))
			}
			st.Variants = append(st.Variants, variant)

			// Register the variant constructor in the symbol table
			a.scope.symbols[v.Name] = &types.Symbol{
				Kind:    types.VariantSymbol,
				Name:    v.Name,
				Type:    st,
				SumType: st,
				Variant: &st.Variants[i],
			}
		}
	}
}

// =============================================================================
// Pass 2: TAST Construction
// =============================================================================

func (a *Analyzer) buildTypeDecl(td *ast.TypeDecl) *tast.TypeDecl {
	// 1. Retrieve the fully resolved type from Pass 1
	resolvedType := a.lookupCustomType(td.Name.Value)

	// 2. Create the permanently bound symbol for this type declaration
	sym := &types.Symbol{
		Kind: types.VarSymbol,
		Name: td.Name.Value,
		Type: resolvedType,
	}

	// 3. Construct the TAST node, completely discarding the AST Rhs tree
	return &tast.TypeDecl{
		Pos_: td.Pos_,
		End_: td.End_,
		Name: &tast.Identifier{
			Pos_:  td.Name.Pos_,
			Value: td.Name.Value,
			Type:  resolvedType,
		},
		Type:   resolvedType,
		Symbol: sym,
	}
}

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
	case *ast.SliceTypeExpr:
		return types.SliceType{
			Base: a.resolveAstType(e.Base),
		}
	case *ast.TaskTypeExpr:
		return types.TaskType{
			Base: a.resolveAstType(e.Base),
		}
	case *ast.MapTypeExpr:
		return types.MapType{
			Key:   a.resolveAstType(e.Key),
			Value: a.resolveAstType(e.Value),
		}
	case *ast.VectorTypeExpr:
		return types.VectorType{
			Base: a.resolveAstType(e.Base),
		}
	case *ast.OptionTypeExpr:
		return types.NewOptionType(a.resolveAstType(e.Base))
	case *ast.ResultTypeExpr:
		return types.NewResultType(a.resolveAstType(e.Ok), a.resolveAstType(e.Err))
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
