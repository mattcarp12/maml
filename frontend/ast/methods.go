package ast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/token"
)

func (p *Program) Pos() Position {
	if len(p.Decls) == 0 {
		return Position{}
	}
	return p.Decls[0].Pos()
}
func (p *Program) End() Position {
	if len(p.Decls) == 0 {
		return Position{}
	}
	return p.Decls[len(p.Decls)-1].End()
}
func (p *Program) String() string {
	var out bytes.Buffer
	for i, d := range p.Decls {
		out.WriteString(d.String())
		if i != len(p.Decls)-1 {
			out.WriteString("\n\n")
		}
	}
	return out.String()
}
func (p *Param) String() string {
	var prefixes []string
	if p.Mut {
		prefixes = append(prefixes, "mut")
	}
	if p.Own {
		prefixes = append(prefixes, "own")
	}
	prefix := ""
	if len(prefixes) > 0 {
		prefix = strings.Join(prefixes, " ") + " "
	}
	if p.Type == nil {
		return prefix + p.Name
	}
	return fmt.Sprintf(
		"%s%s: %s",
		prefix,
		p.Name,
		p.Type.String(),
	)
}
func (p *Param) Pos() Position  { return p.Pos_ }
func (p *Param) End() Position  { return p.End_ }
func (f *FnDecl) Pos() Position { return f.Pos_ }
func (f *FnDecl) End() Position { return f.End_ }
func (f *FnDecl) String() string {
	var out bytes.Buffer
	if f.IsAsync {
		out.WriteString("async ")
	}
	out.WriteString("fn ")
	out.WriteString(f.Name)
	out.WriteString("(")
	for i, p := range f.Params {
		out.WriteString(p.String())
		if i != len(f.Params)-1 {
			out.WriteString(", ")
		}
	}
	out.WriteString(")")
	if f.ReturnType != nil {
		out.WriteString(" -> ")
		out.WriteString(f.ReturnType.String())
	}
	out.WriteString(" ")
	out.WriteString(f.Body.String())
	return out.String()
}
func (f *FnDecl) declNode()       {}
func (t *TypeDecl) Pos() Position { return t.Pos_ }
func (t *TypeDecl) End() Position { return t.End_ }
func (t *TypeDecl) String() string {
	return fmt.Sprintf(
		"type %s = %s",
		t.Name.String(),
		t.Rhs.String(),
	)
}
func (t *TypeDecl) declNode()      {}
func (a *AwaitExpr) Pos() Position { return a.Pos_ }
func (a *AwaitExpr) End() Position { return a.Value.End() }
func (a *AwaitExpr) String() string {
	return fmt.Sprintf("(await %s)", a.Value.String())
}
func (a *AwaitExpr) exprNode()      {}
func (i *Identifier) Pos() Position { return i.Pos_ }
func (i *Identifier) End() Position {
	return Position{
		Line: i.Pos_.Line,
		Col:  i.Pos_.Col + len(i.Value),
	}
}
func (i *Identifier) String() string  { return i.Value }
func (i *Identifier) exprNode()       {}
func (il *IntLiteral) Pos() Position  { return il.Pos_ }
func (il *IntLiteral) End() Position  { return il.End_ }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (il *IntLiteral) exprNode()      {}
func (b *BoolLiteral) Pos() Position  { return b.Pos_ }
func (b *BoolLiteral) End() Position {
	length := 4
	if !b.Value {
		length = 5
	}
	return Position{
		Line: b.Pos_.Line,
		Col:  b.Pos_.Col + length,
	}
}
func (b *BoolLiteral) String() string   { return fmt.Sprintf("%t", b.Value) }
func (b *BoolLiteral) exprNode()        {}
func (sl *StringLiteral) Pos() Position { return sl.Pos_ }
func (sl *StringLiteral) End() Position {
	return Position{
		Line: sl.Pos_.Line,
		Col:  sl.Pos_.Col + len(sl.Value) + 2,
	}
}
func (sl *StringLiteral) String() string {
	return fmt.Sprintf(`"%s"`, sl.Value)
}
func (sl *StringLiteral) exprNode()     {}
func (sl *StructLiteral) Pos() Position { return sl.Pos_ }
func (sl *StructLiteral) End() Position { return sl.End_ }
func (sl *StructLiteral) String() string {
	var fields []string
	for _, f := range sl.Fields {
		if f.Key != nil {
			// Keyed field (e.g., Structs or Maps: `x: 10` or `"hello": 20`)
			fields = append(fields, fmt.Sprintf("%s: %s", f.Key.String(), f.Value.String()))
		} else {
			// Unkeyed field (e.g., Vectors: `1`)
			fields = append(fields, f.Value.String())
		}
	}
	return fmt.Sprintf("%s{%s}", sl.Type.String(), strings.Join(fields, ", "))
}
func (sl *StructLiteral) exprNode()    {}
func (sf *StructField) Pos() Position  { return sf.Pos_ }
func (al *ArrayLiteral) Pos() Position { return al.Pos_ }
func (al *ArrayLiteral) End() Position {
	if len(al.Elements) == 0 {
		return Position{
			Line: al.Pos_.Line,
			Col:  al.Pos_.Col + 2,
		}
	}
	return al.Elements[len(al.Elements)-1].End()
}
func (al *ArrayLiteral) String() string {
	var elems []string
	for _, e := range al.Elements {
		elems = append(elems, e.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}
func (al *ArrayLiteral) exprNode() {}
func (ce *CallExpr) Pos() Position { return ce.Pos_ }
func (ce *CallExpr) End() Position {
	if len(ce.Arguments) == 0 {
		return ce.Function.End()
	}
	return ce.Arguments[len(ce.Arguments)-1].End()
}
func (ce *CallExpr) String() string {
	var args []string
	for _, a := range ce.Arguments {
		args = append(args, a.String())
	}
	return fmt.Sprintf(
		"%s(%s)",
		ce.Function.String(),
		strings.Join(args, ", "),
	)
}
func (ce *CallExpr) exprNode()            {}
func (mce *MethodCallExpr) Pos() Position { return mce.Pos_ }
func (mce *MethodCallExpr) End() Position {
	if len(mce.Arguments) == 0 {
		return mce.Method.End()
	}
	return mce.Arguments[len(mce.Arguments)-1].End()
}
func (mce *MethodCallExpr) String() string {
	var args []string
	for _, a := range mce.Arguments {
		prefix := ""
		if a.Mut {
			prefix = "mut "
		} else if a.Own {
			prefix = "own "
		}
		args = append(args, prefix+a.Argument.String())
	}
	return fmt.Sprintf("%s.%s(%s)", mce.Object.String(), mce.Method, strings.Join(args, ", "))
}
func (mce *MethodCallExpr) exprNode() {}
func (ca *CallArg) Pos() Position     { return ca.Pos_ }
func (ca *CallArg) End() Position {
	if ca.Argument == nil {
		return ca.Pos_
	}
	return ca.Argument.End()
}
func (ca *CallArg) String() string {
	prefix := ""
	if ca.Mut {
		prefix = "mut "
	} else if ca.Own {
		prefix = "own "
	}
	return prefix + ca.Argument.String()
}
func (ie *InfixExpr) Pos() Position { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf(
		"(%s %s %s)",
		ie.Left.String(),
		ie.Operator,
		ie.Right.String(),
	)
}
func (ie *InfixExpr) exprNode()      {}
func (pe *PrefixExpr) Pos() Position { return pe.Pos_ }
func (pe *PrefixExpr) End() Position { return pe.Right.End() }
func (pe *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String())
}
func (pe *PrefixExpr) exprNode()      {}
func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) End() Position { return fa.Field.End() }
func (fa *FieldAccess) String() string {
	objStr := fa.Object.String()
	if _, ok := fa.Object.(*FieldAccess); ok {
		return fmt.Sprintf("%s.%s", objStr, fa.Field.String())
	}
	return fmt.Sprintf("(%s.%s)", objStr, fa.Field.String())
}
func (fa *FieldAccess) exprNode()   {}
func (ie *IndexExpr) Pos() Position { return ie.Pos_ }
func (ie *IndexExpr) End() Position { return ie.Index.End() }
func (ie *IndexExpr) String() string {
	return fmt.Sprintf(
		"(%s[%s])",
		ie.Left.String(),
		ie.Index.String(),
	)
}
func (ie *IndexExpr) exprNode()     {}
func (se *SliceExpr) Pos() Position { return se.Pos_ }
func (se *SliceExpr) End() Position { return se.High.End() }
func (se *SliceExpr) String() string {
	return fmt.Sprintf(
		"(%s[%s:%s])",
		se.Left.String(),
		se.Low.String(),
		se.High.String(),
	)
}
func (se *SliceExpr) exprNode()  {}
func (ie *IfExpr) Pos() Position { return ie.Pos_ }
func (ie *IfExpr) End() Position {
	if ie.Alternative != nil {
		return ie.Alternative.End()
	}
	return ie.Consequence.End()
}
func (ie *IfExpr) String() string {
	var out bytes.Buffer
	out.WriteString("if ")
	out.WriteString(ie.Condition.String())
	out.WriteString(" ")
	out.WriteString(ie.Consequence.String())
	if ie.Alternative != nil {
		out.WriteString(" else ")
		out.WriteString(ie.Alternative.String())
	}
	return out.String()
}
func (ie *IfExpr) exprNode() {}

