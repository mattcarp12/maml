package ast

// =============================================================================
// Helper Utilities
// =============================================================================

func cloneExpr(expr Expr) Expr {
	if expr == nil {
		return nil
	}

	return expr.Clone().(Expr)
}

func cloneStmt(stmt Stmt) Stmt {
	if stmt == nil {
		return nil
	}

	return stmt.Clone().(Stmt)
}

func cloneDecl(decl Decl) Decl {
	if decl == nil {
		return nil
	}

	return decl.Clone().(Decl)
}

func cloneTypeExpr(t TypeExpr) TypeExpr {
	if t == nil {
		return nil
	}

	return t.Clone().(TypeExpr)
}

func clonePattern(p Pattern) Pattern {
	if p == nil {
		return nil
	}

	return p.Clone().(Pattern)
}

func cloneBlock(block *BlockStmt) *BlockStmt {
	if block == nil {
		return nil
	}

	return block.Clone().(*BlockStmt)
}

// =============================================================================
// Program & Declaration Clones
// =============================================================================

func (p *Program) Clone() Node {
	decls := make([]Decl, len(p.Decls))

	for i, d := range p.Decls {
		decls[i] = cloneDecl(d)
	}

	return &Program{
		Decls: decls,
	}
}

func (f *FnDecl) Clone() Node {
	params := make([]*Param, len(f.Params))

	for i, p := range f.Params {
		params[i] = p.Clone().(*Param)
	}

	return &FnDecl{
		Pos_:       f.Pos_,
		Name:       f.Name,
		IsAsync:    f.IsAsync,
		Params:     params,
		ReturnType: cloneTypeExpr(f.ReturnType),
		Body:       cloneBlock(f.Body),
	}
}

func (p *Param) Clone() Node {
	return &Param{
		Pos_: p.Pos_,
		Name: p.Name,
		Type: cloneTypeExpr(p.Type),
		Mut:  p.Mut,
		Own:  p.Own,
	}
}

func (t *TypeDecl) Clone() Node {
	return &TypeDecl{
		Pos_: t.Pos_,
		Name: t.Name.Clone().(*Identifier),
		Rhs:  cloneTypeExpr(t.Rhs),
	}
}

// =============================================================================
// Statement Clones
// =============================================================================

func (b *BlockStmt) Clone() Node {
	stmts := make([]Stmt, len(b.Statements))

	for i, s := range b.Statements {
		stmts[i] = cloneStmt(s)
	}

	return &BlockStmt{
		Pos_:       b.Pos_,
		End_:       b.End_,
		Statements: stmts,
	}
}

func (d *DeclareStmt) Clone() Node {
	return &DeclareStmt{
		Pos_:    d.Pos_,
		Name:    d.Name,
		Mutable: d.Mutable,
		Value:   cloneExpr(d.Value),
	}
}

func (a *AssignStmt) Clone() Node {
	return &AssignStmt{
		Pos_:   a.Pos_,
		LValue: cloneExpr(a.LValue),
		RValue: cloneExpr(a.RValue),
	}
}

func (r *ReturnStmt) Clone() Node {
	return &ReturnStmt{
		Pos_:  r.Pos_,
		Value: cloneExpr(r.Value),
	}
}

func (y *YieldStmt) Clone() Node {
	return &YieldStmt{
		Pos_:  y.Pos_,
		Value: cloneExpr(y.Value),
	}
}

func (e *ExprStmt) Clone() Node {
	return &ExprStmt{
		Pos_:  e.Pos_,
		Value: cloneExpr(e.Value),
	}
}

func (f *ForStmt) Clone() Node {
	return &ForStmt{
		Pos_:      f.Pos_,
		Init:      cloneStmt(f.Init),
		Condition: cloneExpr(f.Condition),
		Post:      cloneStmt(f.Post),
		Body:      cloneBlock(f.Body),
	}
}

func (b *BreakStmt) Clone() Node {
	return &BreakStmt{
		Token: b.Token,
	}
}

func (c *ContinueStmt) Clone() Node {
	return &ContinueStmt{
		Token: c.Token,
	}
}

func (l *LoopStmt) Clone() Node {
	return &LoopStmt{
		Pos_: l.Pos_,
		End_: l.End_,
		Body: cloneBlock(l.Body),
	}
}

func (r *RetainStmt) Clone() Node {
	return &RetainStmt{
		Pos_:  r.Pos_,
		Value: cloneExpr(r.Value),
	}
}

