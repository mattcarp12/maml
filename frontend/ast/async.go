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
