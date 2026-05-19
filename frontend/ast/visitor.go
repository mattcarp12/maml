package ast

// --- VISITOR (Read-Only Traversal) ---

// Visitor defines a read-only AST traversal.
// If Visit returns a non-nil Visitor, that Visitor is used to walk the children.
type Visitor interface {
	Visit(node Node) Visitor
}

// Walk traverses an AST in depth-first order.
func Walk(v Visitor, node Node) {
	if node == nil {
		return
	}

	// Call the visitor on the current node. If it returns nil, stop descending.
	if v = v.Visit(node); v == nil {
		return
	}

	switch n := node.(type) {
	case *Program:
		for _, d := range n.Decls {
			Walk(v, d)
		}
	case *FnDecl:
		Walk(v, n.ReturnType)
		Walk(v, n.Body)
	case *TypeDecl:
		Walk(v, n.Name)
		Walk(v, n.Rhs)
	case *BlockStmt:
		for _, s := range n.Statements {
			Walk(v, s)
		}
	case *ExprStmt:
		Walk(v, n.Value)
	case *DeclareStmt:
		Walk(v, n.Value)
	case *AssignStmt:
		Walk(v, n.LValue)
		Walk(v, n.RValue)
	case *ReturnStmt:
		Walk(v, n.Value)
	case *YieldStmt:
		Walk(v, n.Value)
	case *ForStmt:
		Walk(v, n.Init)
		Walk(v, n.Condition)
		Walk(v, n.Post)
		Walk(v, n.Body)
	case *IfExpr:
		Walk(v, n.Condition)
		Walk(v, n.Consequence)
		if n.Alternative != nil {
			Walk(v, n.Alternative)
		}
	case *MatchExpr:
		Walk(v, n.Subject)
		for _, arm := range n.Arms {
			Walk(v, arm.Pattern)
			Walk(v, arm.Body)
		}
	case *CallExpr:
		Walk(v, n.Function)
		for _, arg := range n.Arguments {
			Walk(v, arg.Argument)
		}
	case *InfixExpr:
		Walk(v, n.Left)
		Walk(v, n.Right)
	case *PrefixExpr:
		Walk(v, n.Right)
	case *IndexExpr:
		Walk(v, n.Left)
		Walk(v, n.Index)
	case *SliceExpr:
		Walk(v, n.Left)
		Walk(v, n.Low)
		Walk(v, n.High)
	case *FieldAccess:
		Walk(v, n.Object)
		Walk(v, n.Field)
	case *ArrayLiteral:
		for _, el := range n.Elements {
			Walk(v, el)
		}
	case *StructLiteral:
		Walk(v, n.Type)
		for _, f := range n.Fields {
			Walk(v, f.Value)
		}
	case *AwaitExpr:
		Walk(v, n.Value)
	// Primitives and Types have no AST children to walk
	case *Identifier, *IntLiteral, *BoolLiteral, *StringLiteral, *WildcardPattern:
		// Nothing to walk
	}
}

// --- MUTATOR (Read-Write Traversal) ---

// Mutator defines an AST rewriting traversal.
// Mutate takes a Node and returns a Node. If it returns nil, the node is removed.
type Mutator interface {
	Mutate(node Node) Node
}

// Rewrite traverses the AST bottom-up, allowing children to be rewritten
// before their parents.
func Rewrite(m Mutator, node Node) Node {
	if node == nil {
		return nil
	}

	// 1. Rewrite children first (Bottom-up)
	switch n := node.(type) {
	case *Program:
		var newDecls []Decl
		for _, d := range n.Decls {
			if res := Rewrite(m, d); res != nil {
				newDecls = append(newDecls, res.(Decl))
			}
		}
		n.Decls = newDecls

	case *BlockStmt:
		var newStmts []Stmt
		for _, s := range n.Statements {
			if res := Rewrite(m, s); res != nil {
				newStmts = append(newStmts, res.(Stmt))
			}
		}
		n.Statements = newStmts

	case *FnDecl:
		if res := Rewrite(m, n.Body); res != nil {
			n.Body = res.(*BlockStmt)
		}

	case *ExprStmt:
		if res := Rewrite(m, n.Value); res != nil {
			n.Value = res.(Expr)
		}

	case *DeclareStmt:
		if res := Rewrite(m, n.Value); res != nil {
			n.Value = res.(Expr)
		}

	case *AssignStmt:
		if res := Rewrite(m, n.LValue); res != nil {
			n.LValue = res.(Expr)
		}
		if res := Rewrite(m, n.RValue); res != nil {
			n.RValue = res.(Expr)
		}

	case *IfExpr:
		if res := Rewrite(m, n.Condition); res != nil {
			n.Condition = res.(Expr)
		}
		if res := Rewrite(m, n.Consequence); res != nil {
			n.Consequence = res.(*BlockStmt)
		}
		if n.Alternative != nil {
			if res := Rewrite(m, n.Alternative); res != nil {
				n.Alternative = res.(*BlockStmt)
			}
		}

	case *InfixExpr:
		if res := Rewrite(m, n.Left); res != nil {
			n.Left = res.(Expr)
		}
		if res := Rewrite(m, n.Right); res != nil {
			n.Right = res.(Expr)
		}

	case *CallExpr:
		if res := Rewrite(m, n.Function); res != nil {
			n.Function = res.(Expr)
		}
		for i, arg := range n.Arguments {
			if res := Rewrite(m, arg.Argument); res != nil {
				n.Arguments[i].Argument = res.(Expr)
			}
		}
	case *AwaitExpr:
		if res := Rewrite(m, n.Value); res != nil {
			n.Value = res.(Expr)
		}
	}

	// 2. Mutate the current node
	return m.Mutate(node)
}
