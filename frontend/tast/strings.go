package tast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
)

func (p *Program) String() string {
	var out bytes.Buffer
	for _, d := range p.Decls {
		out.WriteString(d.String())
		out.WriteString("\n")
	}
	return out.String()
}

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
func (t *TypeDecl) String() string {
	return fmt.Sprintf("type %s = %s", t.Name.String(), t.Type.String())
}
func (i *Identifier) String() string     { return i.Value }
func (il *IntLiteral) String() string    { return fmt.Sprintf("%d", il.Value) }
func (b *BoolLiteral) String() string    { return fmt.Sprintf("%t", b.Value) }
func (sl *StringLiteral) String() string { return fmt.Sprintf("\"%s\"", sl.Value) }
func (pe *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String())
}
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
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
func (al *ArrayLiteral) String() string {
	var elems []string
	for _, el := range al.Elements {
		elems = append(elems, el.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}
func (n *MapLiteral) String() string {
	var elements []string
	for _, e := range n.Elements {
		elements = append(elements, fmt.Sprintf("%s: %s", e.Key.String(), e.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", n.Type.String(), strings.Join(elements, ", "))
}
func (n *VecLiteral) String() string {
	var elems []string
	for _, el := range n.Elements {
		elems = append(elems, el.String())
	}
	return fmt.Sprintf("%s[%s]", n.Type.String(), strings.Join(elems, ", "))
}
func (n *VariantLiteral) String() string { return n.Variant.Name + "(...)" }
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
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s).%s", fa.Object.String(), fa.Field.String())
}
func (ie *IndexExpr) String() string {
	return fmt.Sprintf("(%s[%s])", ie.Left.String(), ie.Index.String())
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
func (a *AwaitExpr) String() string { return fmt.Sprintf("(await %s)", a.Value.String()) }
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
func (m *MatchArm) String() string {
	return fmt.Sprintf("%s => %s,", m.Pattern.String(), m.Body.String())
}
func (w *WildcardPattern) String() string   { return "_" }
func (l *LiteralPattern) String() string    { return l.Value.String() }
func (i *IdentifierPattern) String() string { return i.Name }
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
func (v *VariantPatternField) String() string {
	if v.Binding == nil {
		return v.Field
	}
	return fmt.Sprintf("%s: %s", v.Field, v.Binding.String())
}
func (c *CompositePattern) String() string { return "CompositePattern" }
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
func (d *DeclareStmt) String() string {
	modifier := ""
	if d.Symbol.Mutable {
		modifier = "mut "
	}
	return fmt.Sprintf("%s%s := %s", modifier, d.Symbol.Name, d.Value.String())
}
func (a *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", a.LValue.String(), a.RValue.String())
}
func (vps *VecPushStmt) String() string {
	return fmt.Sprintf(
		"%s << %s",
		vps.LValue.String(),
		vps.RValue.String(),
	)
}
func (r *ReturnStmt) String() string {
	if r.Value != nil {
		return fmt.Sprintf("return %s", r.Value.String())
	}
	return "return"
}
func (ys *YieldStmt) String() string { return fmt.Sprintf("=> %s", ys.Value.String()) }
func (es *ExprStmt) String() string  { return es.Value.String() }
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
func (s *BreakStmt) String() string    { return "break" }
func (s *ContinueStmt) String() string { return "continue" }
func (f *OwnExpr) String() string      { return "own " + f.Value.String() }
func (f *FreezeExpr) String() string   { return "freeze " + f.Value.String() }
func (f *SpawnExpr) String() string    { return "spawn " + f.Value.String() }
