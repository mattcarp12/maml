package hir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// Lowerer handles the translation of a type-checked TAST into
// the High-Level Intermediate Representation (HIR).
type Lowerer struct{}

// NewLowerer creates a new HIR lowerer instance.
func NewLowerer() *Lowerer { return &Lowerer{} }

// LowerProgram is the entry point for the HIR phase.
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
	return &Program{Decls: hirDecls}
}

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

func (l *Lowerer) lowerFnDecl(fn *tast.FnDecl) *FnDecl {
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
	if fn.Body != nil {
		hirFn.Body = l.lowerBlockStmt(fn.Body)
	}
	return hirFn
}

func (l *Lowerer) lowerTypeDecl(td *tast.TypeDecl) *TypeDecl {
	var hirIdent *Identifier
	if td.Name != nil {
		hirIdent = l.lowerIdentifier(td.Name)
	}
	return &TypeDecl{
		Pos_:   td.Pos_,
		End_:   td.End_,
		Name:   hirIdent,
		Type:   td.Type,
		Symbol: td.Symbol,
	}
}

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

// lowerStmt is the central dispatcher for statement lowering.
// Cases marked "lower: true" in the schema are handled by generated methods in
// lower_generated.go; only statements requiring special logic live here.
func (l *Lowerer) lowerStmt(stmt tast.Stmt) Stmt {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *tast.BlockStmt:
		return l.lowerBlockStmt(s)
	case *tast.BreakStmt:
		return l.lowerBreakStmt(s)
	case *tast.ContinueStmt:
		return l.lowerContinueStmt(s)
	case *tast.DeclareStmt:
		return l.lowerDeclareStmt(s)
	case *tast.ReturnStmt:
		return l.lowerReturnStmt(s)
	case *tast.YieldStmt:
		return l.lowerYieldStmt(s)

	case *tast.AssignStmt:
		// Index-assignments on maps/vecs become dedicated HIR statement nodes.
		if idxExpr, ok := s.LValue.(*tast.IndexExpr); ok {
			switch tast.TypeOf(idxExpr.Left).(type) {
			case *types.MapType:
				return &MapInsertStmt{
					Pos_:  s.Pos_,
					Map:   l.lowerExpr(idxExpr.Left),
					Key:   l.lowerExpr(idxExpr.Index),
					Value: l.lowerExpr(s.RValue),
				}
			case *types.VectorType:
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
		// TAST uses LValue/RValue; HIR uses Vec/Value.
		return &VecPushStmt{
			Pos_:  s.Pos_,
			Vec:   l.lowerExpr(s.LValue),
			Value: l.lowerExpr(s.RValue),
		}

	case *tast.ExprStmt:
		// If the lowered expression is already statement-shaped, unwrap it.
		loweredExpr := l.lowerExpr(s.Value)
		if stmtLike, isStmt := loweredExpr.(Stmt); isStmt {
			return stmtLike
		}
		return &ExprStmt{Pos_: s.Pos_, Value: loweredExpr}

	case *tast.ForStmt:
		return l.lowerForStmt(s)

	default:
		return nil
	}
}

// lowerForStmt translates TAST ForStmt → HIR LoopStmt, hoisting any Init
// into an enclosing BlockStmt.
func (l *Lowerer) lowerForStmt(s *tast.ForStmt) Stmt {
	var hirInit Stmt
	if s.Init != nil {
		hirInit = l.lowerStmt(s.Init)
	}

	loop := &LoopStmt{
		Pos_:      s.Pos_,
		Condition: l.lowerExpr(s.Condition),
		Post:      l.lowerStmt(s.Post),
		Body:      l.lowerBlockStmt(s.Body),
	}

	if hirInit == nil {
		return loop
	}
	return &BlockStmt{
		Pos_:       s.Pos_,
		End_:       s.Body.End(),
		Statements: []Stmt{hirInit, loop},
	}
}

// lowerExpr is the central dispatcher for expression lowering.
// Cases marked "lower: true" in the schema are handled by generated methods in
// lower_generated.go; only expressions requiring special logic live here.
func (l *Lowerer) lowerExpr(expr tast.Expr) Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *tast.Identifier:
		return l.lowerIdentifier(e)
	case *tast.IntLiteral:
		return l.lowerIntLiteral(e)
	case *tast.BoolLiteral:
		return l.lowerBoolLiteral(e)
	case *tast.StringLiteral:
		return l.lowerStringLiteral(e)
	case *tast.PrefixExpr:
		return l.lowerPrefixExpr(e)
	case *tast.IfExpr:
		return l.lowerIfExpr(e)
	case *tast.SliceExpr:
		return l.lowerSliceExpr(e)
	case *tast.ArrayLiteral:
		return l.lowerArrayLiteral(e)
	case *tast.VecLiteral:
		return l.lowerVecLiteral(e)
	case *tast.AwaitExpr:
		return l.lowerAwaitExpr(e)
	case *tast.BlockStmt:
		return l.lowerBlockStmt(e)
	case *tast.InfixExpr:
		return l.lowerInfixExpr(e)
	case *tast.CallExpr:
		return l.lowerCallExpr(e)
	case *tast.MatchExpr:
		return l.lowerMatchExpr(e)

	case *tast.FieldAccess:
		return &FieldAccess{
			Pos_:   e.Pos_,
			Object: l.lowerExpr(e.Object),
			Field:  l.lowerIdentifier(e.Field),
			Type:   e.Type,
		}

	case *tast.IndexExpr:
		return l.lowerIndexExpr(e)

	case *tast.StructLiteral:
		// TAST StructField.Key is Expr; HIR StructField.Key is *Identifier.
		fields := make([]StructField, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = StructField{
				Pos_:  f.Pos_,
				Key:   l.lowerIdentifier(f.Key.(*tast.Identifier)),
				Value: l.lowerExpr(f.Value),
			}
		}
		return &StructLiteral{Pos_: e.Pos_, End_: e.End_, Fields: fields, Type: e.Type}

	case *tast.MapLiteral:
		elements := make([]MapElement, len(e.Elements))
		for i, el := range e.Elements {
			elements[i] = lowerTASTMapElement(l, el)
		}
		return &MapLiteral{Pos_: e.Pos_, End_: e.End_, Elements: elements, Type: e.Type}

	case *tast.VariantLiteral:
		args := make([]Expr, len(e.Arguments))
		for i, arg := range e.Arguments {
			args[i] = l.lowerExpr(arg)
		}
		fields := make([]VariantField, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = lowerTASTVariantField(l, f)
		}
		return &VariantLiteral{
			Pos_:      e.Pos_,
			End_:      e.End_,
			Variant:   e.Variant,
			Arguments: args,
			Fields:    fields,
			Type:      e.Type,
		}

	default:
		return nil
	}
}

