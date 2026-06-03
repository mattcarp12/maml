package hir

import (
	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// Lowerer handles the translation of a type-checked TAST into
// the High-Level Intermediate Representation (HIR).
type Lowerer struct {
	// Any state needed during lower passes (such as tracking labels or symbols) can go here.
}

// NewLowerer creates a new HIR lowerer instance.
func NewLowerer() *Lowerer {
	return &Lowerer{}
}

// LowerProgram is the entry point for the HIR phase.
// It translates an entire TAST program into an immutable HIR program tree.
func (l *Lowerer) LowerProgram(prog *tast.Program) *Program {
	if prog == nil {
		return nil
	}

	var hirDecls []Decl
	for _, decl := range prog.Decls {
		if hirDecl := l.lowerDecl(decl); hirDecl != nil {
			hirDecls = append(hirDecls, hirDecl)
		}
	}

	return &Program{
		Decls: hirDecls,
	}
}

// lowerDecl dispatches declaration conversion to specific sub-methods.
func (l *Lowerer) lowerDecl(decl tast.Decl) Decl {
	if decl == nil {
		return nil
	}

	switch d := decl.(type) {
	case *tast.FnDecl:
		return l.lowerFnDecl(d)
	case *tast.TypeDecl:
		return l.lowerTypeDecl(d)
	default:
		return nil
	}
}

// lowerFnDecl translates a function's signature and structural metadata.
func (l *Lowerer) lowerFnDecl(fn *tast.FnDecl) *FnDecl {
	// Lower parameters cleanly, preserving original symbols and positions
	hirParams := make([]*Param, len(fn.Params))
	for i, p := range fn.Params {
		hirParams[i] = &Param{
			Pos_:   p.Pos_,
			End_:   p.End_,
			Name:   p.Name,
			Type:   p.Type,
			Symbol: p.Symbol,
		}
	}

	hirFn := &FnDecl{
		Pos_:       fn.Pos_,
		End_:       fn.End_,
		Name:       fn.Name,
		Params:     hirParams,
		ReturnType: fn.ReturnType,
		IsAsync:    fn.IsAsync,
		Symbol:     fn.Symbol,
	}

	// Function body translation will be implemented during Phase 2.
	// For Phase 1 foundational tracking, we leave it unmapped or forward it.
	if fn.Body != nil {
		hirFn.Body = l.lowerBlockStmt(fn.Body)
	}

	return hirFn
}

// lowerTypeDecl translates type declarations into structural HIR type nodes.
func (l *Lowerer) lowerTypeDecl(td *tast.TypeDecl) *TypeDecl {
	var hirIdent *Identifier
	if td.Name != nil {
		hirIdent = &Identifier{
			Pos_:   td.Name.Pos_,
			End_:   td.Name.End_,
			Value:  td.Name.Value,
			Type:   td.Name.Type,
			Symbol: td.Name.Symbol,
		}
	}

	return &TypeDecl{
		Pos_:   td.Pos_,
		End_:   td.End_,
		Name:   hirIdent,
		Type:   td.Type,
		Symbol: td.Symbol,
	}
}

// lowerBlockStmt recursively converts a TAST block statement into an immutable HIR block.
func (l *Lowerer) lowerBlockStmt(block *tast.BlockStmt) *BlockStmt {
	if block == nil {
		return nil
	}

	var hirStmts []Stmt
	for _, stmt := range block.Statements {
		if hirStmt := l.lowerStmt(stmt); hirStmt != nil {
			hirStmts = append(hirStmts, hirStmt)
		}
	}

	return &BlockStmt{
		Pos_:       block.Pos_,
		End_:       block.End_,
		Statements: hirStmts,
	}
}

// lowerStmt serves as the central dispatcher for statement mapping.
func (l *Lowerer) lowerStmt(stmt tast.Stmt) Stmt {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *tast.BlockStmt:
		return l.lowerBlockStmt(s)

	case *tast.DeclareStmt:
		return &DeclareStmt{
			Pos_:   s.Pos_,
			Value:  l.lowerExpr(s.Value),
			Symbol: s.Symbol,
		}

	case *tast.AssignStmt:
		// Map and Vector direct bracket mutations (e.g. map[k] = v) drop down into
		// clean, dedicated HIR statement nodes instead of general assignments.
		if idxExpr, ok := s.LValue.(*tast.IndexExpr); ok {
			leftType := tast.TypeOf(idxExpr.Left)
			switch leftType.(type) {
			case types.MapType:
				return &MapInsertStmt{
					Pos_:  s.Pos_,
					Map:   l.lowerExpr(idxExpr.Left),
					Key:   l.lowerExpr(idxExpr.Index),
					Value: l.lowerExpr(s.RValue),
				}
			case types.VectorType:
				return &VecWriteStmt{
					Pos_:  s.Pos_,
					Vec:   l.lowerExpr(idxExpr.Left),
					Index: l.lowerExpr(idxExpr.Index),
					Value: l.lowerExpr(s.RValue),
				}
			}
		}

		return &AssignStmt{
			Pos_:   s.Pos_,
			LValue: l.lowerExpr(s.LValue),
			RValue: l.lowerExpr(s.RValue),
		}

	case *tast.ReturnStmt:
		return &ReturnStmt{
			Pos_:  s.Pos_,
			Value: l.lowerExpr(s.Value),
		}

	case *tast.YieldStmt:
		return &YieldStmt{
			Pos_:  s.Pos_,
			Value: l.lowerExpr(s.Value),
		}

	case *tast.BreakStmt:
		return &BreakStmt{
			Pos_: s.Pos_,
			End_: s.End_,
		}

	case *tast.ContinueStmt:
		return &ContinueStmt{
			Pos_: s.Pos_,
			End_: s.End_,
		}

	case *tast.ExprStmt:
		// Clean up standalone expressions. If an expression lowers to an HIR node
		// that is already structurally a statement (like an intercepted vector push),
		// we unwrap it and bubble it up directly as a true statement.
		loweredExpr := l.lowerExpr(s.Value)
		if stmtLike, isStmt := loweredExpr.(Stmt); isStmt {
			return stmtLike
		}
		return &ExprStmt{
			Pos_:  s.Pos_,
			Value: loweredExpr,
		}

	case *tast.ForStmt:
		return l.lowerForStmt(s)

	default:
		return nil
	}
}

