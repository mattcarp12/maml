package hir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// =============================================================================
// The HIR Translator
// =============================================================================

// Translator consumes the fully-typed TAST and emits the desugared HIR.
type Translator struct {
	counter int // Used to generate unique temporary variables during aggregate desugaring
}

func Translate(prog *tast.Program) *Program {
	t := &Translator{counter: 0}
	return t.translateProgram(prog)
}

func (t *Translator) translateProgram(prog *tast.Program) *Program {
	if prog == nil {
		return nil
	}

	var decls []Decl
	for _, d := range prog.Decls {
		if lowered := t.translateDecl(d); lowered != nil {
			decls = append(decls, lowered)
		}
	}

	return &Program{Decls: decls}
}

// =============================================================================
// Declarations & Statements
// =============================================================================

func (t *Translator) translateDecl(decl tast.Decl) Decl {
	if decl == nil {
		return nil
	}

	switch d := decl.(type) {
	case *tast.FnDecl:
		params := make([]*Param, len(d.Params))
		for i, p := range d.Params {
			params[i] = &Param{
				Pos_:   Position{Line: p.Pos_.Line, Col: p.Pos_.Col},
				Name:   p.Name,
				Type:   p.Type,
				Symbol: p.Symbol,
			}
		}

		var body *BlockStmt
		if d.Body != nil {
			body = t.translateStmt(d.Body).(*BlockStmt)
		}

		return &FnDecl{
			Pos_:       Position{Line: d.Pos_.Line, Col: d.Pos_.Col},
			Name:       d.Name,
			Params:     params,
			ReturnType: d.ReturnType,
			Body:       body,
			IsAsync:    d.IsAsync,
			Symbol:     d.Symbol,
		}

	case *tast.TypeDecl:
		return &TypeDecl{
			Pos_:   Position{Line: d.Pos_.Line, Col: d.Pos_.Col},
			Name:   t.translateExpr(d.Name).(*Identifier),
			Type:   d.Type,
			Symbol: d.Symbol,
		}
	}
	return nil
}

func (t *Translator) translateStmt(stmt tast.Stmt) Stmt {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	// --- Direct Mappings ---
	case *tast.BlockStmt:
		stmts := make([]Stmt, len(s.Statements))
		for i, st := range s.Statements {
			stmts[i] = t.translateStmt(st)
		}
		return &BlockStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}, Statements: stmts}

	case *tast.DeclareStmt:
		return &DeclareStmt{
			Pos_:   Position{Line: s.Pos_.Line, Col: s.Pos_.Col},
			Symbol: s.Symbol,
			Value:  t.translateExpr(s.Value),
		}

	case *tast.AssignStmt:
		return &AssignStmt{
			Pos_:   Position{Line: s.Pos_.Line, Col: s.Pos_.Col},
			LValue: t.translateExpr(s.LValue),
			RValue: t.translateExpr(s.RValue),
		}

	case *tast.ReturnStmt:
		return &ReturnStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}, Value: t.translateExpr(s.Value)}

	case *tast.YieldStmt:
		return &YieldStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}, Value: t.translateExpr(s.Value)}

	case *tast.ExprStmt:
		return &ExprStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}, Value: t.translateExpr(s.Value)}

	case *tast.BreakStmt:
		return &BreakStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}}

	case *tast.ContinueStmt:
		return &ContinueStmt{Pos_: Position{Line: s.Pos_.Line, Col: s.Pos_.Col}}

	// --- Syntactic Sugar Diverters ---
	case *tast.ForStmt:
		return t.desugarForStmt(s) // Handled in Step 4
	}

	return nil
}

// =============================================================================
// Expressions
// =============================================================================