// lowerInfixExpr desugars && and || into IfExpr nodes; all other operators
// pass through as InfixExpr.
func (l *Lowerer) lowerInfixExpr(e *tast.InfixExpr) Expr {
	left := l.lowerExpr(e.Left)
	right := l.lowerExpr(e.Right)

	yieldBool := func(v bool) *BlockStmt {
		return &BlockStmt{
			Pos_: e.Pos_,
			Statements: []Stmt{
				&YieldStmt{Pos_: e.Pos_, Value: &BoolLiteral{Pos_: e.Pos_, Value: v, Type: types.BoolType{}}},
			},
		}
	}
	yieldExpr := func(x Expr) *BlockStmt {
		return &BlockStmt{
			Pos_:       e.Pos_,
			Statements: []Stmt{&YieldStmt{Pos_: e.Pos_, Value: x}},
		}
	}

	switch e.Operator {
	case "&&": // A && B  →  if A { B } else { false }
		return &IfExpr{
			Pos_:        e.Pos_,
			Condition:   left,
			Consequence: yieldExpr(right),
			Alternative: yieldBool(false),
			Type:        types.BoolType{},
		}
	case "||": // A || B  →  if A { true } else { B }
		return &IfExpr{
			Pos_:        e.Pos_,
			Condition:   left,
			Consequence: yieldBool(true),
			Alternative: yieldExpr(right),
			Type:        types.BoolType{},
		}
	}

	return &InfixExpr{
		Pos_:     e.Pos_,
		Operator: e.Operator,
		Left:     left,
		Right:    right,
		Type:     e.Type,
	}
}

