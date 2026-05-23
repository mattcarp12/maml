// ============================================================
// FILE: internal/ast/async.go
// ============================================================

package ast

import "fmt"

// AwaitExpr represents:  await <expr>
//
// It is a distinct node (not a PrefixExpr) because:
//   - it introduces a coroutine suspension point
//   - it splits control flow at the boundary
//   - it carries different lifetime / ownership semantics
//   - it requires special lowering to LLVM coroutine intrinsics
type AwaitExpr struct {
	Value Expr
	Pos_  Position
}

func (a *AwaitExpr) Pos() Position { return a.Pos_ }
func (a *AwaitExpr) End() Position { return a.Value.End() }
func (a *AwaitExpr) String() string {
	return fmt.Sprintf("(await %s)", a.Value.String())
}
func (a *AwaitExpr) exprNode() {}

// -----------------------------------------------------------------------------
// Explicit Async Coroutine Setup
// -----------------------------------------------------------------------------

// AsyncPrologueExpr represents the explicit allocation and setup of an LLVM coroutine.
// It instructs the backend to emit coro.id, coro.size, coro.begin, and allocate the Promise.
type AsyncPrologueExpr struct {
	Pos_ Position
}

func (a *AsyncPrologueExpr) Pos() Position  { return a.Pos_ }
func (a *AsyncPrologueExpr) End() Position  { return a.Pos_ }
func (a *AsyncPrologueExpr) String() string { return "async_prologue()" }
func (a *AsyncPrologueExpr) exprNode()      {}

func (a *AsyncPrologueExpr) Clone() Node {
	return &AsyncPrologueExpr{Pos_: a.Pos_}
}