// lowerForStmt translates loops into canonical structures, preserving Post for safe continue jumps.
func (l *Lowerer) lowerForStmt(s *tast.ForStmt) Stmt {
	var hirInit Stmt
	if s.Init != nil {
		hirInit = l.lowerStmt(s.Init)
	}

	var hirCond Expr
	if s.Condition != nil {
		hirCond = l.lowerExpr(s.Condition)
	}

	var hirPost Stmt
	if s.Post != nil {
		hirPost = l.lowerStmt(s.Post)
	}

	var hirBody *BlockStmt
	if s.Body != nil {
		hirBody = l.lowerBlockStmt(s.Body)
	}

	// Build the foundational loop, keeping Post intact
	actualLoop := &LoopStmt{
		Pos_:      s.Pos_,
		Condition: hirCond,
		Post:      hirPost,
		Body:      hirBody,
	}

	// If there's no initialization step, return the loop directly
	if hirInit == nil {
		return actualLoop
	}

	// Transform classic C-style: for init; cond; post { body }
	// into an outer BlockStmt containing the initialization step and the loop
	return &BlockStmt{
		Pos_:       s.Pos_,
		End_:       s.Body.End(), // Assuming s.Body isn't nil, otherwise s.End()
		Statements: []Stmt{hirInit, actualLoop},
	}
}

