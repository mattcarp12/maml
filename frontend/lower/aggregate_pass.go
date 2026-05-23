package lower

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/sema"
)

// AggregatePass lowers populated Array and Struct literals into a block
// of sequential assignments on a zero-initialized allocation.
type AggregatePass struct {
	typeMap map[ast.Node]sema.Type
	counter int // Generates unique temporary variable names
}

func NewAggregatePass(typeMap map[ast.Node]sema.Type) *AggregatePass {
	return &AggregatePass{
		typeMap: typeMap,
		counter: 0,
	}
}

func (p *AggregatePass) Rewrite(node ast.Node) ast.Node {
	switch n := node.(type) {
	case *ast.ArrayLiteral:
		// If it's already empty, it's just an allocation marker. Leave it alone.
		if len(n.Elements) == 0 {
			return n
		}
		return p.lowerArray(n)
	case *ast.StructLiteral:
		if len(n.Fields) == 0 {
			return n
		}
		return p.lowerStruct(n)
	}
	return node
}

func (p *AggregatePass) lowerArray(arr *ast.ArrayLiteral) ast.Node {
	p.counter++
	tmpName := fmt.Sprintf("_arr_tmp_%d", p.counter)
	aggIdent := &ast.Identifier{Value: tmpName, Pos_: arr.Pos()}
	p.typeMap[aggIdent] = p.typeMap[arr]

	// Create the empty allocation marker
	emptyArr := &ast.ArrayLiteral{Elements: nil, Pos_: arr.Pos(), End_: arr.End_}
	p.typeMap[emptyArr] = p.typeMap[arr]

	// Declare the temporary variable (must be mutable so we can assign fields)
	decl := &ast.DeclareStmt{
		Name:    tmpName,
		Value:   emptyArr,
		Mutable: true,
		Pos_:    arr.Pos(),
	}

	stmts := []ast.Stmt{decl}

	// SEMA maps ArrayLiterals to an ArrayType. We need it to type the IndexExpr.
	// (Safely cast, assuming your SEMA uses a struct pointer or interface for Arrays)
	// We'll fall back to UnknownType if cast fails to prevent panics.
	var elementType sema.Type = &sema.UnknownType{}
	if arrTy, ok := p.typeMap[arr].(*sema.ArrayType); ok {
		elementType = arrTy.Base
	}

	for i, el := range arr.Elements {
		idxExpr := &ast.IndexExpr{
			Left:  aggIdent,
			Index: &ast.IntLiteral{Value: int64(i), Pos_: el.Pos()},
			Pos_:  el.Pos(),
		}
		p.typeMap[idxExpr] = elementType

		assign := &ast.AssignStmt{
			LValue: idxExpr,
			RValue: el,
			Pos_:   el.Pos(),
		}
		stmts = append(stmts, assign)
	}

	// Yield the populated array out of the block
	stmts = append(stmts, &ast.YieldStmt{Value: aggIdent, Pos_: arr.End()})

	block := &ast.BlockStmt{
		Statements: stmts,
		Pos_:       arr.Pos(),
		End_:       arr.End(),
	}
	p.typeMap[block] = p.typeMap[arr]

	return block
}

func (p *AggregatePass) lowerStruct(str *ast.StructLiteral) ast.Node {
	p.counter++
	tmpName := fmt.Sprintf("_str_tmp_%d", p.counter)
	aggIdent := &ast.Identifier{Value: tmpName, Pos_: str.Pos()}
	p.typeMap[aggIdent] = p.typeMap[str]

	emptyStr := &ast.StructLiteral{Type: str.Type, Fields: nil, Pos_: str.Pos(), End_: str.End_}
	p.typeMap[emptyStr] = p.typeMap[str]

	decl := &ast.DeclareStmt{
		Name:    tmpName,
		Value:   emptyStr,
		Mutable: true,
		Pos_:    str.Pos(),
	}

	stmts := []ast.Stmt{decl}

	// Lookup field types for the assignments
	strType, isStruct := p.typeMap[str].(*sema.StructType)

	for _, f := range str.Fields {
		fieldAcc := &ast.FieldAccess{
			Object: aggIdent,
			Field:  f.Name,
			Pos_:   f.Pos_,
		}

		var fType sema.Type = &sema.UnknownType{}
		if isStruct {
			for _, def := range strType.Fields {
				if def.Name == f.Name.Value {
					fType = def.Type
					break
				}
			}
		}
		p.typeMap[fieldAcc] = fType

		assign := &ast.AssignStmt{
			LValue: fieldAcc,
			RValue: f.Value,
			Pos_:   f.Pos_,
		}
		stmts = append(stmts, assign)
	}

	stmts = append(stmts, &ast.YieldStmt{Value: aggIdent, Pos_: str.End()})

	block := &ast.BlockStmt{
		Statements: stmts,
		Pos_:       str.Pos(),
		End_:       str.End(),
	}
	p.typeMap[block] = p.typeMap[str]

	return block
}
