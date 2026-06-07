package hir

import (
	"fmt"

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

	case *tast.VecPushStmt:
		return &VecPushStmt{
			Pos_:  s.Pos_,
			Vec:   l.lowerExpr(s.LValue),
			Value: l.lowerExpr(s.RValue),
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

// lowerNormalCall produces a clean hir.CallExpr for user-defined and standard free functions.
func (l *Lowerer) lowerCallExpr(e *tast.CallExpr) *CallExpr {
	var hirArgs []CallArg
	for _, arg := range e.Arguments {
		hirArgs = append(hirArgs, CallArg{
			Pos_:     arg.Pos_,
			Argument: l.lowerExpr(arg.Argument),
			Mut:      arg.Mut,
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

func (l *Lowerer) lowerMatchExpr(e *tast.MatchExpr) Expr {
	if e == nil {
		return nil
	}

	outerBlock := &BlockStmt{Pos_: e.Pos_, End_: e.End_, Statements: []Stmt{}}
	subjectType := tast.TypeOf(e.Subject)

	// 1. Hoist the subject evaluation
	subjectIdent := l.hoistMatchSubject(e.Subject, subjectType, outerBlock)

	// 2. Build the If/Else cascade in reverse
	var finalCascade Expr = nil

	for i := len(e.Arms) - 1; i >= 0; i-- {
		arm := e.Arms[i]

		// HELPER CALL: Abstract away the messy pattern matching logic!
		condition, armStmts := l.lowerMatchArmPattern(arm, subjectIdent, subjectType)

		consequenceBlock := &BlockStmt{
			Pos_:       arm.Body.Pos(),
			End_:       arm.Body.End(),
			Statements: armStmts,
		}

		finalCascade = l.chainIfCascade(arm, condition, consequenceBlock, finalCascade, e.Type)
	}

	// 3. Yield the final cascade
	outerBlock.Statements = append(outerBlock.Statements, &YieldStmt{
		Pos_:  e.Pos_,
		Value: finalCascade,
	})

	return outerBlock
}

// lowerMatchArmPattern unwraps a specific MatchArm, generating the Boolean condition
// for the branch and injecting any necessary variable bindings into the consequence body.
func (l *Lowerer) lowerMatchArmPattern(arm tast.MatchArm, subjectIdent *Identifier, subjectType types.Type) (Expr, []Stmt) {
	var bodyStmts []Stmt

	// 1. Lower the base consequence body
	loweredBodyExpr := l.lowerExpr(arm.Body)
	if block, isBlock := loweredBodyExpr.(*BlockStmt); isBlock {
		bodyStmts = append(bodyStmts, block.Statements...)
	} else {
		bodyStmts = append(bodyStmts, &YieldStmt{Pos_: arm.Body.Pos(), Value: loweredBodyExpr})
	}

	var condition Expr

	// 2. Map the specific pattern to an evaluation condition and extract bindings
	switch pat := arm.Pattern.(type) {

	// THE FIX: Handle literal values (e.g. `case 0:`, `case 10:`)
	case *tast.LiteralPattern:
		condition = &InfixExpr{
			Pos_:     pat.Pos(),
			Operator: "==",
			Left:     subjectIdent,
			Right:    l.lowerExpr(pat.Value),
			Type:     types.BoolType{},
		}

	// Unconditional catch-all `case _:`
	case *tast.WildcardPattern:
		condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}

	// Handles `case None:` (SumType variant) OR variable binding fallback `case someVar:`
	case *tast.IdentifierPattern:
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
			// Inject the binding declaration at the TOP of the body block
			bodyStmts = append([]Stmt{decl}, bodyStmts...)
		}

	// Handles complex sum types with payloads: `case Some(val):`
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

		// Generate internal unpacking assignments for bounded tuple parameters
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
						FieldName:   "",
						Type:        identPat.Type,
					}

					decl := &DeclareStmt{
						Pos_:   identPat.Pos(),
						Symbol: bindingSym,
						Value:  readExpr,
					}
					// Inject the payload extraction at the TOP of the body block
					bodyStmts = append([]Stmt{decl}, bodyStmts...)
				}
			}
		}

	default:
		// DEFENSE IN DEPTH: Crash loudly if we encounter an unknown pattern type
		panic(fmt.Sprintf("Lowering Error: Unhandled Match Pattern Type: %T", pat))
	}

	return condition, bodyStmts
}

func (l *Lowerer) chainIfCascade(arm tast.MatchArm, cond Expr, conseq *BlockStmt, next Expr, exprType types.Type) Expr {
	if next == nil {
		// Base case (the last arm in the match)
		if boolLit, ok := cond.(*BoolLiteral); ok && boolLit.Value {
			return conseq // Optimize out `if true` for default cases
		}
		return &IfExpr{
			Pos_:        arm.Pos_,
			Condition:   cond,
			Consequence: conseq,
			Alternative: &BlockStmt{Pos_: arm.End_, Statements: []Stmt{}},
			Type:        exprType,
		}
	}

	// Chain to previous link
	var altBlock *BlockStmt
	if ab, ok := next.(*BlockStmt); ok {
		altBlock = ab
	} else {
		altBlock = &BlockStmt{Pos_: next.Pos(), Statements: []Stmt{&YieldStmt{Pos_: next.Pos(), Value: next}}}
	}

	if boolLit, ok := cond.(*BoolLiteral); ok && boolLit.Value {
		return conseq // Optimize out shadowed branches
	}

	return &IfExpr{
		Pos_:        arm.Pos_,
		Condition:   cond,
		Consequence: conseq,
		Alternative: altBlock,
		Type:        exprType,
	}
}

// hoistMatchSubject evaluates the match target exactly once and binds it to a
// hidden local variable ($match_subject) to prevent side-effect duplication.
func (l *Lowerer) hoistMatchSubject(subject tast.Expr, subjectType types.Type, outerBlock *BlockStmt) *Identifier {
	// Lower the actual subject expression
	subjectLowered := l.lowerExpr(subject)

	// Create a unique internal symbol for tracking the hoisted value
	subjectSym := &types.Symbol{
		Kind:    types.VarSymbol,
		Name:    "$match_subject",
		Type:    subjectType,
		Mutable: false,
	}

	// Create the identifier that the match arms will use to reference the value
	subjectIdent := &Identifier{
		Pos_:   subject.Pos(),
		End_:   subject.End(),
		Value:  "$match_subject",
		Type:   subjectType,
		Symbol: subjectSym,
	}

	// Hoist the evaluation by injecting the declaration at the top of the block:
	// e.g., let $match_subject = e.Subject;
	outerBlock.Statements = append(outerBlock.Statements, &DeclareStmt{
		Pos_:   subject.Pos(),
		Value:  subjectLowered,
		Symbol: subjectSym,
	})

	return subjectIdent
}
