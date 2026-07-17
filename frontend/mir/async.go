package mir

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

func (b *Builder) LowerAwaitExpr(e *hir.AwaitExpr) Value {
	flatTask := hir.LowerNode(e.Value, b)
	b.EmitMamlTaskAwait(flatTask, b.currentFuture)
	resumeBlock := b.emitCoroSuspend()
	b.current = resumeBlock
	return b.emitGetFutureResult(flatTask, e.Type)
}

func (b *Builder) LowerSpawnExpr(e *hir.SpawnExpr) Value {
	flatFuture := hir.LowerNode(e.Value, b)
	b.EmitMamlSpawnTask(flatFuture)
	return flatFuture
}

func (b *Builder) lowerYieldNow(_ *hir.CallExpr) Value {
	b.EmitMamlYieldNow(b.currentFuture)
	resumeBlock := b.emitCoroSuspend()
	b.current = resumeBlock
	return unitValue
}

func (b *Builder) lowerRunExecutor(e *hir.CallExpr) Value {
	flatArg := hir.LowerNode(e.Arguments[0].Argument, b)
	b.EmitMamlRunExecutor(flatArg)
	return b.emitGetFutureResult(flatArg, e.Type)
}

func (b *Builder) emitGetFutureResult(futureVal Value, resultType types.Type) Value {
	// If the future resolves to void, there's nothing to load from the promise slot.
	if _, isUnit := resultType.(types.UnitType); isUnit {
		return unitValue
	}

	// futureVal IS the raw coroutine handle now. No need to extract struct fields!

	// 1. Emit explicit promise pointer instruction directly from the handle
	promisePtr := b.newTemp()
	b.locals[promisePtr] = types.PtrType{}
	b.push(&CoroPromisePtrInst{
		Dst:    promisePtr,
		Handle: futureVal, // Pass the handle directly here
		Type:   types.PtrType{},
	})
	// 2. Load the actual return value from the promise pointer
	res := b.newTemp()
	b.locals[res] = resultType
	b.push(&LoadPtrInst{
		Dst:  res,
		Ptr:  &Register{Name: promisePtr, Type: types.PtrType{}},
		Type: resultType,
	})
	return &Register{Name: res, Type: resultType}
}