func (l *Lowerer) lowerCallExpr(e *tast.CallExpr) *CallExpr {
	hirArgs := make([]CallArg, len(e.Arguments))
	for i, arg := range e.Arguments {
		hirArgs[i] = lowerTASTCallArg(l, arg)
	}
	return &CallExpr{
		Pos_:      e.Pos_,
		Function:  l.lowerExpr(e.Function),
		Arguments: hirArgs,
		Type:      e.Type,
	}
}

// lowerIndexExpr fans out to MapReadExpr, VecReadExpr, or plain IndexExpr
// based on the container's type.
func (l *Lowerer) lowerIndexExpr(idx *tast.IndexExpr) Expr {
	left := l.lowerExpr(idx.Left)
	index := l.lowerExpr(idx.Index)
	switch tast.TypeOf(idx.Left).(type) {
	case *types.MapType:
		return &MapReadExpr{Pos_: idx.Pos_, Map: left, Key: index, Type: idx.Type}
	case *types.VectorType:
		return &VecReadExpr{Pos_: idx.Pos_, Vec: left, Index: index, Type: idx.Type}
	default:
		return &IndexExpr{Pos_: idx.Pos_, Left: left, Index: index, Type: idx.Type}
	}
}

func (l *Lowerer) lowerMatchExpr(e *tast.MatchExpr) Expr {
	if e == nil {
		return nil
	}

	outerBlock := &BlockStmt{Pos_: e.Pos_, End_: e.End_, Statements: []Stmt{}}
	subjectType := tast.TypeOf(e.Subject)
	subjectIdent := l.hoistMatchSubject(e.Subject, subjectType, outerBlock)

	var finalCascade Expr
	for i := len(e.Arms) - 1; i >= 0; i-- {
		arm := e.Arms[i]
		condition, armStmts := l.lowerMatchArmPattern(arm, subjectIdent, subjectType)
		consequenceBlock := &BlockStmt{
			Pos_:       arm.Body.Pos(),
			End_:       arm.Body.End(),
			Statements: armStmts,
		}
		finalCascade = l.chainIfCascade(arm, condition, consequenceBlock, finalCascade, e.Type)
	}

	outerBlock.Statements = append(outerBlock.Statements, &YieldStmt{
		Pos_:  e.Pos_,
		Value: finalCascade,
	})
	return outerBlock
}