func (t *Translator) translateExpr(expr tast.Expr) Expr {
	if expr == nil {
		return nil
	}

	pos := Position{Line: expr.Pos().Line, Col: expr.Pos().Col}

	switch e := expr.(type) {
	// --- Direct Mappings ---
	case *tast.Identifier:
		return &Identifier{Pos_: pos, Value: e.Value, Type: e.Type, Symbol: e.Symbol}
	case *tast.IntLiteral:
		return &IntLiteral{Pos_: pos, Value: e.Value, Type: e.Type}
	case *tast.BoolLiteral:
		return &BoolLiteral{Pos_: pos, Value: e.Value, Type: e.Type}
	case *tast.StringLiteral:
		return &StringLiteral{Pos_: pos, Value: e.Value, Type: e.Type}

	case *tast.PrefixExpr:
		return &PrefixExpr{Pos_: pos, Operator: e.Operator, Right: t.translateExpr(e.Right), Type: e.Type}
	case *tast.InfixExpr:
		return &InfixExpr{Pos_: pos, Left: t.translateExpr(e.Left), Operator: e.Operator, Right: t.translateExpr(e.Right), Type: e.Type}

	case *tast.CallExpr:
		args := make([]CallArg, len(e.Arguments))
		for i, arg := range e.Arguments {
			args[i] = CallArg{Pos_: Position{Line: arg.Pos_.Line, Col: arg.Pos_.Col}, Argument: t.translateExpr(arg.Argument), Mut: arg.Mut, Own: arg.Own}
		}
		return &CallExpr{Pos_: pos, Function: t.translateExpr(e.Function), Arguments: args, Type: e.Type}

	case *tast.FieldAccess:
		return &FieldAccess{Pos_: pos, Object: t.translateExpr(e.Object), Field: t.translateExpr(e.Field).(*Identifier), Type: e.Type}
	case *tast.IndexExpr:
		return &IndexExpr{Pos_: pos, Left: t.translateExpr(e.Left), Index: t.translateExpr(e.Index), Type: e.Type}
	case *tast.SliceExpr:
		return &SliceExpr{Pos_: pos, Left: t.translateExpr(e.Left), Low: t.translateExpr(e.Low), High: t.translateExpr(e.High), Type: e.Type}

	case *tast.IfExpr:
		var consequence, alternative *BlockStmt
		if e.Consequence != nil {
			consequence = t.translateStmt(e.Consequence).(*BlockStmt)
		}
		if e.Alternative != nil {
			alternative = t.translateStmt(e.Alternative).(*BlockStmt)
		}
		return &IfExpr{Pos_: pos, Condition: t.translateExpr(e.Condition), Consequence: consequence, Alternative: alternative, Type: e.Type}

	case *tast.AwaitExpr:
		return &AwaitExpr{Pos_: pos, Value: t.translateExpr(e.Value), Type: e.Type}

	// --- Syntactic Sugar Diverters ---
	case *tast.MatchExpr:
		return t.desugarMatchExpr(e)

	case *tast.ArrayLiteral, *tast.StructLiteral:
		return t.desugarAggregate(e)
	}

	return nil
}

func (t *Translator) desugarForStmt(s *tast.ForStmt) Stmt {
	var outerStmts []Stmt
	pos := Position{Line: s.Pos_.Line, Col: s.Pos_.Col}

	// 1. Desugar the Initialization (e.g., mut i := 0)
	if s.Init != nil {
		outerStmts = append(outerStmts, t.translateStmt(s.Init))
	}

	// 2. Prepare the Infinite Loop Body
	loopBody := &BlockStmt{
		Pos_:       pos,
		Statements: []Stmt{},
	}

	// 3. Synthesize the Conditional Break: if (!Condition) { break }
	if s.Condition != nil {
		cond := t.translateExpr(s.Condition)

		// Create the logical NOT operator
		notCond := &PrefixExpr{
			Pos_:     pos,
			Operator: "!",
			Right:    cond,
			Type:     types.BoolType{}, // Condition inverted
		}

		breakBlock := &BlockStmt{
			Pos_:       pos,
			Statements: []Stmt{&BreakStmt{Pos_: pos}},
		}

		// Wrap it in a synthesized IF expression
		ifCheck := &ExprStmt{
			Pos_: pos,
			Value: &IfExpr{
				Pos_:        pos,
				Condition:   notCond,
				Consequence: breakBlock,
				Alternative: nil,
				Type:        types.UnitType{},
			},
		}
		loopBody.Statements = append(loopBody.Statements, ifCheck)
	}

	// 4. Append the actual Loop Body
	if s.Body != nil {
		translatedBody, ok := t.translateStmt(s.Body).(*BlockStmt)
		if ok {
			loopBody.Statements = append(loopBody.Statements, translatedBody.Statements...)
		}
	}

	// 5. Append the Post-Statement (e.g., i = i + 1)
	if s.Post != nil {
		loopBody.Statements = append(loopBody.Statements, t.translateStmt(s.Post))
	}

	// 6. Wrap everything into the final HIR structure
	loopStmt := &LoopStmt{
		Pos_: pos,
		Body: loopBody,
	}
	outerStmts = append(outerStmts, loopStmt)

	return &BlockStmt{
		Pos_:       pos,
		Statements: outerStmts,
	}
}

