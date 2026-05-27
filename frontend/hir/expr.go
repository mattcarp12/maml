package hir

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
	Value int64
	Type  types.Type
}

type BoolLiteral struct {
	Pos_  Position `json:"-"`
	Value bool
	Type  types.Type
}

type StringLiteral struct {
	Pos_  Position `json:"-"`
	Value string
	Type  types.Type
}

// =============================================================================
// Primitive Memory Allocation (Desugared Aggregates)
// =============================================================================

// ZeroAllocExpr replaces StructLiteral and ArrayLiteral.
// It instructs the backend to allocate zero-initialized memory for the given type.
// The decision between Stack vs Heap is deferred to the MIR/Backend AllocPass.
type ZeroAllocExpr struct {
	Pos_ Position   `json:"-"`
	Type types.Type // The type defining the size of the allocation
}

// =============================================================================
// Basic Operations
// =============================================================================

type PrefixExpr struct {
	Pos_     Position `json:"-"`
	Operator string
	Right    Expr
	Type     types.Type
}

type InfixExpr struct {
	Pos_     Position `json:"-"`
	Left     Expr
	Operator string
	Right    Expr
	Type     types.Type
}

// =============================================================================
// Complex Operations & Access
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
	Type      types.Type
}

type FieldAccess struct {
	Pos_   Position `json:"-"`
	Object Expr
	Field  *Identifier
	Type   types.Type
}

type IndexExpr struct {
	Pos_  Position `json:"-"`
	Left  Expr
	Index Expr
	Type  types.Type
}

type SliceExpr struct {
	Pos_ Position `json:"-"`
	Left Expr
	Low  Expr
	High Expr
	Type types.Type
}

// =============================================================================
// Control Flow & Async
// =============================================================================

type IfExpr struct {
	Pos_        Position `json:"-"`
	Condition   Expr
	Consequence *BlockStmt
	Alternative *BlockStmt
	Type        types.Type
}

type AwaitExpr struct {
	Pos_  Position `json:"-"`
	Value Expr
	Type  types.Type
}

// =============================================================================
// Interface Implementations
// =============================================================================

func (i *Identifier) Pos() Position  { return i.Pos_ }
func (i *Identifier) String() string { return i.Value }
func (i *Identifier) exprNode()      {}

func (il *IntLiteral) Pos() Position  { return il.Pos_ }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (il *IntLiteral) exprNode()      {}

func (b *BoolLiteral) Pos() Position  { return b.Pos_ }
func (b *BoolLiteral) String() string { return fmt.Sprintf("%t", b.Value) }
func (b *BoolLiteral) exprNode()      {}

func (sl *StringLiteral) Pos() Position  { return sl.Pos_ }
func (sl *StringLiteral) String() string { return fmt.Sprintf("\"%s\"", sl.Value) }
func (sl *StringLiteral) exprNode()      {}

func (z *ZeroAllocExpr) Pos() Position  { return z.Pos_ }
func (z *ZeroAllocExpr) String() string { return fmt.Sprintf("zero_alloc(%s)", z.Type.String()) }
func (z *ZeroAllocExpr) exprNode()      {}

func (pe *PrefixExpr) Pos() Position  { return pe.Pos_ }
func (pe *PrefixExpr) String() string { return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String()) }
func (pe *PrefixExpr) exprNode()      {}

func (ie *InfixExpr) Pos() Position { return ie.Pos_ }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode() {}

func (ce *CallExpr) Pos() Position { return ce.Pos_ }
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



func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s).%s", fa.Object.String(), fa.Field.String())
}
func (fa *FieldAccess) exprNode() {}

func (ie *IndexExpr) Pos() Position { return ie.Pos_ }
func (ie *IndexExpr) String() string {
	return fmt.Sprintf("(%s[%s])", ie.Left.String(), ie.Index.String())
}
func (ie *IndexExpr) exprNode() {}

func (se *SliceExpr) Pos() Position { return se.Pos_ }
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
func (a *AwaitExpr) String() string { return fmt.Sprintf("(await %s)", a.Value.String()) }
func (a *AwaitExpr) exprNode()      {}
