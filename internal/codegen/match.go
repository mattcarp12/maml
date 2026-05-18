package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

func (c *Codegen) visitMatchExpr(e *ast.MatchExpr) (CGValue, error) {
	subjectCG, err := c.evaluateExpression(e.Subject)
	if err != nil {
		return CGValue{}, err
	}

	fn := c.currentBlock.Parent
	mergeBlk := fn.NewBlock(c.newLabel("match_merge"))
	semaType := c.typeMap[e]

	// We collect (yieldValue, exitBlock) pairs for the phi node.
	type incoming struct {
		val value.Value
		blk *ir.Block
	}
	var incomings []incoming

	// For each arm, emit: pattern check → body → branch to merge.
	// Arms are checked in order; each failed check falls through to the next.
	var nextBlk *ir.Block
	for i, arm := range e.Arms {
		// The block where we check this arm's pattern.
		checkBlk := c.currentBlock
		if nextBlk != nil {
			checkBlk = nextBlk
			c.currentBlock = checkBlk
		}

		bodyBlk := fn.NewBlock(c.newLabel("match_arm"))

		// For all arms except the last, we need a "no match" fallthrough block.
		// For the last arm (or wildcard), we branch unconditionally into the body.
		isLast := i == len(e.Arms)-1
		_, isWild := arm.Pattern.(*ast.WildcardPattern)

		if isLast || isWild {
			// Unconditional branch into body — this arm always matches.
			c.currentBlock.NewBr(bodyBlk)
			nextBlk = nil
		} else {
			nextBlk = fn.NewBlock(c.newLabel("match_next"))
			condVal, err := c.compilePatternCheck(subjectCG, arm.Pattern)
			if err != nil {
				return CGValue{}, err
			}
			c.currentBlock.NewCondBr(condVal, bodyBlk, nextBlk)
		}

		// Emit the arm body.
		c.currentBlock = bodyBlk
		c.pushEnv()
		if err := c.injectPatternBindings(subjectCG, arm.Pattern); err != nil {
			c.popEnv()
			return CGValue{}, err
		}

		// Save and restore bindingTypes around the arm body so bindings
		// from one arm don't leak into the next.
		savedBindingTypes := make(map[string]sema.Type, len(c.bindingTypes))
		for k, v := range c.bindingTypes {
			savedBindingTypes[k] = v
		}

		yieldVal, err := c.compileBlockStmt(arm.Body)

		c.bindingTypes = savedBindingTypes
		c.popEnv()
		if err != nil {
			return CGValue{}, err
		}

		if c.currentBlock.Term == nil {
			c.currentBlock.NewBr(mergeBlk)
			if yieldVal != nil && semaType != nil && !semaType.Equals(sema.UnknownType{}) {
				incomings = append(incomings, incoming{val: yieldVal, blk: c.currentBlock})
			}
		}
	}

	// If there was a leftover nextBlk (match fell off the end with no wildcard),
	// branch it to merge — exhaustiveness checker in sema should have caught this.
	if nextBlk != nil {
		c.currentBlock = nextBlk
		c.currentBlock.NewBr(mergeBlk)
	}

	c.currentBlock = mergeBlk

	if len(incomings) > 0 && semaType != nil && !semaType.Equals(sema.UnknownType{}) {
		phiIncoming := make([]*ir.Incoming, len(incomings))
		for i, inc := range incomings {
			phiIncoming[i] = ir.NewIncoming(inc.val, inc.blk)
		}
		phi := mergeBlk.NewPhi(phiIncoming...)
		return CGValue{V: phi, Type: semaType, IsAddress: false}, nil
	}

	return CGValue{}, nil
}

func (c *Codegen) compilePatternCheck(subjectCG CGValue, pat ast.Pattern) (value.Value, error) {
	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return constant.True, nil

	case *ast.LiteralPattern:
		subjectVal := c.load(subjectCG)
		switch lit := p.Value.(type) {
		case *ast.IntLiteral:
			return c.currentBlock.NewICmp(enum.IPredEQ, subjectVal,
				constant.NewInt(types.I32, lit.Value)), nil
		case *ast.BoolLiteral:
			boolConst := constant.False
			if lit.Value {
				boolConst = constant.True
			}
			return c.currentBlock.NewICmp(enum.IPredEQ, subjectVal, boolConst), nil
		}

	case *ast.VariantPattern:
		// Load the discriminant (field 0 of the tagged union).
		discriminant, err := c.loadSumDiscriminant(subjectCG)
		if err != nil {
			return nil, err
		}
		// Find expected discriminant from the sum type.
		expected := int64(0)
		if sumTy, ok := subjectCG.Type.(*sema.SumType); ok {
			if v := sumTy.GetVariant(p.Name); v != nil {
				expected = int64(v.Discriminant)
			}
		}
		return c.currentBlock.NewICmp(enum.IPredEQ, discriminant,
			constant.NewInt(types.I32, expected)), nil
	}

	return nil, fmt.Errorf("unsupported pattern type: %T", pat)
}

