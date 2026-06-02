// frontend/desugar/stmt.go
package desugar

import (
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// desugarStmt dispatches to the appropriate statement desugarer.
func (d *Desugar) desugarStmt(stmt tast.Stmt) tast.Stmt {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *tast.BlockStmt:
		return d.desugarBlockStmt(s)
	case *tast.DeclareStmt:
		return d.desugarDeclareStmt(s)
	case *tast.AssignStmt:
		return d.desugarAssignStmt(s)
	case *tast.ReturnStmt:
		return d.desugarReturnStmt(s)
	case *tast.YieldStmt:
		return d.desugarYieldStmt(s)
	case *tast.ExprStmt:
		return d.desugarExprStmt(s)
	case *tast.ForStmt:
		return d.desugarForStmt(s)
	case *tast.BreakStmt:
		return s
	case *tast.ContinueStmt:
		return s
	default:
		return stmt
	}
}

func (d *Desugar) desugarDeclareStmt(s *tast.DeclareStmt) tast.Stmt {
	if s.Value != nil {
		s.Value = d.desugarExpr(s.Value)
	}
	return s
}

func (d *Desugar) desugarAssignStmt(s *tast.AssignStmt) tast.Stmt {
	if idx, ok := s.LValue.(*tast.IndexExpr); ok {
		leftType := tast.TypeOf(idx.Left)
		if _, isMap := leftType.(types.MapType); isMap {
			// Desugar all three operands before constructing the node.
			mapExpr := d.desugarExpr(idx.Left)
			keyExpr := d.desugarExpr(idx.Index)
			valExpr := d.desugarExpr(s.RValue)

			return &tast.MapInsertStmt{
				Pos_:  s.Pos_,
				Map:   mapExpr,
				Key:   keyExpr,
				Value: valExpr,
			}
		}
	}
	if s.LValue != nil {
		s.LValue = d.desugarExpr(s.LValue)
	}
	if s.RValue != nil {
		s.RValue = d.desugarExpr(s.RValue)
	}
	return s
}

func (d *Desugar) desugarReturnStmt(s *tast.ReturnStmt) tast.Stmt {
	if s.Value != nil {
		s.Value = d.desugarExpr(s.Value)
	}
	return s
}

func (d *Desugar) desugarYieldStmt(s *tast.YieldStmt) tast.Stmt {
	if s.Value != nil {
		s.Value = d.desugarExpr(s.Value)
	}
	return s
}

func (d *Desugar) desugarExprStmt(s *tast.ExprStmt) tast.Stmt {
	if s.Value != nil {
		s.Value = d.desugarExpr(s.Value)
	}
	return s
}

func (d *Desugar) desugarForStmt(forStmt *tast.ForStmt) tast.Stmt {
	if forStmt == nil {
		return nil
	}

	// 1. Desugar nested parts first
	if forStmt.Init != nil {
		forStmt.Init = d.desugarStmt(forStmt.Init)
	}
	if forStmt.Condition != nil {
		forStmt.Condition = d.desugarExpr(forStmt.Condition)
	}
	if forStmt.Post != nil {
		forStmt.Post = d.desugarStmt(forStmt.Post)
	}
	if forStmt.Body != nil {
		forStmt.Body = d.desugarBlockStmt(forStmt.Body)
	}

	// 2. If this is already a while-style loop (Init and Post are both nil),
	//    no further transformation needed.
	if forStmt.Init == nil && forStmt.Post == nil {
		return forStmt
	}

	// 3. Transform classic C-style for-loop into while form:
	//
	//   for init; cond; post { body }
	//     →
	//   {
	//     init;
	//     while cond {
	//       body
	//       post
	//     }
	//   }
	//
	// We represent "while" as ForStmt{Init: nil, Post: nil}

	whileBodyStmts := []tast.Stmt{}

	// Add original body statements
	if forStmt.Body != nil {
		whileBodyStmts = append(whileBodyStmts, forStmt.Body.Statements...)
	}

	// Add post statement (e.g. i++)
	if forStmt.Post != nil {
		whileBodyStmts = append(whileBodyStmts, forStmt.Post)
	}

	whileBlock := &tast.BlockStmt{
		Pos_:       forStmt.Body.Pos_,
		End_:       forStmt.Body.End_,
		Statements: whileBodyStmts,
	}

	whileLoop := &tast.ForStmt{
		Pos_:      forStmt.Pos_,
		Condition: forStmt.Condition,
		Body:      whileBlock,
		// Init and Post are deliberately nil → this is now a while loop
	}

	// If there was an Init statement, wrap everything in a block
	if forStmt.Init != nil {
		outerBlock := &tast.BlockStmt{
			Pos_:       forStmt.Pos_,
			End_:       forStmt.End(),
			Statements: []tast.Stmt{forStmt.Init, whileLoop},
		}
		return outerBlock
	}

	return whileLoop
}
