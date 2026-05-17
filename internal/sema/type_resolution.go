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
			// Register shell
			a.scope.types[td.Name.Name] = &StructType{Name: td.Name.Name}
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
	st, ok := existingType.(*StructType)
	if !ok {
		return
	}

	if pt, ok := v.Rhs.(*ast.ProductType); ok {
		for _, f := range pt.Fields {
			fieldType := a.resolveAstType(f.Type)
			st.Fields = append(st.Fields, StructField{
				Name: f.Name,
				Type: fieldType,
			})
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

		case "Option":
			if len(t.Params) != 1 {
				a.errorf(t.Pos(), "Option expects exactly 1 type argument, got %d", len(t.Params))
				return UnknownType{}
			}
			base := a.resolveAstType(t.Params[0])
			resolvedType = OptionType{Base: base}

		case "Result":
			if len(t.Params) != 2 {
				a.errorf(t.Pos(), "Result expects exactly 2 type arguments, got %d", len(t.Params))
				return UnknownType{}
			}
			val := a.resolveAstType(t.Params[0])
			errTy := a.resolveAstType(t.Params[1])
			resolvedType = ResultType{Value: val, Error: errTy}

		default:
			a.errorf(t.Pos(), "unknown generic type symbol %s", t.Name)
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