// lowerMatchArmPattern generates the boolean condition for a match arm and
// injects any variable bindings into the body statement list.
func (l *Lowerer) lowerMatchArmPattern(arm tast.MatchArm, subjectIdent *Identifier, subjectType types.Type) (Expr, []Stmt) {
	var bodyStmts []Stmt

	loweredBodyExpr := l.lowerExpr(arm.Body)
	if block, isBlock := loweredBodyExpr.(*BlockStmt); isBlock {
		bodyStmts = append(bodyStmts, block.Statements...)
	} else {
		bodyStmts = append(bodyStmts, &YieldStmt{Pos_: arm.Body.Pos(), Value: loweredBodyExpr})
	}

	var condition Expr

	switch pat := arm.Pattern.(type) {

	case *tast.LiteralPattern:
		condition = &InfixExpr{
			Pos_:     pat.Pos(),
			Operator: "==",
			Left:     subjectIdent,
			Right:    l.lowerExpr(pat.Value),
			Type:     types.BoolType{},
		}

	case *tast.WildcardPattern:
		condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}

	case *tast.IdentifierPattern:
		if sumTy, ok := subjectType.(*types.SumType); ok && sumTy.GetVariant(pat.Name) != nil {
			variant := sumTy.GetVariant(pat.Name)
			condition = &InfixExpr{
				Pos_:     pat.Pos(),
				Operator: "==",
				Left:     &VariantDiscriminantExpr{Pos_: pat.Pos(), Object: subjectIdent, Type: types.IntType{}},
				Right:    &IntLiteral{Pos_: pat.Pos(), Value: int64(variant.Discriminant), Type: types.IntType{}},
				Type:     types.BoolType{},
			}
		} else {
			condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}
			bindingSym := &types.Symbol{
				Kind:    types.VarSymbol,
				Name:    pat.Name,
				Type:    subjectType,
				Mutable: false,
			}
			bodyStmts = append([]Stmt{&DeclareStmt{Pos_: pat.Pos(), Value: subjectIdent, Symbol: bindingSym}}, bodyStmts...)
		}

	case *tast.VariantPattern:
		variantName := ""
		if pat.Variant != nil {
			variantName = pat.Variant.Value
		}
		discriminant := 0
		if sumTy, ok := subjectType.(*types.SumType); ok {
			if v := sumTy.GetVariant(variantName); v != nil {
				discriminant = v.Discriminant
			}
		}
		condition = &InfixExpr{
			Pos_:     pat.Pos(),
			Operator: "==",
			Left:     &VariantDiscriminantExpr{Pos_: pat.Pos(), Object: subjectIdent, Type: types.IntType{}},
			Right:    &IntLiteral{Pos_: pat.Pos(), Value: int64(discriminant), Type: types.IntType{}},
			Type:     types.BoolType{},
		}

		for idx, identPat := range pat.TupleBindings {
			if identPat.Value == "_" {
				continue
			}
			sym := identPat.Symbol
			if sym == nil {
				sym = &types.Symbol{Kind: types.VarSymbol, Name: identPat.Value, Type: identPat.Type}
			}
			readExpr := &VariantReadExpr{
				Pos_:        identPat.Pos(),
				Object:      subjectIdent,
				VariantName: variantName,
				FieldIndex:  idx,
				Type:        identPat.Type,
			}
			bodyStmts = append([]Stmt{&DeclareStmt{Pos_: identPat.Pos(), Symbol: sym, Value: readExpr}}, bodyStmts...)
		}

		for _, fieldPat := range pat.Fields {
			payloadIndex := 0
			if sumTy, ok := subjectType.(*types.SumType); ok {
				if v := sumTy.GetVariant(variantName); v != nil {
					for i, f := range v.Fields {
						if f.Name == fieldPat.Field {
							payloadIndex = i
							break
						}
					}
				}
			}
			sym := fieldPat.Binding.Symbol
			if sym == nil {
				sym = &types.Symbol{Kind: types.VarSymbol, Name: fieldPat.Binding.Value, Type: fieldPat.Binding.Type}
			}
			readExpr := &VariantReadExpr{
				Pos_:        fieldPat.Binding.Pos(),
				Object:      subjectIdent,
				VariantName: variantName,
				FieldIndex:  payloadIndex,
				FieldName:   fieldPat.Field,
				Type:        fieldPat.Binding.Type,
			}
			bodyStmts = append([]Stmt{&DeclareStmt{Pos_: fieldPat.Binding.Pos(), Symbol: sym, Value: readExpr}}, bodyStmts...)
		}

	default:
		panic(fmt.Sprintf("Lowering Error: Unhandled Match Pattern Type: %T", pat))
	}

	return condition, bodyStmts
}

func (l *Lowerer) chainIfCascade(arm tast.MatchArm, cond Expr, conseq *BlockStmt, next Expr, exprType types.Type) Expr {
	if next == nil {
		return conseq
	}

	var altBlock *BlockStmt
	if ab, ok := next.(*BlockStmt); ok {
		altBlock = ab
	} else {
		altBlock = &BlockStmt{Pos_: next.Pos(), Statements: []Stmt{&YieldStmt{Pos_: next.Pos(), Value: next}}}
	}

	if boolLit, ok := cond.(*BoolLiteral); ok && boolLit.Value {
		return conseq // optimize out shadowed branches
	}

	return &IfExpr{
		Pos_:        arm.Pos_,
		Condition:   cond,
		Consequence: conseq,
		Alternative: altBlock,
		Type:        exprType,
	}
}

// hoistMatchSubject evaluates the match subject exactly once into a hidden
// local ($match_subject) to prevent side-effect duplication across arms.
func (l *Lowerer) hoistMatchSubject(subject tast.Expr, subjectType types.Type, outerBlock *BlockStmt) *Identifier {
	sym := &types.Symbol{
		Kind:    types.VarSymbol,
		Name:    "$match_subject",
		Type:    subjectType,
		Mutable: false,
	}
	ident := &Identifier{
		Pos_:   subject.Pos(),
		End_:   subject.End(),
		Value:  "$match_subject",
		Type:   subjectType,
		Symbol: sym,
	}
	outerBlock.Statements = append(outerBlock.Statements, &DeclareStmt{
		Pos_:   subject.Pos(),
		Value:  l.lowerExpr(subject),
		Symbol: sym,
	})
	return ident
}