// lowerExpr dispatches a type-checked TAST expression to its corresponding HIR conversion.
func (l *Lowerer) lowerExpr(expr tast.Expr) Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *tast.Identifier:
		return &Identifier{
			Pos_:   e.Pos_,
			End_:   e.End_,
			Value:  e.Value,
			Type:   e.Type,
			Symbol: e.Symbol,
		}

	case *tast.IntLiteral:
		return &IntLiteral{Pos_: e.Pos_, End_: e.End_, Value: e.Value, Type: e.Type}

	case *tast.BoolLiteral:
		return &BoolLiteral{Pos_: e.Pos_, Value: e.Value, Type: e.Type}

	case *tast.StringLiteral:
		return &StringLiteral{Pos_: e.Pos_, Value: e.Value, Type: e.Type}

	case *tast.PrefixExpr:
		return &PrefixExpr{
			Pos_:     e.Pos_,
			Operator: e.Operator,
			Right:    l.lowerExpr(e.Right),
			Type:     e.Type,
		}

	case *tast.InfixExpr:
		return l.lowerInfixExpr(e)

	case *tast.IfExpr:
		return &IfExpr{
			Pos_:        e.Pos_,
			Condition:   l.lowerExpr(e.Condition),
			Consequence: l.lowerBlockStmt(e.Consequence),
			Alternative: l.lowerBlockStmt(e.Alternative),
			Type:        e.Type,
		}

	case *tast.CallExpr:
		return l.lowerCallExpr(e)

	case *tast.MatchExpr:
		return l.lowerMatchExpr(e)

	case *tast.FieldAccess:
		return &FieldAccess{
			Pos_:   e.Pos_,
			Object: l.lowerExpr(e.Object),
			Field:  l.lowerExpr(e.Field).(*Identifier),
			Type:   e.Type,
		}

	case *tast.IndexExpr:
		return l.lowerIndexExpr(e)

	case *tast.SliceExpr:
		return &SliceExpr{
			Pos_: e.Pos_,
			Left: l.lowerExpr(e.Left),
			Low:  l.lowerExpr(e.Low),
			High: l.lowerExpr(e.High),
			Type: e.Type,
		}

	case *tast.StructLiteral:
		var fields []StructField
		for _, f := range e.Fields {
			fields = append(fields, StructField{
				Pos_:  f.Pos_,
				Key:   l.lowerExpr(f.Key).(*Identifier), // Cast strictly to *Identifier
				Value: l.lowerExpr(f.Value),
			})
		}
		return &StructLiteral{Pos_: e.Pos_, End_: e.End_, Fields: fields, Type: e.Type}

	case *tast.ArrayLiteral:
		var elems []Expr
		for _, elem := range e.Elements {
			elems = append(elems, l.lowerExpr(elem))
		}
		return &ArrayLiteral{Pos_: e.Pos_, Elements: elems, Type: e.Type}

	case *tast.MapLiteral:
		var elements []MapElement
		for _, el := range e.Elements {
			elements = append(elements, MapElement{
				Key:   l.lowerExpr(el.Key),
				Value: l.lowerExpr(el.Value),
			})
		}
		return &MapLiteral{Pos_: e.Pos_, End_: e.End_, Elements: elements, Type: e.Type}

	case *tast.VecLiteral:
		var elems []Expr
		for _, elem := range e.Elements {
			elems = append(elems, l.lowerExpr(elem))
		}
		return &VecLiteral{Pos_: e.Pos_, End_: e.End_, Elements: elems, Type: e.Type}

	case *tast.VariantLiteral:
		var args []Expr
		for _, arg := range e.Arguments {
			args = append(args, l.lowerExpr(arg))
		}
		var fields []VariantField
		for _, f := range e.Fields {
			fields = append(fields, VariantField{
				Name:  f.Name,
				Value: l.lowerExpr(f.Value),
			})
		}
		return &VariantLiteral{
			Pos_:      e.Pos_,
			End_:      e.End_,
			Variant:   e.Variant,
			Arguments: args,
			Fields:    fields,
			Type:      e.Type,
		}

	case *tast.AwaitExpr:
		return &AwaitExpr{
			Pos_:  e.Pos_,
			Value: l.lowerExpr(e.Value),
			Type:  e.Type,
		}

	case *tast.BlockStmt:
		// Blocks evaluated in expression position are cleanly represented as BlockStmt nodes
		return l.lowerBlockStmt(e)

	default:
		return nil
	}
}

