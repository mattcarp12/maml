package mir

import (
	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

func (b *Builder) LowerVecLiteral(e *hir.VecLiteral) Value {
	tmp := b.newTemp()
	t := e.Type
	b.locals[tmp] = t // 'tmp' is now the Vector struct value on the stack
	obj := &Register{Name: tmp, Type: t}

	// Inline construct the { buffer, cap, len, elem_size } struct directly
	b.storeField(obj, t, &Register{Name: "null", Type: types.PtrType{}}, "buffer", 0, types.PtrType{})
	b.storeField(obj, t, &IntConstant{Value: 0, Type: types.U32Type{}}, "cap", 1, types.U32Type{})
	b.storeField(obj, t, &IntConstant{Value: 0, Type: types.U32Type{}}, "len", 2, types.U32Type{})
	b.storeField(obj, t, &IntConstant{Value: int64(SizeOf(t.Base, b.Target)), Type: types.U32Type{}}, "elem_size", 3, types.U32Type{})

	// If there are elements to push, we must take the address of the stack struct to pass to the ABI
	if len(e.Elements) > 0 {
		vecPtrReg := b.emitBorrow(tmp, true)

		for _, elem := range e.Elements {
			flatElem := hir.LowerNode(elem, b)
			boxedElem := b.boxScalar(flatElem, t.Base)
			b.EmitMamlVecPush(vecPtrReg, boxedElem)
		}
	}

	return obj
}

func (b *Builder) LowerVecReadExpr(e *hir.VecReadExpr) Value {
	vecPtrVal := b.addressOf(e.Vec)
	flatIdx := hir.LowerNode(e.Index, b)

	opaquePtr := b.EmitMamlVecGet(vecPtrVal, flatIdx)
	return b.emitLoad(opaquePtr, e.Type)
}

func (b *Builder) LowerVecPushStmt(n *hir.VecPushStmt) Value {
	if n == nil {
		return unitValue
	}
	vecPtrVal := b.addressOf(n.Vec)
	flatVal := hir.LowerNode(n.Value, b)
	elemType := getValueType(flatVal)

	b.EmitMamlVecPush(vecPtrVal, b.boxScalar(flatVal, elemType))
	return unitValue
}

func (b *Builder) LowerVecWriteStmt(n *hir.VecWriteStmt) Value {
	if n == nil {
		return unitValue
	}
	vecPtrVal := b.addressOf(n.Vec)
	flatIdx := hir.LowerNode(n.Index, b)

	var writeVal Value
	var elemType types.Type
	if n.Operator != "" {
		opaquePtr := b.EmitMamlVecGet(vecPtrVal, flatIdx)
		elemType = hir.TypeOf(n.Value)
		writeVal = b.emitCompoundMath(opaquePtr, n.Operator, n.Value, elemType)
	} else {
		writeVal = hir.LowerNode(n.Value, b)
		elemType = getValueType(writeVal)
	}

	boxedVal := b.boxScalar(writeVal, elemType)
	b.EmitMamlVecSet(vecPtrVal, flatIdx, boxedVal)
	return unitValue
}