func (m *MatchExpr) Pos() Position { return m.Pos_ }
func (m *MatchExpr) End() Position { return m.End_ }
func (m *MatchExpr) String() string {
	var out bytes.Buffer
	out.WriteString("match ")
	out.WriteString(m.Subject.String())
	out.WriteString(" {\n")
	for _, arm := range m.Arms {
		out.WriteString("\t")
		out.WriteString(arm.String())
		out.WriteString("\n")
	}
	out.WriteString("}")
	return out.String()
}
func (m *MatchExpr) exprNode() {}
func (m *MatchArm) String() string {
	return fmt.Sprintf(
		"%s => %s,",
		m.Pattern.String(),
		m.Body.String(),
	)
}
func (w *WildcardPattern) Pos() Position { return w.Pos_ }
func (w *WildcardPattern) End() Position {
	return Position{
		Line: w.Pos_.Line,
		Col:  w.Pos_.Col + 1,
	}
}
func (w *WildcardPattern) String() string {
	return "_"
}
func (w *WildcardPattern) patternNode() {}
func (l *LiteralPattern) Pos() Position { return l.Pos_ }
func (l *LiteralPattern) End() Position { return l.End_ }
func (l *LiteralPattern) String() string {
	return l.Value.String()
}
func (l *LiteralPattern) patternNode()  {}
func (v *VariantPattern) Pos() Position { return v.Pos_ }
func (v *VariantPattern) End() Position { return v.End_ }
func (v *VariantPattern) String() string {
	var out bytes.Buffer
	out.WriteString(v.Name)
	if len(v.TupleBindings) > 0 {
		var bindings []string
		for _, f := range v.TupleBindings {
			bindings = append(bindings, f.String())
		}
		out.WriteString(" ( ")
		out.WriteString(strings.Join(bindings, ", "))
		out.WriteString(" )")
	}
	if len(v.Fields) > 0 {
		var fields []string
		for _, f := range v.Fields {
			fields = append(fields, f.String())
		}
		out.WriteString(" { ")
		out.WriteString(strings.Join(fields, ", "))
		out.WriteString(" }")
	}
	return out.String()
}
func (v *VariantPattern) patternNode() {}
func (v *VariantPatternField) String() string {
	if v.Binding == nil {
		return v.Field
	}
	if v.Field == v.Binding.Value {
		return v.Field
	}
	return fmt.Sprintf("%s: %s", v.Field, v.Binding.String())
}

