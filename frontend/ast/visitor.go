package ast

// =============================================================================
// Visitor Traversal
// =============================================================================

// Visitor defines a read-only AST traversal.
//
// If Visit returns a non-nil Visitor, traversal continues into the node's
// children using the returned visitor.
type Visitor interface {
	Visit(node Node) Visitor
}

// Walk performs a depth-first traversal of the AST.
func Walk(v Visitor, node Node) {
	if node == nil {
		return
	}

	if v = v.Visit(node); v == nil {
		return
	}

	switch n := node.(type) {

	// =========================================================================
	// Program & Declaration Nodes
	// =========================================================================

	case *Program:
		for _, d := range n.Decls {
			Walk(v, d)
		}

	case *FnDecl:
		for _, p := range n.Params {
			Walk(v, p.Type)
		}

		Walk(v, n.ReturnType)
		Walk(v, n.Body)

	case *TypeDecl:
		Walk(v, n.Name)
		Walk(v, n.Rhs)

	// =========================================================================
	// Statement Nodes
	// =========================================================================

	case *BlockStmt:
		for _, s := range n.Statements {
			Walk(v, s)
		}

	case *DeclareStmt:
		Walk(v, n.Value)

	case *AssignStmt:
		Walk(v, n.LValue)
		Walk(v, n.RValue)

	case *ReturnStmt:
		Walk(v, n.Value)

	case *YieldStmt:
		Walk(v, n.Value)

	case *ExprStmt:
		Walk(v, n.Value)

	case *ForStmt:
		Walk(v, n.Init)
		Walk(v, n.Condition)
		Walk(v, n.Post)
		Walk(v, n.Body)

	case *LoopStmt:
		Walk(v, n.Body)

	case *RetainStmt:
		Walk(v, n.Value)

	case *ReleaseStmt:
		Walk(v, n.Value)

	case *BreakStmt:
		// Leaf node.

	case *ContinueStmt:
		// Leaf node.

	// =========================================================================
	// Expression Nodes
	// =========================================================================

	case *Identifier:
		// Leaf node.

	case *IntLiteral:
		// Leaf node.

	case *BoolLiteral:
		// Leaf node.

	case *StringLiteral:
		// Leaf node.

	case *ArrayLiteral:
		for _, el := range n.Elements {
			Walk(v, el)
		}

	case *StructLiteral:
		Walk(v, n.Type)

		for _, field := range n.Fields {
			Walk(v, field.Value)
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

	case *FieldAccess:
		Walk(v, n.Object)
		Walk(v, n.Field)

	case *IndexExpr:
		Walk(v, n.Left)
		Walk(v, n.Index)

	case *SliceExpr:
		Walk(v, n.Left)
		Walk(v, n.Low)
		Walk(v, n.High)

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

	case *AwaitExpr:
		Walk(v, n.Value)

	case *StackAllocExpr:
		Walk(v, n.Value)

	case *HeapAllocExpr:
		Walk(v, n.Value)

	// =========================================================================
	// Pattern Nodes
	// =========================================================================

	case *WildcardPattern:
		// Leaf node.

	case *LiteralPattern:
		Walk(v, n.Value)

	case *VariantPattern:
		if n.Binding != nil {
			Walk(v, n.Binding)
		}

		for _, field := range n.Fields {
			if field.Binding != nil {
				Walk(v, field.Binding)
			}
		}

	// =========================================================================
	// Type Expression Nodes
	// =========================================================================

	case *NamedTypeExpr:
		Walk(v, n.Name)

	case *ArrayTypeExpr:
		Walk(v, n.Base)

	case *SliceTypeExpr:
		Walk(v, n.Base)

	case *VectorTypeExpr:
		Walk(v, n.Base)

	case *MapTypeExpr:
		Walk(v, n.Key)
		Walk(v, n.Value)

	case *TaskTypeExpr:
		Walk(v, n.Base)

	case *StructTypeExpr:
		for _, field := range n.Fields {
			Walk(v, field.Type)
		}

	case *SumTypeExpr:
		for _, arg := range n.TypeArgs {
			Walk(v, arg)
		}

		for _, variant := range n.Variants {
			for _, field := range variant.Fields {
				Walk(v, field.Type)
			}
		}

	case *FunctionTypeExpr:
		for _, param := range n.Params {
			Walk(v, param)
		}

		Walk(v, n.Return)
	}
}

// =============================================================================
// Rewriting Traversal
// =============================================================================

// Rewriter defines an AST transformation pass.
type Rewriter interface {
	Rewrite(node Node) Node
}

// Rewrite recursively rewrites an AST in depth-first order.
func Rewrite(r Rewriter, node Node) Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {

	// =========================================================================
	// Program & Declaration Nodes
	// =========================================================================

	case *Program:
		decls := make([]Decl, len(n.Decls))

		for i, d := range n.Decls {
			decls[i] = Rewrite(r, d).(Decl)
		}

		n.Decls = decls

	case *FnDecl:
		for i, p := range n.Params {
			if p.Type != nil {
				n.Params[i].Type = Rewrite(r, p.Type).(TypeExpr)
			}
		}

		if n.ReturnType != nil {
			n.ReturnType = Rewrite(r, n.ReturnType).(TypeExpr)
		}

		if n.Body != nil {
			n.Body = Rewrite(r, n.Body).(*BlockStmt)
		}

	case *TypeDecl:
		n.Name = Rewrite(r, n.Name).(*Identifier)
		n.Rhs = Rewrite(r, n.Rhs).(TypeExpr)

	// =========================================================================
	// Statement Nodes
	// =========================================================================

	case *BlockStmt:
		stmts := make([]Stmt, len(n.Statements))

		for i, s := range n.Statements {
			stmts[i] = Rewrite(r, s).(Stmt)
		}

		n.Statements = stmts

	case *DeclareStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *AssignStmt:
		n.LValue = Rewrite(r, n.LValue).(Expr)
		n.RValue = Rewrite(r, n.RValue).(Expr)

	case *ReturnStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *YieldStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *ExprStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *ForStmt:
		if n.Init != nil {
			n.Init = Rewrite(r, n.Init).(Stmt)
		}

		if n.Condition != nil {
			n.Condition = Rewrite(r, n.Condition).(Expr)
		}

		if n.Post != nil {
			n.Post = Rewrite(r, n.Post).(Stmt)
		}

		if n.Body != nil {
			n.Body = Rewrite(r, n.Body).(*BlockStmt)
		}

	case *LoopStmt:
		n.Body = Rewrite(r, n.Body).(*BlockStmt)

	case *RetainStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *ReleaseStmt:
		n.Value = Rewrite(r, n.Value).(Expr)

	// =========================================================================
	// Expression Nodes
	// =========================================================================

	case *ArrayLiteral:
		elements := make([]Expr, len(n.Elements))

		for i, e := range n.Elements {
			elements[i] = Rewrite(r, e).(Expr)
		}

		n.Elements = elements

	case *StructLiteral:
		n.Type = Rewrite(r, n.Type).(*Identifier)

		for i, f := range n.Fields {
			n.Fields[i].Value = Rewrite(r, f.Value).(Expr)
		}

	case *CallExpr:
		n.Function = Rewrite(r, n.Function).(Expr)

		for i, arg := range n.Arguments {
			n.Arguments[i].Argument =
				Rewrite(r, arg.Argument).(Expr)
		}

	case *InfixExpr:
		n.Left = Rewrite(r, n.Left).(Expr)
		n.Right = Rewrite(r, n.Right).(Expr)

	case *PrefixExpr:
		n.Right = Rewrite(r, n.Right).(Expr)

	case *FieldAccess:
		n.Object = Rewrite(r, n.Object).(Expr)
		n.Field = Rewrite(r, n.Field).(*Identifier)

	case *IndexExpr:
		n.Left = Rewrite(r, n.Left).(Expr)
		n.Index = Rewrite(r, n.Index).(Expr)

	case *SliceExpr:
		n.Left = Rewrite(r, n.Left).(Expr)

		if n.Low != nil {
			n.Low = Rewrite(r, n.Low).(Expr)
		}

		if n.High != nil {
			n.High = Rewrite(r, n.High).(Expr)
		}

	case *IfExpr:
		n.Condition = Rewrite(r, n.Condition).(Expr)
		n.Consequence = Rewrite(r, n.Consequence).(*BlockStmt)

		if n.Alternative != nil {
			n.Alternative =
				Rewrite(r, n.Alternative).(*BlockStmt)
		}

	case *MatchExpr:
		n.Subject = Rewrite(r, n.Subject).(Expr)

		arms := make([]MatchArm, len(n.Arms))

		for i, arm := range n.Arms {
			arms[i] = MatchArm{
				Pattern: Rewrite(r, arm.Pattern).(Pattern),
				Body:    Rewrite(r, arm.Body).(Expr),
			}
		}

		n.Arms = arms

	case *AwaitExpr:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *StackAllocExpr:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *HeapAllocExpr:
		n.Value = Rewrite(r, n.Value).(Expr)

	// =========================================================================
	// Pattern Nodes
	// =========================================================================

	case *LiteralPattern:
		n.Value = Rewrite(r, n.Value).(Expr)

	case *VariantPattern:
		if n.Binding != nil {
			n.Binding =
				Rewrite(r, n.Binding).(*Identifier)
		}

		for i, field := range n.Fields {
			if field.Binding != nil {
				n.Fields[i].Binding =
					Rewrite(r, field.Binding).(*Identifier)
			}
		}

	// =========================================================================
	// Type Expression Nodes
	// =========================================================================

	case *NamedTypeExpr:
		n.Name = Rewrite(r, n.Name).(*Identifier)

	case *ArrayTypeExpr:
		n.Base = Rewrite(r, n.Base).(TypeExpr)

	case *SliceTypeExpr:
		n.Base = Rewrite(r, n.Base).(TypeExpr)

	case *VectorTypeExpr:
		n.Base = Rewrite(r, n.Base).(TypeExpr)

	case *MapTypeExpr:
		n.Key = Rewrite(r, n.Key).(TypeExpr)
		n.Value = Rewrite(r, n.Value).(TypeExpr)

	case *TaskTypeExpr:
		n.Base = Rewrite(r, n.Base).(TypeExpr)

	case *StructTypeExpr:
		for i, field := range n.Fields {
			n.Fields[i].Type =
				Rewrite(r, field.Type).(TypeExpr)
		}

	case *SumTypeExpr:
		typeArgs := make([]TypeExpr, len(n.TypeArgs))

		for i, arg := range n.TypeArgs {
			typeArgs[i] = Rewrite(r, arg).(TypeExpr)
		}

		n.TypeArgs = typeArgs

		for vi, variant := range n.Variants {
			for fi, field := range variant.Fields {
				n.Variants[vi].Fields[fi].Type =
					Rewrite(r, field.Type).(TypeExpr)
			}
		}

	case *FunctionTypeExpr:
		params := make([]TypeExpr, len(n.Params))

		for i, p := range n.Params {
			params[i] = Rewrite(r, p).(TypeExpr)
		}

		n.Params = params
		n.Return = Rewrite(r, n.Return).(TypeExpr)
	}

	return r.Rewrite(node)
}
