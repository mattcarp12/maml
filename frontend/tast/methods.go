package tast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
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
	for _, d := range p.Decls {
		out.WriteString(d.String())
		out.WriteString("\n")
	}
	return out.String()
}

func (p *Param) Pos() Position { return p.Pos_ }
func (p *Param) End() Position { return p.End_ }
func (p *Param) String() string {
	// Relying strictly on the Symbol's ParamMode (0=Borrow, 1=MutBorrow, 2=Owned)
	prefix := ""
	if p.Symbol != nil {
		switch p.Symbol.ParamMode {
		case types.ParamMutBorrow:
			prefix = "mut "
		}
	}
	return fmt.Sprintf("%s%s %s", prefix, p.Name, p.Type.String())
}

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

	var params []string
	for _, p := range f.Params {
		params = append(params, p.String())
	}
	out.WriteString(strings.Join(params, ", "))
	out.WriteString(") ")

	if f.ReturnType != nil {
		out.WriteString(f.ReturnType.String())
		out.WriteString(" ")
	}
	if f.Body != nil {
		out.WriteString(f.Body.String())
	}
	return out.String()
}
func (f *FnDecl) declNode() {}

func (t *TypeDecl) Pos() Position { return t.Pos_ }
func (t *TypeDecl) End() Position { return t.End_ }
func (t *TypeDecl) String() string {
	return fmt.Sprintf("type %s = %s", t.Name.String(), t.Type.String())
}
func (t *TypeDecl) declNode() {}

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
func (sl *StringLiteral) String() string { return fmt.Sprintf("\"%s\"", sl.Value) }
func (sl *StringLiteral) exprNode()      {}
func (pe *PrefixExpr) Pos() Position     { return pe.Pos_ }
func (pe *PrefixExpr) End() Position     { return pe.Right.End() }
func (pe *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String())
}
func (pe *PrefixExpr) exprNode()    {}
func (ie *InfixExpr) Pos() Position { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode()         {}
func (sl *StructLiteral) Pos() Position { return sl.Pos_ }
func (sl *StructLiteral) End() Position { return sl.End_ }
func (sl *StructLiteral) String() string {
	var fields []string
	for _, f := range sl.Fields {
		if f.Key != nil {
			fields = append(fields, fmt.Sprintf("%s: %s", f.Key.String(), f.Value.String()))
		} else {
			fields = append(fields, f.Value.String())
		}
	}
	return fmt.Sprintf("%s{%s}", sl.Type.String(), strings.Join(fields, ", "))
}
func (sl *StructLiteral) exprNode()    {}
func (al *ArrayLiteral) Pos() Position { return al.Pos_ }
func (al *ArrayLiteral) End() Position {
	if len(al.Elements) == 0 {
		return Position{Line: al.Pos_.Line, Col: al.Pos_.Col + 2}
	}
	return al.End_
}
func (al *ArrayLiteral) String() string {
	var elems []string
	for _, el := range al.Elements {
		elems = append(elems, el.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}
func (al *ArrayLiteral) exprNode()  {}
func (n *MapLiteral) Pos() Position { return n.Pos_ }
func (n *MapLiteral) End() Position { return n.End_ }
func (n *MapLiteral) String() string {
	var elements []string
	for _, e := range n.Elements {
		elements = append(elements, fmt.Sprintf("%s: %s", e.Key.String(), e.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", n.Type.String(), strings.Join(elements, ", "))
}
func (n *MapLiteral) exprNode()     {}
func (n *VecLiteral) Pos() Position { return n.Pos_ }
func (n *VecLiteral) End() Position { return n.End_ }
func (n *VecLiteral) String() string {
	var elems []string
	for _, el := range n.Elements {
		elems = append(elems, el.String())
	}
	return fmt.Sprintf("%s[%s]", n.Type.String(), strings.Join(elems, ", "))
}
func (n *VecLiteral) exprNode()          {}
func (n *VariantLiteral) Pos() Position  { return n.Pos_ }
func (n *VariantLiteral) End() Position  { return n.End_ }
func (n *VariantLiteral) String() string { return n.Variant.Name + "(...)" }
func (n *VariantLiteral) exprNode()      {}
func (ce *CallExpr) Pos() Position       { return ce.Pos_ }
func (ce *CallExpr) End() Position {
	if len(ce.Arguments) == 0 {
		return ce.Function.End()
	}
	return ce.Arguments[len(ce.Arguments)-1].Argument.End()
}
func (ce *CallExpr) String() string {
	var args []string
	for _, a := range ce.Arguments {
		prefix := ""
		if a.Mut {
			prefix = "mut "
		}
		args = append(args, prefix+a.Argument.String())
	}
	return fmt.Sprintf("%s(%s)", ce.Function.String(), strings.Join(args, ", "))
}
func (ce *CallExpr) exprNode()        {}
func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) End() Position { return fa.Field.End() }
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s).%s", fa.Object.String(), fa.Field.String())
}
func (fa *FieldAccess) exprNode()   {}
func (ie *IndexExpr) Pos() Position { return ie.Pos_ }
func (ie *IndexExpr) End() Position { return ie.Index.End() }
func (ie *IndexExpr) String() string {
	return fmt.Sprintf("(%s[%s])", ie.Left.String(), ie.Index.String())
}
func (ie *IndexExpr) exprNode()     {}
func (se *SliceExpr) Pos() Position { return se.Pos_ }
func (se *SliceExpr) End() Position {
	if se.High != nil {
		return se.High.End()
	}
	return se.Left.End()
}
func (se *SliceExpr) String() string {
	lowStr, highStr := "", ""
	if se.Low != nil {
		lowStr = se.Low.String()
	}
	if se.High != nil {
		highStr = se.High.String()
	}
	return fmt.Sprintf("(%s[%s:%s])", se.Left.String(), lowStr, highStr)
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
func (ie *IfExpr) exprNode()        {}
func (a *AwaitExpr) Pos() Position  { return a.Pos_ }
func (a *AwaitExpr) End() Position  { return a.Value.End() }
func (a *AwaitExpr) String() string { return fmt.Sprintf("(await %s)", a.Value.String()) }
func (a *AwaitExpr) exprNode()      {}

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
	return fmt.Sprintf("%s => %s,", m.Pattern.String(), m.Body.String())
}

func (w *WildcardPattern) Pos() Position { return w.Pos_ }
func (w *WildcardPattern) End() Position {
	return Position{
		Line: w.Pos_.Line,
		Col:  w.Pos_.Col + 1,
	}
}
func (w *WildcardPattern) String() string { return "_" }
func (w *WildcardPattern) patternNode()   {}

func (l *LiteralPattern) Pos() Position  { return l.Pos_ }
func (l *LiteralPattern) End() Position  { return l.End_ }
func (l *LiteralPattern) String() string { return l.Value.String() }
func (l *LiteralPattern) patternNode()   {}

func (i *IdentifierPattern) Pos() Position  { return i.Pos_ }
func (i *IdentifierPattern) End() Position  { return i.End_ }
func (i *IdentifierPattern) String() string { return i.Name }
func (i *IdentifierPattern) patternNode()   {}

func (v *VariantPattern) Pos() Position { return v.Pos_ }
func (v *VariantPattern) End() Position { return v.End_ }
func (v *VariantPattern) String() string {
	var out bytes.Buffer
	out.WriteString(v.Variant.String())
	if len(v.Fields) > 0 {
		out.WriteString(" { ")
		var fields []string
		for _, f := range v.Fields {
			fields = append(fields, f.String())
		}
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
func (b *BlockStmt) stmtNode() {}
func (b *BlockStmt) exprNode() {}

func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) End() Position { return d.Value.End() }
func (d *DeclareStmt) String() string {
	modifier := ""
	if d.Symbol.Mutable {
		modifier = "mut "
	}
	return fmt.Sprintf("%s%s := %s", modifier, d.Symbol.Name, d.Value.String())
}
func (d *DeclareStmt) stmtNode() {}

func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) End() Position { return a.RValue.End() }
func (a *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", a.LValue.String(), a.RValue.String())
}
func (a *AssignStmt) stmtNode()        {}
func (vps *VecPushStmt) Pos() Position { return vps.Pos_ }
func (vps *VecPushStmt) End() Position { return vps.RValue.End() }
func (vps *VecPushStmt) String() string {
	return fmt.Sprintf(
		"%s << %s",
		vps.LValue.String(),
		vps.RValue.String(),
	)
}
func (vps *VecPushStmt) stmtNode()  {}
func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) End() Position {
	if r.Value != nil {
		return r.Value.End()
	}
	// Fallback for bare return keyword
	return Position{Line: r.Pos_.Line, Col: r.Pos_.Col + 6}
}
func (r *ReturnStmt) String() string {
	if r.Value != nil {
		return fmt.Sprintf("return %s", r.Value.String())
	}
	return "return"
}
func (r *ReturnStmt) stmtNode() {}

func (ys *YieldStmt) Pos() Position  { return ys.Pos_ }
func (ys *YieldStmt) End() Position  { return ys.Value.End() }
func (ys *YieldStmt) String() string { return fmt.Sprintf("=> %s", ys.Value.String()) }
func (ys *YieldStmt) stmtNode()      {}

func (es *ExprStmt) Pos() Position  { return es.Pos_ }
func (es *ExprStmt) End() Position  { return es.Value.End() }
func (es *ExprStmt) String() string { return es.Value.String() }
func (es *ExprStmt) stmtNode()      {}

func (f *ForStmt) Pos() Position { return f.Pos_ }
func (f *ForStmt) End() Position { return f.Body.End() }
func (f *ForStmt) String() string {
	initStr := ""
	if f.Init != nil {
		initStr = f.Init.String()
	}
	condStr := ""
	if f.Condition != nil {
		condStr = f.Condition.String()
	}
	postStr := ""
	if f.Post != nil {
		postStr = f.Post.String()
	}
	return fmt.Sprintf("for (%s; %s; %s) %s", initStr, condStr, postStr, f.Body.String())
}
func (f *ForStmt) stmtNode()           {}
func (s *BreakStmt) Pos() Position     { return s.Pos_ }
func (s *BreakStmt) End() Position     { return s.End_ }
func (s *BreakStmt) String() string    { return "break" }
func (s *BreakStmt) stmtNode()         {}
func (s *ContinueStmt) Pos() Position  { return s.Pos_ }
func (s *ContinueStmt) End() Position  { return s.End_ }
func (s *ContinueStmt) String() string { return "continue" }
func (s *ContinueStmt) stmtNode()      {}
