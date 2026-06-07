// frontend/hir/hir.go
//
// High-Level Intermediate Representation (HIR)
//
// The HIR sits between the TAST and MIR in the compiler pipeline:
//
//   Source → Lexer → Parser → AST → Sema → TAST → HIR → MIR → Codegen
//
// The HIR is produced by the lowering pass (hir/lower.go), which consumes
// a fully type-checked TAST and emits a structurally simpler tree. Every
// node in the HIR carries a resolved types.Type — no type inference happens
// here. The HIR is read-only from the perspective of later passes.
//
// =============================================================================
// What the HIR removes relative to the TAST
// =============================================================================
//
//  TAST node               → HIR equivalent
//  ──────────────────────────────────────────────────────────────────────────
//  InfixExpr{op: "&&"}     → IfExpr (short-circuit lowered to branch)
//  InfixExpr{op: "||"}     → IfExpr (short-circuit lowered to branch)
//  MatchExpr               → nested IfExpr chain (see VariantDiscriminantExpr,
//                            VariantReadExpr, and payload DeclareStmts)
//  MethodCallExpr          → CallExpr (receiver prepended as argument 0,
//                            method name mangled to its runtime symbol)
//  IndexExpr on MapType    → MapReadExpr
//  IndexExpr on VectorType → VecReadExpr
//  AssignStmt{map[k]=v}    → MapInsertStmt
//  MethodCall map.get(k)   → MapReadExpr
//  MethodCall map.put(k,v) → MapInsertStmt (wrapped in a block yielding unit)
//  ForStmt{Init, Post}     → outer block{ init; ForStmt{cond, body+post} }
//                            (C-style for becomes a while-style loop)
//
// The HIR retains everything else from the TAST unchanged in structure,
// including IfExpr, BlockStmt, CallExpr, FieldAccess, SliceExpr, all
// literal nodes, and all declaration nodes.
//
// =============================================================================
// What the HIR adds relative to the TAST
// =============================================================================
//
//  VariantDiscriminantExpr  — reads the integer discriminant of a sum-type value
//  VariantReadExpr          — extracts a single payload field from a variant,
//                             identified by variant name and zero-based field index
//  MapReadExpr              — calls the runtime map-get function
//  VecReadExpr              — calls the runtime vec-get function
//  MapInsertStmt            — calls the runtime map-put function
//
// These nodes were previously smuggled into the TAST as "desugaring helpers".
// In the HIR they are proper first-class citizens with no TAST dependency.

package hir

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// Position (aliased from ast for source-location tracking)
// =============================================================================

type Position = ast.Position

// =============================================================================
// Node interfaces
// =============================================================================

type Node interface {
	Pos() Position
	End() Position
	String() string
}

type Decl interface {
	Node
	declNode()
}

type Stmt interface {
	Node
	stmtNode()
}

type Expr interface {
	Node
	exprNode()
}

// =============================================================================
// Declarations
// =============================================================================

type Program struct {
	Decls []Decl
}

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

// Param is a single function parameter, fully resolved to a concrete type.
type Param struct {
	Pos_   Position
	End_   Position
	Name   string
	Type   types.Type
	Symbol *types.Symbol
}

func (p *Param) Pos() Position  { return p.Pos_ }
func (p *Param) End() Position  { return p.End_ }
func (p *Param) String() string { return fmt.Sprintf("%s %s", p.Name, p.Type.String()) }

// FnDecl is a fully lowered function declaration. Its body is a BlockStmt
// expressed entirely in HIR terms — no MatchExpr, no MethodCallExpr, no &&/||.
type FnDecl struct {
	Pos_       Position
	End_       Position
	Name       string
	Params     []*Param
	ReturnType types.Type
	Body       *BlockStmt
	IsAsync    bool
	Symbol     *types.Symbol
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
	out.WriteString(")")
	if f.ReturnType != nil {
		out.WriteString(" ")
		out.WriteString(f.ReturnType.String())
	}
	if f.Body != nil {
		out.WriteString(" ")
		out.WriteString(f.Body.String())
	}
	return out.String()
}
func (f *FnDecl) declNode() {}

