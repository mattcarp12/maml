package sema

import (
	"github.com/mattcarp12/maml/frontend/tast"
)

// =============================================================================
// FnDecl Rules
// =============================================================================

// MainCannotBeAsync rejects an async declaration on the entry-point function.
// The runtime requires main to be synchronous: it must manually spawn tasks
// and call run_executor() rather than being an async coroutine itself.
type MainCannotBeAsync struct{}

func (r MainCannotBeAsync) Name() string { return "main-cannot-be-async" }

func (r MainCannotBeAsync) Check(node *tast.FnDecl, ctx *RuleContext) []Violation {
	if node.Name == "main" && node.IsAsync {
		return []Violation{violation(node.Pos_,
			"the 'main' function cannot be async; you must manually spawn tasks and run the executor",
		)}
	}
	return nil
}