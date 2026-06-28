package sema

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

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
			return types.I64Type{} // Assuming 64-bit platform default for 'int'
		case "i8":
			return types.I8Type{}
		case "i16":
			return types.I16Type{}
		case "i32":
			return types.I32Type{}
		case "i64":
			return types.I64Type{}
		case "i128":
			return types.I128Type{}
		case "u8", "byte": // 'byte' is an alias for 'u8'
			return types.U8Type{}
		case "u16":
			return types.U16Type{}
		case "u32":
			return types.U32Type{}
		case "u64":
			return types.U64Type{}
		case "u128":
			return types.U128Type{}
		case "f32":
			return types.F32Type{}
		case "f64":
			return types.F64Type{}
		case "bool":
			return types.BoolType{}
		case "char":
			return types.CharType{}
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
		return &types.ArrayType{
			Base: a.resolveAstType(e.Base),
			Size: int(e.Size),
		}
	case *ast.GenericTypeExpr:
		return a.resolveBuiltinGeneric(e)
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

func (a *Analyzer) lookupStruct(name string) *types.StructType {
	t := a.lookupCustomType(name)
	if t == nil {
		return nil
	}
	st, _ := t.(*types.StructType)
	return st
}

func (a *Analyzer) getMissingFields(st *types.StructType, seen map[string]bool) []string {
	var missing []string
	for _, f := range st.Fields {
		if !seen[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	return missing
}