func (b *BlockStmt) Pos() Position { return b.Pos_ }
func (b *BlockStmt) End() Position { return b.End_ }
func (b *BlockStmt) String() string {
	var out bytes.Buffer
	out.WriteString("{\n")
	for _, s := range b.Statements {
		out.WriteString("\t")
		out.WriteString(s.String())
		out.WriteString("\n")
	}
	out.WriteString("}")
	return out.String()
}
func (b *BlockStmt) stmtNode()       {}
func (b *BlockStmt) exprNode()       {}
func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) End() Position { return d.Value.End() }
func (d *DeclareStmt) String() string {
	op := token.DECLARE.String()
	format := "%s %s %s"
	if d.Mutable {
		format = "mut " + format
	}
	return fmt.Sprintf(format, d.Name, op, d.Value.String())
}
func (d *DeclareStmt) stmtNode()    {}
func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) End() Position { return a.RValue.End() }
func (a *AssignStmt) String() string {
	return fmt.Sprintf(
		"%s = %s",
		a.LValue.String(),
		a.RValue.String(),
	)
}
func (a *AssignStmt) stmtNode()     {}
func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) End() Position { return r.Value.End() }
func (r *ReturnStmt) String() string {
	return fmt.Sprintf(
		token.RETURN.String()+" %s",
		r.Value.String(),
	)
}
func (r *ReturnStmt) stmtNode()     {}
func (ys *YieldStmt) Pos() Position { return ys.Pos_ }
func (ys *YieldStmt) End() Position { return ys.Value.End() }
func (ys *YieldStmt) String() string {
	return fmt.Sprintf("=> %s", ys.Value.String())
}
func (ys *YieldStmt) stmtNode()    {}
func (es *ExprStmt) Pos() Position { return es.Pos_ }
func (es *ExprStmt) End() Position { return es.Value.End() }
func (es *ExprStmt) String() string {
	return es.Value.String()
}
func (es *ExprStmt) stmtNode()   {}
func (f *ForStmt) Pos() Position { return f.Pos_ }
func (f *ForStmt) End() Position { return f.Body.End() }
func (f *ForStmt) String() string {
	return fmt.Sprintf(
		"for (%s; %s; %s) %s",
		f.Init.String(),
		f.Condition.String(),
		f.Post.String(),
		f.Body.String(),
	)
}
func (f *ForStmt) stmtNode() {}
func (s *BreakStmt) Pos() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col,
	}
}
func (s *BreakStmt) End() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col + len(s.Token.Literal),
	}
}
func (s *BreakStmt) String() string {
	return s.Token.Literal
}
func (s *BreakStmt) stmtNode() {}
func (s *ContinueStmt) Pos() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col,
	}
}
func (s *ContinueStmt) End() Position {
	return Position{
		Line: s.Token.Line,
		Col:  s.Token.Col + len(s.Token.Literal),
	}
}
func (s *ContinueStmt) String() string {
	return s.Token.Literal
}
func (s *ContinueStmt) stmtNode() {}