// TypeDecl carries the resolved type for a user-defined type declaration.
// The HIR does not re-analyse type bodies; it simply preserves the resolved
// types.Type from the TAST for use by later passes.
type TypeDecl struct {
	Pos_   Position
	End_   Position
	Name   *Identifier
	Type   types.Type
	Symbol *types.Symbol
}

func (t *TypeDecl) Pos() Position { return t.Pos_ }
func (t *TypeDecl) End() Position { return t.End_ }
func (t *TypeDecl) String() string {
	return fmt.Sprintf("type %s = %s", t.Name.String(), t.Type.String())
}
func (t *TypeDecl) declNode() {}

// =============================================================================
// Statements
// =============================================================================

// BlockStmt is a sequence of statements forming a lexical scope.
// In the HIR a BlockStmt can also appear as an Expr (it implements exprNode)
// so that consequence/alternative branches of IfExpr can hold blocks directly.
type BlockStmt struct {
	Pos_       Position
	End_       Position
	Statements []Stmt
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

// DeclareStmt introduces a new immutable or mutable local variable.
type DeclareStmt struct {
	Pos_   Position
	Symbol *types.Symbol // carries Name, Type, Mutable
	Value  Expr
}

func (d *DeclareStmt) Pos() Position { return d.Pos_ }
func (d *DeclareStmt) End() Position { return d.Value.End() }
func (d *DeclareStmt) String() string {
	mod := ""
	if d.Symbol != nil && d.Symbol.Mutable {
		mod = "mut "
	}
	name := ""
	if d.Symbol != nil {
		name = d.Symbol.Name
	}
	return fmt.Sprintf("%s%s := %s", mod, name, d.Value.String())
}
func (d *DeclareStmt) stmtNode() {}

// AssignStmt mutates an existing lvalue. In the HIR, AssignStmt is only ever
// used for plain variable or field/index assignments — map insertions have
// already been rewritten to MapInsertStmt by the lowering pass.
type AssignStmt struct {
	Pos_   Position
	LValue Expr
	RValue Expr
}

func (a *AssignStmt) Pos() Position { return a.Pos_ }
func (a *AssignStmt) End() Position { return a.RValue.End() }
func (a *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", a.LValue.String(), a.RValue.String())
}
func (a *AssignStmt) stmtNode() {}

// ReturnStmt exits the enclosing function with an optional value.
type ReturnStmt struct {
	Pos_  Position
	Value Expr // nil for bare `return`
}

func (r *ReturnStmt) Pos() Position { return r.Pos_ }
func (r *ReturnStmt) End() Position {
	if r.Value != nil {
		return r.Value.End()
	}
	return Position{Line: r.Pos_.Line, Col: r.Pos_.Col + 6}
}
func (r *ReturnStmt) String() string {
	if r.Value != nil {
		return fmt.Sprintf("return %s", r.Value.String())
	}
	return "return"
}
func (r *ReturnStmt) stmtNode() {}

// YieldStmt provides the value of the enclosing block expression (the `=>` arrow).
type YieldStmt struct {
	Pos_  Position
	Value Expr
}

func (y *YieldStmt) Pos() Position  { return y.Pos_ }
func (y *YieldStmt) End() Position  { return y.Value.End() }
func (y *YieldStmt) String() string { return fmt.Sprintf("=> %s", y.Value.String()) }
func (y *YieldStmt) stmtNode()      {}

// ExprStmt wraps an expression used purely for its side effects.
type ExprStmt struct {
	Pos_  Position
	Value Expr
}

func (e *ExprStmt) Pos() Position  { return e.Pos_ }
func (e *ExprStmt) End() Position  { return e.Value.End() }
func (e *ExprStmt) String() string { return e.Value.String() }
func (e *ExprStmt) stmtNode()      {}

// LoopStmt in the HIR is always while-style: Init and Post are always nil.
// The lowering pass rewrites C-style for(init; cond; post) into:
//
//	BlockStmt{ init; LoopStmt{ cond, body+post } }
//
// A missing Condition means an infinite loop (for {}).
type LoopStmt struct {
	Pos_      Position
	Condition Expr // nil → infinite loop
	Post      Stmt // for 'continue' jumps
	Body      *BlockStmt
}

func (f *LoopStmt) Pos() Position { return f.Pos_ }
func (f *LoopStmt) End() Position { return f.Body.End() }
func (f *LoopStmt) String() string {
	cond := ""
	if f.Condition != nil {
		cond = f.Condition.String()
	}
	return fmt.Sprintf("for %s %s", cond, f.Body.String())
}
func (f *LoopStmt) stmtNode() {}

// BreakStmt exits the nearest enclosing loop.
type BreakStmt struct {
	Pos_ Position
	End_ Position
}

func (s *BreakStmt) Pos() Position  { return s.Pos_ }
func (s *BreakStmt) End() Position  { return s.End_ }
func (s *BreakStmt) String() string { return "break" }
func (s *BreakStmt) stmtNode()      {}

// ContinueStmt jumps to the next iteration of the nearest enclosing loop.
type ContinueStmt struct {
	Pos_ Position
	End_ Position
}

func (s *ContinueStmt) Pos() Position  { return s.Pos_ }
func (s *ContinueStmt) End() Position  { return s.End_ }
func (s *ContinueStmt) String() string { return "continue" }
func (s *ContinueStmt) stmtNode()      {}

// MapInsertStmt is produced by the lowering pass for both `map[k] = v` and
// `map.put(k, v)`. It calls the runtime `maml_map_put` function.
// It is a statement, not an expression, because insertion returns unit.
type MapInsertStmt struct {
	Pos_  Position
	Map   Expr
	Key   Expr
	Value Expr
}

func (m *MapInsertStmt) Pos() Position { return m.Pos_ }
func (m *MapInsertStmt) End() Position { return m.Value.End() }
func (m *MapInsertStmt) String() string {
	return fmt.Sprintf("%s[%s] = %s", m.Map.String(), m.Key.String(), m.Value.String())
}
func (m *MapInsertStmt) stmtNode() {}

// =============================================================================
// Expressions — primitives
// =============================================================================

// Identifier is a resolved reference to a named symbol (variable, function, or
// variant constructor). Its Type and Symbol are always populated after lowering.
type Identifier struct {
	Pos_   Position
	End_   Position
	Value  string
	Type   types.Type
	Symbol *types.Symbol
}

func (i *Identifier) Pos() Position { return i.Pos_ }
func (i *Identifier) End() Position {
	return Position{Line: i.Pos_.Line, Col: i.Pos_.Col + len(i.Value)}
}
func (i *Identifier) String() string { return i.Value }
func (i *Identifier) exprNode()      {}

type IntLiteral struct {
	Pos_  Position
	End_  Position
	Value int64
	Type  types.Type
}

func (il *IntLiteral) Pos() Position  { return il.Pos_ }
func (il *IntLiteral) End() Position  { return il.End_ }
func (il *IntLiteral) String() string { return fmt.Sprintf("%d", il.Value) }
func (il *IntLiteral) exprNode()      {}

type BoolLiteral struct {
	Pos_  Position
	Value bool
	Type  types.Type
}

func (b *BoolLiteral) Pos() Position { return b.Pos_ }
func (b *BoolLiteral) End() Position {
	l := 4
	if !b.Value {
		l = 5
	}
	return Position{Line: b.Pos_.Line, Col: b.Pos_.Col + l}
}
func (b *BoolLiteral) String() string { return fmt.Sprintf("%t", b.Value) }
func (b *BoolLiteral) exprNode()      {}

type StringLiteral struct {
	Pos_  Position
	Value string
	Type  types.Type
}

func (sl *StringLiteral) Pos() Position { return sl.Pos_ }
func (sl *StringLiteral) End() Position {
	return Position{Line: sl.Pos_.Line, Col: sl.Pos_.Col + len(sl.Value) + 2}
}
func (sl *StringLiteral) String() string { return fmt.Sprintf("%q", sl.Value) }
func (sl *StringLiteral) exprNode()      {}

// =============================================================================
// Expressions — arithmetic and logic
// =============================================================================

// PrefixExpr represents unary prefix operators (`!`, `-`).
type PrefixExpr struct {
	Pos_     Position
	Operator string
	Right    Expr
	Type     types.Type
}

func (pe *PrefixExpr) Pos() Position  { return pe.Pos_ }
func (pe *PrefixExpr) End() Position  { return pe.Right.End() }
func (pe *PrefixExpr) String() string { return fmt.Sprintf("(%s%s)", pe.Operator, pe.Right.String()) }
func (pe *PrefixExpr) exprNode()      {}

// InfixExpr represents binary operators. In the HIR, `&&` and `||` no longer
// appear here — they have been rewritten to IfExpr by the lowering pass.
// Valid operators: +  -  *  /  %  ==  !=  <  <=  >  >=
type InfixExpr struct {
	Pos_     Position
	Left     Expr
	Operator string
	Right    Expr
	Type     types.Type
}

func (ie *InfixExpr) Pos() Position { return ie.Left.Pos() }
func (ie *InfixExpr) End() Position { return ie.Right.End() }
func (ie *InfixExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", ie.Left.String(), ie.Operator, ie.Right.String())
}
func (ie *InfixExpr) exprNode() {}

// =============================================================================
// Expressions — control flow
// =============================================================================

// IfExpr is the HIR's sole conditional construct. It covers:
//   - source-level if/else
//   - desugared && / ||
//   - desugared match arms (nested IfExpr chains)
//
// When used as a statement (inside ExprStmt), its Type is UnitType if no
// alternative branch is present. When used as an expression both branches
// must be present and their types must unify.
type IfExpr struct {
	Pos_        Position
	Condition   Expr
	Consequence *BlockStmt
	Alternative *BlockStmt // nil → no else branch
	Type        types.Type
}

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

// AwaitExpr suspends the current async function until the Task resolves.
type AwaitExpr struct {
	Pos_  Position
	Value Expr
	Type  types.Type
}

func (a *AwaitExpr) Pos() Position  { return a.Pos_ }
func (a *AwaitExpr) End() Position  { return a.Value.End() }
func (a *AwaitExpr) String() string { return fmt.Sprintf("(await %s)", a.Value.String()) }
func (a *AwaitExpr) exprNode()      {}

// =============================================================================
// Expressions — calls
// =============================================================================

// CallArg wraps a single function argument
type CallArg struct {
	Pos_     Position
	Argument Expr
	Mut      bool
}

// CallExpr is the only call form in the HIR. MethodCallExpr from the TAST has
// been lowered: the receiver becomes argument 0 and the method's mangled
// runtime name becomes the Function identifier.
type CallExpr struct {
	Pos_      Position
	Function  Expr
	Arguments []CallArg
	Type      types.Type
}

func (ce *CallExpr) Pos() Position { return ce.Pos_ }
func (ce *CallExpr) End() Position {
	if len(ce.Arguments) > 0 {
		return ce.Arguments[len(ce.Arguments)-1].Argument.End()
	}
	return ce.Function.End()
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
func (ce *CallExpr) exprNode() {}

// =============================================================================
// Expressions — field / index access
// =============================================================================

// FieldAccess reads a named field from a struct value.
type FieldAccess struct {
	Pos_   Position
	Object Expr
	Field  *Identifier
	Type   types.Type
}

func (fa *FieldAccess) Pos() Position { return fa.Pos_ }
func (fa *FieldAccess) End() Position { return fa.Field.End() }
func (fa *FieldAccess) String() string {
	return fmt.Sprintf("(%s).%s", fa.Object.String(), fa.Field.String())
}
func (fa *FieldAccess) exprNode() {}

// IndexExpr indexes into a fixed-size array or string (by integer).
// In the HIR, map and vector indexing have been rewritten to MapReadExpr and
// VecReadExpr respectively; this node only remains for arrays/strings/slices.
type IndexExpr struct {
	Pos_  Position
	Left  Expr
	Index Expr
	Type  types.Type
}

func (ie *IndexExpr) Pos() Position { return ie.Pos_ }
func (ie *IndexExpr) End() Position { return ie.Index.End() }
func (ie *IndexExpr) String() string {
	return fmt.Sprintf("(%s[%s])", ie.Left.String(), ie.Index.String())
}
func (ie *IndexExpr) exprNode() {}

// SliceExpr produces a view (fat pointer) into a contiguous sequence.
type SliceExpr struct {
	Pos_ Position
	Left Expr
	Low  Expr // nil → slice from beginning
	High Expr // nil → slice to end
	Type types.Type
}

func (se *SliceExpr) Pos() Position { return se.Pos_ }
func (se *SliceExpr) End() Position {
	if se.High != nil {
		return se.High.End()
	}
	return se.Left.End()
}
func (se *SliceExpr) String() string {
	lo, hi := "", ""
	if se.Low != nil {
		lo = se.Low.String()
	}
	if se.High != nil {
		hi = se.High.String()
	}
	return fmt.Sprintf("(%s[%s:%s])", se.Left.String(), lo, hi)
}
func (se *SliceExpr) exprNode() {}

// =============================================================================
// Expressions — collection literals
// =============================================================================

// StructField is one key-value pair inside a StructLiteral.
type StructField struct {
	Pos_  Position
	End_  Position
	Key   *Identifier
	Value Expr
}

// StructLiteral constructs a value of a named struct type.
type StructLiteral struct {
	Pos_   Position
	End_   Position
	Fields []StructField
	Type   types.Type
}

func (sl *StructLiteral) Pos() Position { return sl.Pos_ }
func (sl *StructLiteral) End() Position { return sl.End_ }
func (sl *StructLiteral) String() string {
	var fields []string
	for _, f := range sl.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Key.String(), f.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", sl.Type.String(), strings.Join(fields, ", "))
}
func (sl *StructLiteral) exprNode() {}

// ArrayLiteral constructs a fixed-size array value.
type ArrayLiteral struct {
	Pos_     Position
	End_     Position
	Elements []Expr
	Type     types.Type
}

func (al *ArrayLiteral) Pos() Position { return al.Pos_ }
func (al *ArrayLiteral) End() Position { return al.End_ }
func (al *ArrayLiteral) String() string {
	var elems []string
	for _, e := range al.Elements {
		elems = append(elems, e.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}
func (al *ArrayLiteral) exprNode() {}

// MapElement is one key-value pair inside a MapLiteral.
type MapElement struct {
	Key   Expr
	Value Expr
}

// MapLiteral constructs a Map<K,V> value.
type MapLiteral struct {
	Pos_     Position
	End_     Position
	Elements []MapElement
	Type     types.MapType
}

func (ml *MapLiteral) Pos() Position { return ml.Pos_ }
func (ml *MapLiteral) End() Position { return ml.End_ }
func (ml *MapLiteral) String() string {
	var elems []string
	for _, e := range ml.Elements {
		elems = append(elems, fmt.Sprintf("%s: %s", e.Key.String(), e.Value.String()))
	}
	return fmt.Sprintf("%s{%s}", ml.Type.String(), strings.Join(elems, ", "))
}
func (ml *MapLiteral) exprNode() {}

// VecLiteral constructs a Vec<T> value.
type VecLiteral struct {
	Pos_     Position
	End_     Position
	Elements []Expr
	Type     types.VectorType
}

func (vl *VecLiteral) Pos() Position { return vl.Pos_ }
func (vl *VecLiteral) End() Position { return vl.End_ }
func (vl *VecLiteral) String() string {
	var elems []string
	for _, e := range vl.Elements {
		elems = append(elems, e.String())
	}
	return fmt.Sprintf("%s[%s]", vl.Type.String(), strings.Join(elems, ", "))
}
func (vl *VecLiteral) exprNode() {}

// =============================================================================
// Expressions — sum type construction
// =============================================================================

// VariantField is one named field in a struct-style VariantLiteral.
type VariantField struct {
	Name  string
	Value Expr
}

// VariantLiteral constructs a value of a sum type variant. It covers all three
// variant shapes:
//
//   - Unit:   Arguments == nil, Fields == nil   (e.g. Loading)
//   - Tuple:  Arguments populated, Fields == nil (e.g. Active(5))
//   - Struct: Arguments == nil, Fields populated (e.g. Error{code: 404, msg: "x"})
type VariantLiteral struct {
	Pos_      Position
	End_      Position
	Variant   *types.SumVariant
	Arguments []Expr         // populated for tuple variants
	Fields    []VariantField // populated for struct variants
	Type      types.Type     // always the parent SumType
}

func (vl *VariantLiteral) Pos() Position  { return vl.Pos_ }
func (vl *VariantLiteral) End() Position  { return vl.End_ }
func (vl *VariantLiteral) String() string { return vl.Variant.Name + "(...)" }
func (vl *VariantLiteral) exprNode()      {}

// =============================================================================
// Expressions — sum type destruction (produced by match lowering)
// =============================================================================

// VariantDiscriminantExpr reads the integer discriminant tag from a sum-type
// value. It is produced during match lowering to form the condition of each
// desugared arm:
//
//	discriminant(subject) == <constant>
//
// The Type is always IntType.
type VariantDiscriminantExpr struct {
	Pos_   Position
	Object Expr       // the sum-type value being inspected
	Type   types.Type // always IntType{}
}

func (e *VariantDiscriminantExpr) Pos() Position { return e.Pos_ }
func (e *VariantDiscriminantExpr) End() Position { return e.Object.End() }
func (e *VariantDiscriminantExpr) String() string {
	return fmt.Sprintf("discriminant(%s)", e.Object.String())
}
func (e *VariantDiscriminantExpr) exprNode() {}

// VariantReadExpr extracts a single payload field from a sum-type value.
// It is produced during match lowering to bind pattern variables in the
// consequence block of each desugared arm.
//
// For tuple variants, FieldIndex is the zero-based position of the tuple
// element (0, 1, 2, …).
//
// For struct variants, FieldIndex is the zero-based position of the named
// field as declared in the type definition — and FieldName provides the
// human-readable name for debugging/printing.
//
// The type of the extracted value is carried in Type.
type VariantReadExpr struct {
	Pos_        Position
	Object      Expr       // the sum-type value to extract from
	VariantName string     // name of the variant (e.g. "Active", "Error")
	FieldIndex  int        // zero-based index into tuple or struct fields
	FieldName   string     // empty for tuple fields; set for struct fields
	Type        types.Type // type of the extracted payload field
}

func (e *VariantReadExpr) Pos() Position { return e.Pos_ }
func (e *VariantReadExpr) End() Position { return e.Object.End() }
func (e *VariantReadExpr) String() string {
	if e.FieldName != "" {
		return fmt.Sprintf("%s.%s::%s", e.Object.String(), e.VariantName, e.FieldName)
	}
	return fmt.Sprintf("%s.%s[%d]", e.Object.String(), e.VariantName, e.FieldIndex)
}
func (e *VariantReadExpr) exprNode() {}

// =============================================================================
// Expressions — runtime collection operations
// =============================================================================

// MapReadExpr calls the runtime `maml_map_get` function to retrieve the value
// associated with Key in Map. It is produced from both `map[key]` index
// expressions and `map.get(key)` method calls during lowering.
type MapReadExpr struct {
	Pos_ Position
	Map  Expr
	Key  Expr
	Type types.Type // the map's value type V
}

func (e *MapReadExpr) Pos() Position  { return e.Pos_ }
func (e *MapReadExpr) End() Position  { return e.Key.End() }
func (e *MapReadExpr) String() string { return fmt.Sprintf("%s[%s]", e.Map.String(), e.Key.String()) }
func (e *MapReadExpr) exprNode()      {}

// VecReadExpr calls the runtime `maml_vec_get` function to retrieve the element
// at Index in Vec. It is produced from `vec[index]` index expressions during
// lowering.
type VecReadExpr struct {
	Pos_  Position
	Vec   Expr
	Index Expr
	Type  types.Type // the vector's element type T
}

func (e *VecReadExpr) Pos() Position  { return e.Pos_ }
func (e *VecReadExpr) End() Position  { return e.Index.End() }
func (e *VecReadExpr) String() string { return fmt.Sprintf("%s[%s]", e.Vec.String(), e.Index.String()) }
func (e *VecReadExpr) exprNode()      {}

type VecWriteStmt struct {
	Pos_  Position
	Vec   Expr
	Index Expr
	Value Expr
}

func (s *VecWriteStmt) Pos() Position { return s.Pos_ }
func (s *VecWriteStmt) End() Position { return s.Value.End() }
func (s *VecWriteStmt) String() string {
	return fmt.Sprintf("%s[%s] = %s", s.Vec.String(), s.Index.String(), s.Value.String())
}
func (s *VecWriteStmt) stmtNode() {}

type VecPushStmt struct {
	Pos_  Position
	Vec   Expr
	Value Expr
}

func (s *VecPushStmt) Pos() Position { return s.Pos_ }
func (s *VecPushStmt) End() Position { return s.Value.End() }
func (s *VecPushStmt) String() string {
	return fmt.Sprintf("%s << %s", s.Vec.String(), s.Value.String())
}
func (s *VecPushStmt) stmtNode() {}

// =============================================================================
// Operand — atomic expressions safe for direct use in MIR instructions
// =============================================================================

// Operand marks expressions that are fully flattened and atomic — no nested
// sub-expressions. Later passes (MIR construction) can assume an Operand needs
// no further decomposition.
type Operand interface {
	Expr
	isOperand()
}

// Only primitive leaf expressions implement Operand.
func (*Identifier) isOperand()    {}
func (*IntLiteral) isOperand()    {}
func (*BoolLiteral) isOperand()   {}
func (*StringLiteral) isOperand() {}

// =============================================================================
// TypeOf — extract the resolved type from any HIR expression
// =============================================================================

// TypeOf returns the types.Type bound to expr, or UnknownType if expr is nil
// or not a recognised HIR expression node.
func TypeOf(expr Expr) types.Type {
	if expr == nil {
		return types.UnknownType{}
	}
	switch e := expr.(type) {
	case *Identifier:
		return e.Type
	case *IntLiteral:
		return e.Type
	case *BoolLiteral:
		return e.Type
	case *StringLiteral:
		return e.Type
	case *PrefixExpr:
		return e.Type
	case *InfixExpr:
		return e.Type
	case *IfExpr:
		return e.Type
	case *CallExpr:
		return e.Type
	case *FieldAccess:
		return e.Type
	case *IndexExpr:
		return e.Type
	case *SliceExpr:
		return e.Type
	case *StructLiteral:
		return e.Type
	case *ArrayLiteral:
		return e.Type
	case *VariantLiteral:
		return e.Type
	case *VariantDiscriminantExpr:
		return e.Type
	case *VariantReadExpr:
		return e.Type
	case *MapLiteral:
		return e.Type
	case *VecLiteral:
		return e.Type
	case *MapReadExpr:
		return e.Type
	case *VecReadExpr:
		return e.Type
	case *AwaitExpr:
		return e.Type
	case *BlockStmt:
		return TypeOfBlock(e)
	}
	return types.UnknownType{}
}

// TypeOfBlock returns the yield type of a block — the type of the final
// YieldStmt if present, otherwise UnitType.
func TypeOfBlock(block *BlockStmt) types.Type {
	if block == nil || len(block.Statements) == 0 {
		return types.UnitType{}
	}
	last := block.Statements[len(block.Statements)-1]
	if y, ok := last.(*YieldStmt); ok {
		return TypeOf(y.Value)
	}
	return types.UnitType{}
}