func (r *ReleaseStmt) Clone() Node {
	return &ReleaseStmt{
		Pos_:  r.Pos_,
		Value: cloneExpr(r.Value),
	}
}

// =============================================================================
// Expression Clones
// =============================================================================

func (i *Identifier) Clone() Node {
	return &Identifier{
		Pos_:  i.Pos_,
		Value: i.Value,
	}
}

func (i *IntLiteral) Clone() Node {
	return &IntLiteral{
		Pos_:  i.Pos_,
		End_:  i.End_,
		Value: i.Value,
	}
}

func (b *BoolLiteral) Clone() Node {
	return &BoolLiteral{
		Pos_:  b.Pos_,
		Value: b.Value,
	}
}

func (s *StringLiteral) Clone() Node {
	return &StringLiteral{
		Pos_:  s.Pos_,
		Value: s.Value,
	}
}

func (s *StructField) Clone() StructField {
	return StructField{
		Name:  s.Name.Clone().(*Identifier),
		Value: cloneExpr(s.Value),
	}
}

func (s *StructLiteral) Clone() Node {
	fields := make([]StructField, len(s.Fields))

	for i, f := range s.Fields {
		fields[i] = f.Clone()
	}

	return &StructLiteral{
		Pos_:   s.Pos_,
		End_:   s.End_,
		Type:   s.Type.Clone().(*Identifier),
		Fields: fields,
	}
}

func (a *ArrayLiteral) Clone() Node {
	elements := make([]Expr, len(a.Elements))

	for i, e := range a.Elements {
		elements[i] = cloneExpr(e)
	}

	return &ArrayLiteral{
		Pos_:     a.Pos_,
		End_:     a.End_,
		Elements: elements,
	}
}

func (c *CallExpr) Clone() Node {
	args := make([]CallArg, len(c.Arguments))

	for i, a := range c.Arguments {
		args[i] = a.Clone()
	}

	return &CallExpr{
		Pos_:      c.Pos_,
		Function:  cloneExpr(c.Function),
		Arguments: args,
	}
}

func (c *CallArg) Clone() CallArg {
	return CallArg{
		Pos_:     c.Pos_,
		Argument: cloneExpr(c.Argument),
		Mut:      c.Mut,
		Own:      c.Own,
	}
}

func (i *InfixExpr) Clone() Node {
	return &InfixExpr{
		Pos_:     i.Pos_,
		Left:     cloneExpr(i.Left),
		Operator: i.Operator,
		Right:    cloneExpr(i.Right),
	}
}

func (p *PrefixExpr) Clone() Node {
	return &PrefixExpr{
		Pos_:     p.Pos_,
		Operator: p.Operator,
		Right:    cloneExpr(p.Right),
	}
}

func (f *FieldAccess) Clone() Node {
	return &FieldAccess{
		Pos_:   f.Pos_,
		Object: cloneExpr(f.Object),
		Field:  f.Field.Clone().(*Identifier),
	}
}

func (i *IndexExpr) Clone() Node {
	return &IndexExpr{
		Pos_:  i.Pos_,
		Left:  cloneExpr(i.Left),
		Index: cloneExpr(i.Index),
	}
}

func (s *SliceExpr) Clone() Node {
	return &SliceExpr{
		Pos_: s.Pos_,
		Left: cloneExpr(s.Left),
		Low:  cloneExpr(s.Low),
		High: cloneExpr(s.High),
	}
}

func (i *IfExpr) Clone() Node {
	return &IfExpr{
		Pos_:        i.Pos_,
		Condition:   cloneExpr(i.Condition),
		Consequence: cloneBlock(i.Consequence),
		Alternative: cloneBlock(i.Alternative),
	}
}

func (a *AwaitExpr) Clone() Node {
	return &AwaitExpr{
		Pos_:  a.Pos_,
		Value: cloneExpr(a.Value),
	}
}

func (s *StackAllocExpr) Clone() Node {
	return &StackAllocExpr{
		Pos_:  s.Pos_,
		Value: cloneExpr(s.Value),
	}
}

func (h *HeapAllocExpr) Clone() Node {
	return &HeapAllocExpr{
		Pos_:  h.Pos_,
		Value: cloneExpr(h.Value),
	}
}

// =============================================================================
// Match Expression Clones
// =============================================================================