func (t *Translator) desugarMatchExpr(e *tast.MatchExpr) Expr {
	var currentIf *IfExpr

	// Iterate backwards through the match arms to build the nested if/else chain
	for i := len(e.Arms) - 1; i >= 0; i-- {
		arm := e.Arms[i]
		pos := Position{Line: arm.Pos_.Line, Col: arm.Pos_.Col}

		var condition Expr
		var consequenceStmts []Stmt

		// 1. Build the condition and destructuring logic based on the pattern
		switch pat := arm.Pattern.(type) {
		case *tast.WildcardPattern:
			// A wildcard is always true
			condition = &BoolLiteral{Pos_: pos, Value: true, Type: types.BoolType{}}

		case *tast.LiteralPattern:
			// Primitive equality check: if (subject == literal)
			condition = &InfixExpr{
				Pos_:     pos,
				Left:     t.translateExpr(e.Subject),
				Operator: "==",
				Right:    t.translateExpr(pat.Value),
				Type:     types.BoolType{},
			}

		case *tast.VariantPattern:
			// Extract the discriminant integer mathematically bound during Semantic Analysis
			discriminantValue := int64(pat.Symbol.Variant.Discriminant)

			// Create a magical field access for the backend to read the SumType's tag
			discrimAccess := &FieldAccess{
				Pos_:   pos,
				Object: t.translateExpr(e.Subject),
				Field:  &Identifier{Pos_: pos, Value: "__discriminant", Type: types.IntType{}},
				Type:   types.IntType{},
			}

			// if (subject.__discriminant == VariantDiscriminant)
			condition = &InfixExpr{
				Pos_:     pos,
				Left:     discrimAccess,
				Operator: "==",
				Right:    &IntLiteral{Pos_: pos, Value: discriminantValue, Type: types.IntType{}},
				Type:     types.BoolType{},
			}

			// Inject variable declarations for destructured payload fields
			for _, f := range pat.Fields {
				consequenceStmts = append(consequenceStmts, &DeclareStmt{
					Pos_:   pos,
					Symbol: f.Binding.Symbol,
					Value: &FieldAccess{
						Pos_:   pos,
						Object: t.translateExpr(e.Subject),
						Field:  &Identifier{Pos_: pos, Value: f.Field, Type: f.Binding.Type},
						Type:   f.Binding.Type,
					},
				})
			}
		}

		// 2. Build the Consequence Block
		// Because MatchExpr returns a value, every block must end with a YieldStmt
		consequenceStmts = append(consequenceStmts, &YieldStmt{
			Pos_:  pos,
			Value: t.translateExpr(arm.Body),
		})

		consequenceBlock := &BlockStmt{
			Pos_:       pos,
			Statements: consequenceStmts,
		}

		// 3. Chain the Alternative
		var alternative *BlockStmt
		if currentIf != nil {
			// Wrap the previously built IfExpr in a block so it can serve as the 'else' path
			alternative = &BlockStmt{
				Pos_: pos,
				Statements: []Stmt{
					&YieldStmt{Pos_: pos, Value: currentIf},
				},
			}
		}

		// 4. Construct the current level of the IfExpr chain
		currentIf = &IfExpr{
			Pos_:        pos,
			Condition:   condition,
			Consequence: consequenceBlock,
			Alternative: alternative,
			Type:        e.Type, // The unified return type of the match block
		}
	}

	// Fallback guard (Semantic Analyzer guarantees at least one arm)
	if currentIf == nil {
		return nil
	}

	// Return the outermost IfExpr which now represents the entire desugared Match block
	return currentIf
}

