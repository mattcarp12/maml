package tast

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Literals & Identifiers
// =============================================================================

type Identifier struct {
	Pos_   Position `json:"-"`
	Value  string
	Type   types.Type    // Permanently bound type
	Symbol *types.Symbol // Permanently bound semantic identity/scope
}

type IntLiteral struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Value int64
	Type  types.Type // Will always be types.IntType
}

type BoolLiteral struct {
	Pos_  Position `json:"-"`
	Value bool
	Type  types.Type // Will always be types.BoolType
}

type StringLiteral struct {
	Pos_  Position `json:"-"`
	Value string
	Type  types.Type // Will always be types.StringType
}

// =============================================================================
// Basic Operations
// =============================================================================

type PrefixExpr struct {
	Pos_     Position `json:"-"`
	Operator string
	Right    Expr
	Type     types.Type // The resulting type of the operation
}

type InfixExpr struct {
	Pos_     Position `json:"-"`
	Left     Expr
	Operator string
	Right    Expr
	Type     types.Type // The resulting type of the operation
}

// =============================================================================
// Interface Implementations
// =============================================================================

func (i *Identifier) Pos() Position { return i.Pos_ }
func (i *Identifier) End() Position {
	return Position{
		Line: i.Pos_.Line,
		Col:  i.Pos_.Col + len(i.Value),
	}
}
func (i *Identifier) String() string { return i.Value }
func (i *Identifier) exprNode()      {}

func (il *IntLiteral) Pos() Position  { return il.Pos_ }
func (il *IntLiteral) End() Position  { return il.End_ }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (il *IntLiteral) exprNode()      {}

func (b *BoolLiteral) Pos() Position { return b.Pos_ }
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
func (b *BoolLiteral) String() string { return fmt.Sprintf("%t", b.Value) }
func (b *BoolLiteral) exprNode()      {}

func (sl *StringLiteral) Pos() Position { return sl.Pos_ }
func (sl *StringLiteral) End() Position {
	return Position{
		Line: sl.Pos_.Line,
		Col:  sl.Pos_.Col + len(sl.Value) + 2, // +2 for quotes
	}
}
func (sl *StringLiteral) String() string { return fmt.Sprintf("\"%s\"", sl.Value) }
func (sl *StringLiteral) exprNode()      {}

func (pe *PrefixExpr) Pos() Position { return pe.Pos_ }
func (pe *PrefixExpr) End() Position { return pe.Right.End() }
func (pe *PrefixExpr) String() string {
	return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String())
}
func (pe *PrefixExpr) exprNode() {}

func (ie *InfixExpr) Pos() Position { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode() {}

// =============================================================================
// Aggregate Literals
// =============================================================================

type StructField struct {
	Pos_  Position `json:"-"`
	End_  Position `json:"-"`
	Key   Expr
	Value Expr
}

type StructLiteral struct {
	Pos_   Position `json:"-"`
	End_   Position `json:"-"`
	Fields []StructField
	Type   types.Type // Permanently bound semantic type
}

type ArrayLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []Expr
	Type     types.Type // Permanently bound semantic type
}

type MapLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []MapElement
	Type     types.MapType
}

type MapElement struct {
	Key   Expr
	Value Expr
}

type VecLiteral struct {
	Pos_     Position `json:"-"`
	End_     Position `json:"-"`
	Elements []Expr
	Type     types.VectorType
}

type VariantLiteral struct {
	Pos_      Position          `json:"-"`
	End_      Position          `json:"-"`
	Variant   *types.SumVariant // The specific variant (e.g., Some)
	Arguments []Expr            // The inner values (e.g., [5])
	Type      types.Type        // The resolved SumType (e.g., Option<int>)
}

// =============================================================================
// Complex Expressions
// =============================================================================

type CallArg struct {
	Pos_     Position `json:"-"`
	Argument Expr
	Mut      bool
	Own      bool
}

type CallExpr struct {
	Pos_      Position `json:"-"`
	Function  Expr
	Arguments []CallArg
	Type      types.Type // Resulting type of the function call
}

type MethodCallExpr struct {
	Pos_      Position `json:"-"`
	Object    Expr
	Method    *Identifier
	Arguments []CallArg
	Type      types.Type // Resulting type of the method call
}