// loadSumDiscriminant loads field 0 (the i32 discriminant) from a sum type value.
func (c *Codegen) loadSumDiscriminant(subjectCG CGValue) (value.Value, error) {
	sumTy, ok := subjectCG.Type.(*sema.SumType)
	if !ok {
		return nil, fmt.Errorf("loadSumDiscriminant: not a SumType: %T", subjectCG.Type)
	}
	llvmType := c.llvmTypeFor(sumTy)
	zero := constant.NewInt(types.I32, 0)

	var ptr value.Value
	if subjectCG.IsAddress {
		ptr = subjectCG.V
	} else {
		alloc := c.currentBlock.NewAlloca(llvmType)
		c.currentBlock.NewStore(subjectCG.V, alloc)
		ptr = alloc
	}

	discrimPtr := c.currentBlock.NewGetElementPtr(llvmType, ptr, zero, zero)
	return c.currentBlock.NewLoad(types.I32, discrimPtr), nil
}

func (c *Codegen) injectPatternBindings(subjectCG CGValue, pat ast.Pattern) error {
	vp, ok := pat.(*ast.VariantPattern)
	if !ok {
		return nil
	}

	sumTy, ok := subjectCG.Type.(*sema.SumType)
	if !ok {
		return nil
	}

	variant := sumTy.GetVariant(vp.Name)
	if variant == nil || variant.IsUnit() {
		return nil
	}

	llvmType := c.llvmTypeFor(sumTy)
	zero := constant.NewInt(types.I32, 0)

	var ptr value.Value
	if subjectCG.IsAddress {
		ptr = subjectCG.V
	} else {
		alloc := c.currentBlock.NewAlloca(llvmType)
		c.currentBlock.NewStore(subjectCG.V, alloc)
		ptr = alloc
	}

	// Get pointer to the payload ([MaxPayload x i8] at field 1).
	payloadPtr := c.currentBlock.NewGetElementPtr(llvmType, ptr, zero,
		constant.NewInt(types.I32, 1))

	// Reconstruct the variant's payload struct type for field access.
	var payloadFieldTypes []types.Type
	for _, vf := range variant.Fields {
		payloadFieldTypes = append(payloadFieldTypes, c.llvmTypeFor(vf.Type))
	}
	payloadStructType := types.NewStruct(payloadFieldTypes...)

	// Bitcast the payload bytes to the typed struct pointer.
	typedPayloadPtr := c.currentBlock.NewBitCast(payloadPtr, types.NewPointer(payloadStructType))

	// Single binding: case Circle(c) — only valid when variant has exactly one field.
	if vp.Binding != nil && len(variant.Fields) == 1 {
		fieldPtr := c.currentBlock.NewGetElementPtr(payloadStructType, typedPayloadPtr,
			zero, zero)
		c.setVar(vp.Binding.Value, fieldPtr, false)
		c.bindingTypes[vp.Binding.Value] = variant.Fields[0].Type
		return nil
	}

	// Field bindings: case Circle{radius: r}
	for _, fb := range vp.Fields {
		fieldIndex := -1
		var fieldSemaType sema.Type
		for i, vf := range variant.Fields {
			if vf.Name == fb.Field {
				fieldIndex = i
				fieldSemaType = vf.Type
				break
			}
		}
		if fieldIndex < 0 {
			return fmt.Errorf("unknown field '%s' in variant '%s'", fb.Field, variant.Name)
		}
		if fb.Binding != nil {
			fieldPtr := c.currentBlock.NewGetElementPtr(payloadStructType, typedPayloadPtr,
				zero, constant.NewInt(types.I32, int64(fieldIndex)))
			c.setVar(fb.Binding.Value, fieldPtr, false)
			c.bindingTypes[fb.Binding.Value] = fieldSemaType
		}
	}

	return nil
}