func (n *NamedTypeExpr) Pos() Position { return n.Pos_ }
func (n *NamedTypeExpr) End() Position { return n.End_ }
func (n *NamedTypeExpr) String() string {
	return n.Name.String()
}
func (n *NamedTypeExpr) typeNode()     {}
func (a *ArrayTypeExpr) Pos() Position { return a.Pos_ }
func (a *ArrayTypeExpr) End() Position { return a.End_ }
func (a *ArrayTypeExpr) String() string {
	return fmt.Sprintf(
		"[%d]%s",
		a.Size,
		a.Base.String(),
	)
}
func (a *ArrayTypeExpr) typeNode()     {}
func (s *SliceTypeExpr) Pos() Position { return s.Pos_ }
func (s *SliceTypeExpr) End() Position { return s.End_ }
func (s *SliceTypeExpr) String() string {
	return fmt.Sprintf("[]%s", s.Base.String())
}
func (s *SliceTypeExpr) typeNode()      {}
func (s *StructTypeExpr) Pos() Position { return s.Pos_ }
func (s *StructTypeExpr) End() Position { return s.End_ }
func (s *StructTypeExpr) String() string {
	var out bytes.Buffer
	out.WriteString("struct")
	if s.Name != "" {
		out.WriteString(" ")
		out.WriteString(s.Name)
	}
	out.WriteString(" {\n")
	for _, f := range s.Fields {
		out.WriteString("\t")
		out.WriteString(f.String())
		out.WriteString(",\n")
	}
	out.WriteString("}")
	return out.String()
}
func (s *StructTypeExpr) typeNode() {}
func (s *StructTypeField) String() string {
	return fmt.Sprintf("%s: %s", s.Name, s.Type.String())
}
func (s *SumTypeExpr) Pos() Position { return s.Pos_ }
func (s *SumTypeExpr) End() Position { return s.End_ }
func (s *SumTypeExpr) String() string {
	var out bytes.Buffer
	out.WriteString("sum ")
	out.WriteString(s.Name)
	if len(s.TypeArgs) > 0 {
		var args []string
		for _, arg := range s.TypeArgs {
			args = append(args, arg.String())
		}
		out.WriteString("<")
		out.WriteString(strings.Join(args, ", "))
		out.WriteString(">")
	}
	out.WriteString(" {\n")
	for _, variant := range s.Variants {
		out.WriteString("\t")
		out.WriteString(variant.String())
		out.WriteString(",\n")
	}
	out.WriteString("}")
	return out.String()
}
func (s *SumTypeExpr) typeNode() {}
func (v *VariantTypeExpr) String() string {
	var out bytes.Buffer
	out.WriteString(v.Name)
	if len(v.Fields) > 0 {
		var fields []string
		for _, f := range v.Fields {
			fields = append(fields, f.String())
		}
		out.WriteString(" { ")
		out.WriteString(strings.Join(fields, ", "))
		out.WriteString(" }")
	}
	return out.String()
}
func (g *GenericTypeExpr) Pos() Position { return g.Pos_ }
func (g *GenericTypeExpr) End() Position { return g.End_ }
func (g *GenericTypeExpr) String() string {
	var parts []string
	for _, arg := range g.Args {
		parts = append(parts, arg.String())
	}
	return fmt.Sprintf("%s<%s>",
		g.Name.String(),
		strings.Join(parts, ", "),
	)
}
func (g *GenericTypeExpr) typeNode() {}
func (g *GenericTypeExpr) exprNode() {}