type FieldAccess struct {
	Pos_   Position `json:"-"`
	Object Expr
	Field  *Identifier
	Type   types.Type // The type of the accessed field
}

type IndexExpr struct {
	Pos_  Position `json:"-"`
	Left  Expr
	Index Expr
	Type  types.Type // The type of the array/slice element
}

type SliceExpr struct {
	Pos_ Position `json:"-"`
	Left Expr
	Low  Expr
	High Expr
	Type types.Type // The resulting slice type
}

// =============================================================================
// Control Flow & Async Expressions
// =============================================================================

type IfExpr struct {
	Pos_        Position `json:"-"`
	Condition   Expr
	Consequence *BlockStmt
	Alternative *BlockStmt
	Type        types.Type // The unified return type of the branches
}

type AwaitExpr struct {
	Pos_  Position `json:"-"`
	Value Expr
	Type  types.Type // The resolved inner type of the Task
}

// =============================================================================
// Interface Implementations (Complex Expressions)
// =============================================================================

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
func (sl *StructLiteral) exprNode() {}

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
func (al *ArrayLiteral) exprNode() {}

func (n *MapLiteral) Pos() Position { return n.Pos_ }
func (n *MapLiteral) End() Position { return n.End_ }
func (n *MapLiteral) String() string {
	var elements []string
	for _, e := range n.Elements {
		elements = append(elements, fmt.Sprintf("%s: %s", e.Key.String(), e.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", n.Type.String(), strings.Join(elements, ", "))
}
func (n *MapLiteral) exprNode() {}

func (n *VecLiteral) Pos() Position { return n.Pos_ }
func (n *VecLiteral) End() Position { return n.End_ }
func (n *VecLiteral) String() string {
	var elems []string
	for _, el := range n.Elements {
		elems = append(elems, el.String())
	}
	return fmt.Sprintf("%s[%s]", n.Type.String(), strings.Join(elems, ", "))
}
func (n *VecLiteral) exprNode() {}

func (n *VariantLiteral) Pos() Position  { return n.Pos_ }
func (n *VariantLiteral) End() Position  { return n.End_ }
func (n *VariantLiteral) String() string { return n.Variant.Name + "(...)" }
func (n *VariantLiteral) exprNode()      {}

func (ce *CallExpr) Pos() Position { return ce.Pos_ }
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
		} else if a.Own {
			prefix = "own "
		}
		args = append(args, prefix+a.Argument.String())
	}
	return fmt.Sprintf("%s(%s)", ce.Function.String(), strings.Join(args, ", "))
}
func (ce *CallExpr) exprNode() {}

func (m *MethodCallExpr) Pos() Position { return m.Pos_ }
func (m *MethodCallExpr) End() Position {
	if len(m.Arguments) == 0 {
		return m.Method.End()
	}
	return m.Arguments[len(m.Arguments)-1].Argument.End()
}
func (m *MethodCallExpr) String() string {
	var args []string
	for _, a := range m.Arguments {
		args = append(args, a.Argument.String())
	}
	return fmt.Sprintf("%s.%s(%s)", m.Object.String(), m.Method.String(), strings.Join(args, ", "))
}
func (m *MethodCallExpr) exprNode() {}

func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) End() Position { return fa.Field.End() }
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s).%s", fa.Object.String(), fa.Field.String())
}
func (fa *FieldAccess) exprNode() {}

func (ie *IndexExpr) Pos() Position { return ie.Pos_ }
func (ie *IndexExpr) End() Position { return ie.Index.End() }
func (ie *IndexExpr) String() string {
	return fmt.Sprintf("(%s[%s])", ie.Left.String(), ie.Index.String())
}
func (ie *IndexExpr) exprNode() {}

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
func (se *SliceExpr) exprNode() {}

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

func (a *AwaitExpr) Pos() Position  { return a.Pos_ }
func (a *AwaitExpr) End() Position  { return a.Value.End() }
func (a *AwaitExpr) String() string { return fmt.Sprintf("(await %s)", a.Value.String()) }
func (a *AwaitExpr) exprNode()      {}
