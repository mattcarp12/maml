// sema/type_resolution.go
package sema

import "github.com/mattcarp12/maml/internal/ast"

func (a *Analyzer) discoverTypes(program *ast.Program) {
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, exists := a.scope.types[td.Name.Name]; exists {
				a.errorf(td.Pos(), "type '%s' already defined", td.Name.Name)
				continue
			}
			switch td.Rhs.(type) {
			case *ast.ProductType:
				a.scope.types[td.Name.Name] = &StructType{Name: td.Name.Name}
			case *ast.SumType:
				a.scope.types[td.Name.Name] = &SumType{Name: td.Name.Name}
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
	existingType := a.scope.types[v.Name.Name]

	switch rhs := v.Rhs.(type) {
	case *ast.ProductType:
		st, ok := existingType.(*StructType)
		if !ok {
			return
		}
		for _, f := range rhs.Fields {
			fieldType := a.resolveAstType(f.Type)
			st.Fields = append(st.Fields, StructField{Name: f.Name, Type: fieldType})
		}

	case *ast.SumType:
		st, ok := existingType.(*SumType)
		if !ok {
			return
		}
		for i, astVariant := range rhs.Variants {
			variant := SumVariant{
				Name:         astVariant.Name,
				Discriminant: i,
			}
			for _, f := range astVariant.Fields {
				fieldType := a.resolveAstType(f.Type)
				variant.Fields = append(variant.Fields, StructField{
					Name: f.Name,
					Type: fieldType,
				})
			}
			st.Variants = append(st.Variants, variant)

			// Register the variant name as a symbol in global scope so that
			// Circle{...} and Point are recognized as constructors.
			a.scope.symbols[astVariant.Name] = &Symbol{
				Kind:    VariantSymbol,
				Name:    astVariant.Name,
				Mutable: false,
				Type:    st,
				SumType: st,
				Variant: &st.Variants[i],
			}
		}
	}
}

func (a *Analyzer) resolveAstType(expr ast.TypeExpr) Type {
	var resolvedType Type = UnknownType{}

	switch t := expr.(type) {
	case *ast.NamedType:
		switch t.Name {
		case "int":
			resolvedType = IntType{}
		case "bool":
			resolvedType = BoolType{}
		case "string":
			resolvedType = StringType{}
		default:
			if found := a.lookupCustomType(t.Name); found != nil {
				resolvedType = found
			} else {
				a.errorf(t.Pos(), "unknown type %s", t.Name)
			}
		}

	// NEW: Handle []T
	case *ast.SliceType:
		baseType := a.resolveAstType(t.Base)
		resolvedType = SliceType{Base: baseType}

	// NEW: Handle [N]T
	case *ast.ArrayType:
		baseType := a.resolveAstType(t.Base)
		resolvedType = ArrayType{Base: baseType, Size: int(t.Size)}

	case *ast.GenericType:
		switch t.Name {
		case "Vec":
			if len(t.Params) != 1 {
				a.errorf(t.Pos(), "Vec expects exactly 1 type argument, got %d", len(t.Params))
				return UnknownType{}
			}
			base := a.resolveAstType(t.Params[0])
			resolvedType = VectorType{Base: base}

		case "Map":
			if len(t.Params) != 2 {
				a.errorf(t.Pos(), "Map expects exactly 2 type arguments, got %d", len(t.Params))
				return UnknownType{}
			}
			key := a.resolveAstType(t.Params[0])
			val := a.resolveAstType(t.Params[1])

			// FIXED: Strict scalar hashability enforcement
			switch key.(type) {
			case IntType, StringType, BoolType:
				// Allowed scalar keys
			default:
				a.errorf(t.Pos(), "map key type '%s' is not hashable; only int, string, and bool are permitted", key.String())
				return UnknownType{}
			}

			resolvedType = MapType{Key: key, Value: val}

		default:
			if found := a.lookupCustomType(t.Name); found != nil {
				// Generic user types not yet supported — error clearly.
				a.errorf(t.Pos(), "type '%s' is not a generic type", t.Name)
				return UnknownType{}
			}
			a.errorf(t.Pos(), "unknown generic type '%s'", t.Name)
			return UnknownType{}
		}
	}

	a.TypeMap[expr] = resolvedType
	return resolvedType
}

func (a *Analyzer) lookupCustomType(name string) Type {
	for s := a.scope; s != nil; s = s.parent {
		if custom, ok := s.types[name]; ok {
			return custom
		}
	}
	return nil
}