func (t *Translator) desugarAggregate(expr tast.Expr) Expr {
	t.counter++
	tmpName := fmt.Sprintf("__tmp_%d", t.counter)
	pos := Position{Line: expr.Pos().Line, Col: expr.Pos().Col}

	var stmts []Stmt
	var aggType types.Type
	var zeroAlloc *ZeroAllocExpr

	// 1. Declare the temporary variable's symbol
	// It is mathematically guaranteed to be mutable so we can populate its fields.
	sym := &types.Symbol{
		Name:    tmpName,
		Mutable: true,
		Kind:    types.VarSymbol,
	}

	switch e := expr.(type) {
	// --- Desugar Array Literals ---
	case *tast.ArrayLiteral:
		aggType = e.Type
		sym.Type = aggType
		zeroAlloc = &ZeroAllocExpr{Pos_: pos, Type: aggType}

		stmts = append(stmts, &DeclareStmt{
			Pos_:   pos,
			Symbol: sym,
			Value:  zeroAlloc,
		})

		// Safely extract the base type for the array elements
		var baseType types.Type
		if arrTy, ok := aggType.(types.ArrayType); ok {
			baseType = arrTy.Base
		} else if arrTyPtr, ok := aggType.(*types.ArrayType); ok {
			baseType = arrTyPtr.Base
		} else {
			baseType = types.UnknownType{}
		}

		// Emit sequential assignments for each element
		for i, el := range e.Elements {
			stmts = append(stmts, &AssignStmt{
				Pos_: pos,
				LValue: &IndexExpr{
					Pos_:  pos,
					Left:  &Identifier{Pos_: pos, Value: tmpName, Type: aggType, Symbol: sym},
					Index: &IntLiteral{Pos_: pos, Value: int64(i), Type: types.IntType{}},
					Type:  baseType,
				},
				RValue: t.translateExpr(el),
			})
		}

	// --- Desugar Struct Literals ---
	case *tast.StructLiteral:
		aggType = e.Type
		sym.Type = aggType
		zeroAlloc = &ZeroAllocExpr{Pos_: pos, Type: aggType}

		stmts = append(stmts, &DeclareStmt{
			Pos_:   pos,
			Symbol: sym,
			Value:  zeroAlloc,
		})

		// Emit sequential assignments for each struct field
		for _, f := range e.Fields {
			// FIX: Safely translate the Key Expr into an Identifier
			var keyIdent *Identifier
			var keyType types.Type = types.UnknownType{}

			if f.Key != nil {
				if ident, ok := t.translateExpr(f.Key).(*Identifier); ok {
					keyIdent = ident
					keyType = ident.Type
				}
			}

			stmts = append(stmts, &AssignStmt{
				Pos_: pos,
				LValue: &FieldAccess{
					Pos_:   pos,
					Object: &Identifier{Pos_: pos, Value: tmpName, Type: aggType, Symbol: sym},
					Field:  keyIdent,
					Type:   keyType,
				},
				RValue: t.translateExpr(f.Value),
			})
		}

	}

	// 2. Yield the populated temporary variable
	stmts = append(stmts, &YieldStmt{
		Pos_:  pos,
		Value: &Identifier{Pos_: pos, Value: tmpName, Type: aggType, Symbol: sym},
	})

	// 3. Wrap the initialization block in an Always-True IfExpr
	// This mathematically forces the HIR to treat the block as a value-producing expression.
	return &IfExpr{
		Pos_: pos,
		Condition: &BoolLiteral{
			Pos_:  pos,
			Value: true,
			Type:  types.BoolType{},
		},
		Consequence: &BlockStmt{
			Pos_:       pos,
			Statements: stmts,
		},
		Type: aggType,
	}
}
