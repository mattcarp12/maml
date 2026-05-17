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
