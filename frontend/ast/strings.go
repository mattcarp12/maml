package ast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/token"
)

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
func (t *TypeDecl) String() string {
	return fmt.Sprintf(
		"type %s = %s",
		t.Name.String(),
		t.Rhs.String(),
	)
}
func (a *AwaitExpr) String() string {
	return fmt.Sprintf("(await %s)", a.Value.String())
}
func (i *Identifier) String() string  { return i.Value }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (b *BoolLiteral) String() string { return fmt.Sprintf("%t", b.Value) }
func (sl *StringLiteral) String() string {
	return fmt.Sprintf(`"%s"`, sl.Value)
}

func (ce *CompositeElement) String() string {
	if ce.Key != nil {
		return fmt.Sprintf("%s: %s", ce.Key.String(), ce.Value.String())
	}
	return ce.Value.String()
}
func (cl *CompositeLiteral) String() string {
	var elems []string
	for _, e := range cl.Elements {
		elems = append(elems, e.String())
	}
	return fmt.Sprintf("%s{%s}", cl.TypeExpr.String(), strings.Join(elems, ", "))
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
func (ca *CallArg) String() string {
	prefix := ""
	if ca.Mut {
		prefix = "mut "
	} else if ca.Own {
		prefix = "own "
	}
	return prefix + ca.Argument.String()
}

func (f *OwnExpr) String() string    { return "own " + f.Value.String() }
func (f *FreezeExpr) String() string { return "freeze " + f.Value.String() }

func (ie *InfixExpr) String() string {
	return fmt.Sprintf(
		"(%s %s %s)",
		ie.Left.String(),
		ie.Operator,
		ie.Right.String(),
	)
}
func (pe *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String())
}
func (fa *FieldAccess) String() string {
	objStr := fa.Object.String()
	if _, ok := fa.Object.(*FieldAccess); ok {
		return fmt.Sprintf("%s.%s", objStr, fa.Field.String())
	}
	return fmt.Sprintf("(%s.%s)", objStr, fa.Field.String())
}
func (ie *IndexExpr) String() string {
	return fmt.Sprintf(
		"(%s[%s])",
		ie.Left.String(),
		ie.Index.String(),
	)
}
func (se *SliceExpr) String() string {
	return fmt.Sprintf(
		"(%s[%s:%s])",
		se.Left.String(),
		se.Low.String(),
		se.High.String(),
	)
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
	return fmt.Sprintf(
		"%s => %s,",
		m.Pattern.String(),
		m.Body.String(),
	)
}
func (w *WildcardPattern) String() string {
	return "_"
}
func (ip *IdentifierPattern) String() string {
	return ip.Name
}
func (l *LiteralPattern) String() string {
	return l.Value.String()
}
func (cp *CompositePattern) String() string {
	var elems []string
	for _, e := range cp.Elements {
		if e.Key != nil {
			elems = append(elems, fmt.Sprintf("%s: %s", e.Key.String(), e.Pattern.String()))
		} else {
			elems = append(elems, e.Pattern.String())
		}
	}
	return fmt.Sprintf("%s{%s}", cp.TypeExpr.String(), strings.Join(elems, ", "))
}
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
	op := token.DECLARE.String()
	format := "%s %s %s"
	if d.Mutable {
		format = "mut " + format
	}
	return fmt.Sprintf(format, d.Name, op, d.Value.String())
}
func (a *AssignStmt) String() string {
	return fmt.Sprintf(
		"%s = %s",
		a.LValue.String(),
		a.RValue.String(),
	)
}
func (vps *VecPushStmt) String() string {
	return fmt.Sprintf(
		"%s << %s",
		vps.LValue.String(),
		vps.RValue.String(),
	)
}
func (r *ReturnStmt) String() string {
	return fmt.Sprintf(
		token.RETURN.String()+" %s",
		r.Value.String(),
	)
}
func (ys *YieldStmt) String() string {
	return fmt.Sprintf("=> %s", ys.Value.String())
}
func (es *ExprStmt) String() string {
	return es.Value.String()
}
func (f *ForStmt) String() string {
	return fmt.Sprintf(
		"for (%s; %s; %s) %s",
		f.Init.String(),
		f.Condition.String(),
		f.Post.String(),
		f.Body.String(),
	)
}
func (s *BreakStmt) String() string {
	return s.Token.Literal
}
func (s *ContinueStmt) String() string {
	return s.Token.Literal
}
func (n *NamedTypeExpr) String() string {
	return n.Name.String()
}
func (a *ArrayTypeExpr) String() string {
	return fmt.Sprintf(
		"[%d]%s",
		a.Size,
		a.Base.String(),
	)
}
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
func (s *StructTypeField) String() string {
	return fmt.Sprintf("%s: %s", s.Name, s.Type.String())
}
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
func (tew *TypeExprWrapper) String() string {
	return tew.TypeExpr.String()
}
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