// lowerInfixExpr explicitly targets and decomposes short-circuit logical operators.
func (l *Lowerer) lowerInfixExpr(e *tast.InfixExpr) Expr {
	leftLowered := l.lowerExpr(e.Left)
	rightLowered := l.lowerExpr(e.Right)

	// A && B -> if A { B } else { false }
	if e.Operator == "&&" {
		falseBlock := &BlockStmt{
			Pos_: e.Pos_,
			Statements: []Stmt{
				&YieldStmt{Pos_: e.Pos_, Value: &BoolLiteral{Pos_: e.Pos_, Value: false, Type: types.BoolType{}}},
			},
		}
		trueBlock := &BlockStmt{
			Pos_: e.Pos_,
			Statements: []Stmt{
				&YieldStmt{Pos_: e.Pos_, Value: rightLowered},
			},
		}
		return &IfExpr{
			Pos_:        e.Pos_,
			Condition:   leftLowered,
			Consequence: trueBlock,
			Alternative: falseBlock,
			Type:        types.BoolType{},
		}
	}

	// A || B -> if A { true } else { B }
	if e.Operator == "||" {
		trueBlock := &BlockStmt{
			Pos_: e.Pos_,
			Statements: []Stmt{
				&YieldStmt{Pos_: e.Pos_, Value: &BoolLiteral{Pos_: e.Pos_, Value: true, Type: types.BoolType{}}},
			},
		}
		falseBlock := &BlockStmt{
			Pos_: e.Pos_,
			Statements: []Stmt{
				&YieldStmt{Pos_: e.Pos_, Value: rightLowered},
			},
		}
		return &IfExpr{
			Pos_:        e.Pos_,
			Condition:   leftLowered,
			Consequence: trueBlock,
			Alternative: falseBlock,
			Type:        types.BoolType{},
		}
	}

	// Standard mathematical or relational expressions pass through unchanged
	return &InfixExpr{
		Pos_:     e.Pos_,
		Operator: e.Operator,
		Left:     leftLowered,
		Right:    rightLowered,
		Type:     e.Type,
	}
}

// lowerCallExpr intercepts method calls on containers using MethodKind identifiers
func (l *Lowerer) lowerCallExpr(e *tast.CallExpr) Expr {
	calleeIdent, ok := e.Function.(*tast.Identifier)
	if !ok || calleeIdent.Symbol == nil {
		return l.lowerNormalCall(e)
	}

	// Intercept container structural operations via MethodKind tags
	switch calleeIdent.Symbol.MethodKind {
	case types.MethodMapPut:
		return &BlockStmt{
			Pos_: e.Pos_,
			End_: e.End_,
			Statements: []Stmt{
				&MapInsertStmt{
					Pos_:  e.Pos_,
					Map:   l.lowerExpr(e.Arguments[0].Argument),
					Key:   l.lowerExpr(e.Arguments[1].Argument),
					Value: l.lowerExpr(e.Arguments[2].Argument),
				},
			},
		}

	case types.MethodMapGet:
		return &MapReadExpr{
			Pos_: e.Pos_,
			Map:  l.lowerExpr(e.Arguments[0].Argument),
			Key:  l.lowerExpr(e.Arguments[1].Argument),
			Type: e.Type,
		}

	default:
		// MethodVecPush, MethodVecPop, MethodVecLen, MethodMapRemove
		// all fall through to standard runtime function calls.
		return l.lowerNormalCall(e)
	}
}

// lowerNormalCall produces a clean hir.CallExpr for user-defined and standard free functions.
func (l *Lowerer) lowerNormalCall(e *tast.CallExpr) *CallExpr {
	var hirArgs []CallArg
	for _, arg := range e.Arguments {
		hirArgs = append(hirArgs, CallArg{
			Pos_:     arg.Pos_,
			Argument: l.lowerExpr(arg.Argument),
			Mut:      arg.Mut,
			Own:      arg.Own,
		})
	}
	return &CallExpr{
		Pos_:      e.Pos_,
		Function:  l.lowerExpr(e.Function),
		Arguments: hirArgs,
		Type:      e.Type,
	}
}