func (m *MatchExpr) Clone() Node {
	arms := make([]MatchArm, len(m.Arms))

	for i, arm := range m.Arms {
		arms[i] = arm.Clone()
	}

	return &MatchExpr{
		Pos_:    m.Pos_,
		Subject: cloneExpr(m.Subject),
		Arms:    arms,
	}
}

func (m *MatchArm) Clone() MatchArm {
	return MatchArm{
		Pattern: clonePattern(m.Pattern),
		Body:    cloneExpr(m.Body),
	}
}

// =============================================================================
// Pattern Clones
// =============================================================================

func (w *WildcardPattern) Clone() Node {
	return &WildcardPattern{
		Pos_: w.Pos_,
	}
}

func (l *LiteralPattern) Clone() Node {
	return &LiteralPattern{
		Pos_:  l.Pos_,
		Value: cloneExpr(l.Value),
	}
}

func (v *VariantPattern) Clone() Node {
	fields := make([]VariantPatternField, len(v.Fields))

	for i, f := range v.Fields {
		fields[i] = f.Clone()
	}

	var binding *Identifier
	if v.Binding != nil {
		binding = v.Binding.Clone().(*Identifier)
	}

	return &VariantPattern{
		Pos_:    v.Pos_,
		Name:    v.Name,
		Binding: binding,
		Fields:  fields,
	}
}

func (v *VariantPatternField) Clone() VariantPatternField {
	return VariantPatternField{
		Field:   v.Field,
		Binding: v.Binding.Clone().(*Identifier),
	}
}

// =============================================================================
// Type Expression Clones
// =============================================================================

func (i *NamedTypeExpr) Clone() Node {
	return &NamedTypeExpr{
		Pos_: i.Pos_,
		Name: i.Name.Clone().(*Identifier),
	}
}

func (a *ArrayTypeExpr) Clone() Node {
	return &ArrayTypeExpr{
		Pos_: a.Pos_,
		Size: a.Size,
		Base: cloneTypeExpr(a.Base),
	}
}

func (s *SliceTypeExpr) Clone() Node {
	return &SliceTypeExpr{
		Pos_: s.Pos_,
		Base: cloneTypeExpr(s.Base),
	}
}

func (m *MapTypeExpr) Clone() Node {
	return &MapTypeExpr{
		Pos_:  m.Pos_,
		Key:   cloneTypeExpr(m.Key),
		Value: cloneTypeExpr(m.Value),
	}
}

func (v *VectorTypeExpr) Clone() Node {
	return &VectorTypeExpr{
		Pos_: v.Pos_,
		Base: cloneTypeExpr(v.Base),
	}
}

func (t *TaskTypeExpr) Clone() Node {
	return &TaskTypeExpr{
		Pos_: t.Pos_,
		Base: cloneTypeExpr(t.Base),
	}
}

func (s *StructTypeExpr) Clone() Node {
	fields := make([]StructTypeField, len(s.Fields))

	for i, f := range s.Fields {
		fields[i] = f.Clone()
	}

	return &StructTypeExpr{
		Pos_:   s.Pos_,
		Name:   s.Name,
		Fields: fields,
	}
}

func (s *StructTypeField) Clone() StructTypeField {
	return StructTypeField{
		Name: s.Name,
		Type: cloneTypeExpr(s.Type),
	}
}

func (s *SumTypeExpr) Clone() Node {
	variants := make([]VariantTypeExpr, len(s.Variants))

	for i, v := range s.Variants {
		variants[i] = v.Clone()
	}

	typeArgs := make([]TypeExpr, len(s.TypeArgs))

	for i, t := range s.TypeArgs {
		typeArgs[i] = cloneTypeExpr(t)
	}

	return &SumTypeExpr{
		Pos_:     s.Pos_,
		Name:     s.Name,
		TypeArgs: typeArgs,
		Variants: variants,
	}
}

func (v *VariantTypeExpr) Clone() VariantTypeExpr {
	fields := make([]StructTypeField, len(v.Fields))

	for i, f := range v.Fields {
		fields[i] = f.Clone()
	}

	return VariantTypeExpr{
		Name:   v.Name,
		Fields: fields,
	}
}

func (f *FunctionTypeExpr) Clone() Node {
	params := make([]TypeExpr, len(f.Params))

	for i, p := range f.Params {
		params[i] = cloneTypeExpr(p)
	}

	return &FunctionTypeExpr{
		Pos_:   f.Pos_,
		Params: params,
		Return: cloneTypeExpr(f.Return),
	}
}
