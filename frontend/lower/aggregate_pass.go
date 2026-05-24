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

	// 1. Create the new empty allocation marker and TYPE IT!
	emptyArr := &ast.ArrayLiteral{Pos_: arr.Pos(), End_: arr.End()}
	p.typeMap[emptyArr] = p.typeMap[arr]

	// 2. Create the variable declaration: mut tmp = []
	decl := &ast.DeclareStmt{
		Name:    tmpName,
		Mutable: true,
		Value:   emptyArr,
		Pos_:    arr.Pos(),
	}

	var stmts []ast.Stmt
	stmts = append(stmts, decl)

	// 3. Build assignments for each element: tmp[i] = elem
	for i, elem := range arr.Elements {
		// Ensure the new identifier gets the array's type
		ident := &ast.Identifier{Value: tmpName, Pos_: arr.Pos()}
		p.typeMap[ident] = p.typeMap[arr]

		indexExpr := &ast.IndexExpr{
			Left:  ident,
			Index: &ast.IntLiteral{Value: int64(i), Pos_: arr.Pos(), End_: arr.End()},
			Pos_:  arr.Pos(),
		}
		// Safely type the index expression using the element's resolved type
		p.typeMap[indexExpr] = p.typeMap[elem]

		assign := &ast.AssignStmt{
			LValue: indexExpr,
			RValue: p.Rewrite(elem).(ast.Expr), // Recursively lower inner elements
			Pos_:   arr.Pos(),
		}
		stmts = append(stmts, assign)
	}

	// 4. Yield the populated array: => tmp
	yieldIdent := &ast.Identifier{Value: tmpName, Pos_: arr.Pos()}
	p.typeMap[yieldIdent] = p.typeMap[arr]

	yield := &ast.YieldStmt{
		Value: yieldIdent,
		Pos_:  arr.Pos(),
	}
	p.typeMap[yield] = p.typeMap[arr] // TYPE IT!

	stmts = append(stmts, yield)

	// 5. Wrap everything in a BlockStmt
	block := &ast.BlockStmt{
		Statements: stmts,
		Pos_:       arr.Pos(),
		End_:       arr.End(),
	}
	p.typeMap[block] = p.typeMap[arr] // TYPE IT!

	return block
}

func (p *AggregatePass) lowerStruct(str *ast.StructLiteral) ast.Node {
	p.counter++
	tmpName := fmt.Sprintf("_str_tmp_%d", p.counter)

	// 1. Create the new empty allocation marker and TYPE IT!
	emptyStr := &ast.StructLiteral{
		Type: str.Type,
		Pos_: str.Pos(),
		End_: str.End(),
	}
	p.typeMap[emptyStr] = p.typeMap[str]

	// 2. Create the variable declaration: mut tmp = StructType{}
	decl := &ast.DeclareStmt{
		Name:    tmpName,
		Mutable: true,
		Value:   emptyStr,
		Pos_:    str.Pos(),
	}

	var stmts []ast.Stmt
	stmts = append(stmts, decl)

	// 3. Build assignments for each field: tmp.field = value
	for _, field := range str.Fields {
		// Ensure the new identifier gets the struct's type
		ident := &ast.Identifier{Value: tmpName, Pos_: str.Pos()}
		p.typeMap[ident] = p.typeMap[str]

		fieldAccess := &ast.FieldAccess{
			Object: ident,
			Field:  field.Name,
			Pos_:   field.Pos_,
		}
		// Safely type the field access using the field value's resolved type
		p.typeMap[fieldAccess] = p.typeMap[field.Value]

		assign := &ast.AssignStmt{
			LValue: fieldAccess,
			RValue: p.Rewrite(field.Value).(ast.Expr), // Recursively lower inner fields
			Pos_:   field.Pos_,
		}
		stmts = append(stmts, assign)
	}

	// 4. Yield the populated struct: => tmp
	yieldIdent := &ast.Identifier{Value: tmpName, Pos_: str.Pos()}
	p.typeMap[yieldIdent] = p.typeMap[str]

	yield := &ast.YieldStmt{
		Value: yieldIdent,
		Pos_:  str.Pos(),
	}
	p.typeMap[yield] = p.typeMap[str] // TYPE IT!

	stmts = append(stmts, yield)

	// 5. Wrap everything in a BlockStmt
	block := &ast.BlockStmt{
		Statements: stmts,
		Pos_:       str.Pos(),
		End_:       str.End(),
	}
	p.typeMap[block] = p.typeMap[str] // TYPE IT!

	return block
}