// lowerIndexExpr extracts type bracket operations and translates them to specialized reads.
func (l *Lowerer) lowerIndexExpr(idx *tast.IndexExpr) Expr {
	leftLowered := l.lowerExpr(idx.Left)
	indexLowered := l.lowerExpr(idx.Index)
	containerType := tast.TypeOf(idx.Left)

	switch containerType.(type) {
	case types.MapType:
		return &MapReadExpr{
			Pos_: idx.Pos_,
			Map:  leftLowered,
			Key:  indexLowered,
			Type: idx.Type,
		}
	case types.VectorType:
		return &VecReadExpr{
			Pos_:  idx.Pos_,
			Vec:   leftLowered,
			Index: indexLowered,
			Type:  idx.Type,
		}
	default:
		// Fall back to a normal index expression for arrays/slices
		return &IndexExpr{
			Pos_:  idx.Pos_,
			Left:  leftLowered,
			Index: indexLowered,
			Type:  idx.Type,
		}
	}
}

// lowerMatchExpr converts high-level pattern matching blocks into a flattened
// cascade of conditional branches, preserving side-effect limits by evaluating
// the target subject expression exactly once.
func (l *Lowerer) lowerMatchExpr(e *tast.MatchExpr) Expr {
	if e == nil {
		return nil
	}

	// 1. To guarantee the match subject is evaluated exactly once, we wrap our
	// desugared cascade inside an outer BlockStmt expression that resolves via a trailing YieldStmt.
	outerBlock := &BlockStmt{
		Pos_:       e.Pos_,
		End_:       e.End_,
		Statements: []Stmt{},
	}

	subjectLowered := l.lowerExpr(e.Subject)
	subjectType := tast.TypeOf(e.Subject)

	// Create a unique internal identifier for tracking the hoisted subject value
	subjectSym := &types.Symbol{
		Kind:    types.VarSymbol,
		Name:    "$match_subject",
		Type:    subjectType,
		Mutable: false,
	}

	subjectIdent := &Identifier{
		Pos_:   e.Pos_,
		End_:   e.End_,
		Value:  "$match_subject",
		Type:   subjectType,
		Symbol: subjectSym,
	}

	// Hoist evaluation: let $match_subject = e.Subject;
	outerBlock.Statements = append(outerBlock.Statements, &DeclareStmt{
		Pos_:   e.Pos_,
		Value:  subjectLowered,
		Symbol: subjectSym,
	})

	// 2. Loop through the match arms in reverse order to stitch together
	// the linked alternative links (nested if-else structural chain).
	var finalCascade Expr = nil

	for i := len(e.Arms) - 1; i >= 0; i-- {
		arm := e.Arms[i]
		var armBodyStmts []Stmt

		// Extract or lower the body of this specific match branch arm
		loweredBodyExpr := l.lowerExpr(arm.Body)
		if block, isBlock := loweredBodyExpr.(*BlockStmt); isBlock {
			armBodyStmts = append(armBodyStmts, block.Statements...)
		} else {
			armBodyStmts = append(armBodyStmts, &YieldStmt{
				Pos_:  arm.Body.Pos(),
				Value: loweredBodyExpr,
			})
		}

		var condition Expr = nil

		switch pat := arm.Pattern.(type) {
		case *tast.WildcardPattern:
			// Unconditional baseline catch-all '_'
			condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}

		case *tast.IdentifierPattern:
			// If this identifier name is recognized as a valid constituent member of a Sum Type variant,
			// handle it as a naked structural variant comparison.
			if sumTy, ok := subjectType.(*types.SumType); ok && sumTy.GetVariant(pat.Name) != nil {
				// Variant tag comparison: VariantDiscriminant($match_subject) == <DiscriminantInteger>
				variant := sumTy.GetVariant(pat.Name)
				condition = &InfixExpr{
					Pos_:     pat.Pos(),
					Operator: "==",
					Left: &VariantDiscriminantExpr{
						Pos_:   pat.Pos(),
						Object: subjectIdent,
						Type:   types.IntType{},
					},
					Right: &IntLiteral{
						Pos_:  pat.Pos(),
						Value: int64(variant.Discriminant),
						Type:  types.IntType{},
					},
					Type: types.BoolType{},
				}
			} else {
				// Variable catch-all fallback: binds everything to a local name
				condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}

				// SYNTHESIZE the symbol here since tast.IdentifierPattern doesn't carry one
				bindingSym := &types.Symbol{
					Kind:    types.VarSymbol,
					Name:    pat.Name,
					Type:    subjectType,
					Mutable: false, // Default match bindings to immutable
				}

				decl := &DeclareStmt{
					Pos_:   pat.Pos(),
					Value:  subjectIdent,
					Symbol: bindingSym,
				}
				armBodyStmts = append([]Stmt{decl}, armBodyStmts...)
			}
		case *tast.VariantPattern:
			variantName := ""
			if pat.Variant != nil {
				variantName = pat.Variant.Value
			}

			condition = &InfixExpr{
				Pos_:     pat.Pos(),
				Operator: "==",
				Left: &VariantDiscriminantExpr{
					Pos_:   pat.Pos(),
					Object: subjectIdent,
					Type:   types.StringType{},
				},
				Right: &StringLiteral{
					Pos_:  pat.Pos(),
					Value: variantName,
					Type:  types.StringType{},
				},
				Type: types.BoolType{},
			}

			// Generate internal unpacking assignments if there are bounded tuple parameters
			if len(pat.TupleBindings) > 0 {
				for idx, identPat := range pat.TupleBindings {
					if identPat.Value != "_" {
						bindingSym := identPat.Symbol
						if bindingSym == nil {
							bindingSym = &types.Symbol{Kind: types.VarSymbol, Name: identPat.Value, Type: identPat.Type}
						}

						// Extract value via VariantReadExpr
						readExpr := &VariantReadExpr{
							Pos_:        identPat.Pos(),
							Object:      subjectIdent,
							VariantName: variantName,
							FieldIndex:  idx, // Populated properly for tuple variants
							FieldName:   "",  // Empty for tuples
							Type:        identPat.Type,
						}

						decl := &DeclareStmt{
							Pos_:   identPat.Pos(),
							Symbol: bindingSym,
							Value:  readExpr,
						}
						armBodyStmts = append([]Stmt{decl}, armBodyStmts...)
					}
				}
			}

		}

		consequenceBlock := &BlockStmt{
			Pos_:       arm.Body.Pos(),
			End_:       arm.Body.End(),
			Statements: armBodyStmts,
		}

		if finalCascade == nil {
			// Base branch condition mapping
			if boolLit, ok := condition.(*BoolLiteral); ok && boolLit.Value {
				finalCascade = consequenceBlock
			} else {
				finalCascade = &IfExpr{
					Pos_:        arm.Pos_,
					Condition:   condition,
					Consequence: consequenceBlock,
					Alternative: &BlockStmt{Pos_: arm.End_, Statements: []Stmt{}},
					Type:        e.Type,
				}
			}
		} else {
			// Wrap current alternative links into matching containers
			var alternativeBlock *BlockStmt
			if ab, ok := finalCascade.(*BlockStmt); ok {
				alternativeBlock = ab
			} else {
				alternativeBlock = &BlockStmt{
					Pos_:       finalCascade.Pos(),
					Statements: []Stmt{&YieldStmt{Pos_: finalCascade.Pos(), Value: finalCascade}},
				}
			}

			if boolLit, ok := condition.(*BoolLiteral); ok && boolLit.Value {
				// Shorthand path optimizing shadowed sub-branches
				finalCascade = consequenceBlock
			} else {
				finalCascade = &IfExpr{
					Pos_:        arm.Pos_,
					Condition:   condition,
					Consequence: consequenceBlock,
					Alternative: alternativeBlock,
					Type:        e.Type,
				}
			}
		}
	}

	// 3. Close out by yielding the fully integrated cascade out through our block expression
	outerBlock.Statements = append(outerBlock.Statements, &YieldStmt{
		Pos_:  e.Pos_,
		Value: finalCascade,
	})

	return outerBlock
}
