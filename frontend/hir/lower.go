package hir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

type Lowerer struct{}

func NewLowerer() *Lowerer { return &Lowerer{} }

// =============================================================================
// Strongly-Typed Entry Points & Wrappers
// =============================================================================

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

// These wrappers use the generated generic dispatcher to cast the returned Node
// to the specific HIR interface required by the structs.
func (l *Lowerer) lowerDecl(decl tast.Decl) Decl {
	if res := tast.MapNode(decl, l); res != nil {
		return res.(Decl)
	}
	return nil
}

func (l *Lowerer) lowerStmt(stmt tast.Stmt) Stmt {
	if res := tast.MapNode(stmt, l); res != nil {
		return res.(Stmt)
	}
	return nil
}

func (l *Lowerer) lowerExpr(expr tast.Expr) Expr {
	if res := tast.MapNode(expr, l); res != nil {
		return res.(Expr)
	}
	return nil
}

func (l *Lowerer) lowerBlockStmt(s *tast.BlockStmt) *BlockStmt {
	// Catch the typed nil before it hits the generic dispatcher!
	if s == nil {
		return nil
	}
	if res := tast.MapNode(s, l); res != nil {
		return res.(*BlockStmt)
	}
	return nil
}

func (l *Lowerer) lowerIdentifier(ident *tast.Identifier) *Identifier {
	if ident == nil {
		return nil
	}
	if res := tast.MapNode(ident, l); res != nil {
		return res.(*Identifier)
	}
	return nil
}

// =============================================================================
// Manual Mappers (Implementing tast.Mapper[Node])
// =============================================================================

func (l *Lowerer) MapFnDecl(fn *tast.FnDecl) Node {
	hirParams := make([]*Param, len(fn.Params))
	for i, p := range fn.Params {
		hirParams[i] = &Param{
			Pos_: p.Pos_, End_: p.End_, Name: p.Name, Type: p.Type, Symbol: p.Symbol,
		}
	}
	hirFn := &FnDecl{
		Pos_:       fn.Pos_,
		End_:       fn.End_,
		Name:       fn.Name,
		Params:     hirParams,
		ReturnType: fn.ReturnType,
		IsAsync:    fn.IsAsync,
		IsExtern:   fn.IsExtern,
		Symbol:     fn.Symbol,
	}
	if fn.Body != nil {
		hirFn.Body = l.lowerBlockStmt(fn.Body)
	}
	return hirFn
}

func (l *Lowerer) MapTypeDecl(td *tast.TypeDecl) Node {
	var hirIdent *Identifier
	if td.Name != nil {
		hirIdent = l.lowerIdentifier(td.Name)
	}
	return &TypeDecl{
		Pos_: td.Pos_, End_: td.End_, Name: hirIdent, Type: td.Type, Symbol: td.Symbol,
	}
}

func (l *Lowerer) MapBlockStmt(block *tast.BlockStmt) Node {
	// Defensive guard just in case!
	if block == nil {
		return nil
	}
	var hirStmts []Stmt
	for _, stmt := range block.Statements {
		if hirStmt := l.lowerStmt(stmt); hirStmt != nil {
			hirStmts = append(hirStmts, hirStmt)
		}
	}
	return &BlockStmt{Pos_: block.Pos_, End_: block.End_, Statements: hirStmts}
}

func (l *Lowerer) MapAssignStmt(s *tast.AssignStmt) Node {
	hirLValue := l.lowerExpr(s.LValue)
	hirRValue := l.lowerExpr(s.RValue)

	if idxExpr, ok := s.LValue.(*tast.IndexExpr); ok {
		switch tast.TypeOf(idxExpr.Left).(type) {
		case *types.MapType:
			return &MapInsertStmt{Pos_: s.Pos_, Map: l.lowerExpr(idxExpr.Left), Key: l.lowerExpr(idxExpr.Index), Value: hirRValue, Operator: s.Operator}
		case *types.VectorType:
			return &VecWriteStmt{Pos_: s.Pos_, Vec: l.lowerExpr(idxExpr.Left), Index: l.lowerExpr(idxExpr.Index), Value: hirRValue, Operator: s.Operator}
		}
	}
	return &AssignStmt{Pos_: s.Pos_, LValue: hirLValue, RValue: hirRValue, Operator: s.Operator}
}

func (l *Lowerer) MapVecPushStmt(s *tast.VecPushStmt) Node {
	return &VecPushStmt{Pos_: s.Pos_, Vec: l.lowerExpr(s.LValue), Value: l.lowerExpr(s.RValue)}
}

func (l *Lowerer) MapExprStmt(s *tast.ExprStmt) Node {
	loweredExpr := l.lowerExpr(s.Value)
	if stmtLike, isStmt := loweredExpr.(Stmt); isStmt {
		return stmtLike
	}
	return &ExprStmt{Pos_: s.Pos_, Value: loweredExpr}
}

func (l *Lowerer) MapForStmt(s *tast.ForStmt) Node {
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
	return &BlockStmt{Pos_: s.Pos_, End_: s.Body.End(), Statements: []Stmt{hirInit, loop}}
}

func (l *Lowerer) MapInfixExpr(e *tast.InfixExpr) Node {
	left := l.lowerExpr(e.Left)
	right := l.lowerExpr(e.Right)

	yieldBool := func(v bool) *BlockStmt {
		return &BlockStmt{Pos_: e.Pos_, Statements: []Stmt{&YieldStmt{Pos_: e.Pos_, Value: &BoolLiteral{Pos_: e.Pos_, Value: v, Type: types.BoolType{}}}}}
	}
	yieldExpr := func(x Expr) *BlockStmt {
		return &BlockStmt{Pos_: e.Pos_, Statements: []Stmt{&YieldStmt{Pos_: e.Pos_, Value: x}}}
	}

	switch e.Operator {
	case "&&":
		return &IfExpr{Pos_: e.Pos_, Condition: left, Consequence: yieldExpr(right), Alternative: yieldBool(false), Type: types.BoolType{}}
	case "||":
		return &IfExpr{Pos_: e.Pos_, Condition: left, Consequence: yieldBool(true), Alternative: yieldExpr(right), Type: types.BoolType{}}
	}
	return &InfixExpr{Pos_: e.Pos_, Operator: e.Operator, Left: left, Right: right, Type: e.Type}
}

func (l *Lowerer) MapCallExpr(e *tast.CallExpr) Node {
	hirArgs := make([]CallArg, len(e.Arguments))
	for i, arg := range e.Arguments {
		hirArgs[i] = lowerTASTCallArg(l, arg)
	}
	return &CallExpr{Pos_: e.Pos_, Function: l.lowerExpr(e.Function), Arguments: hirArgs, Type: e.Type}
}

func (l *Lowerer) MapIndexExpr(idx *tast.IndexExpr) Node {
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

func (l *Lowerer) MapFieldAccess(e *tast.FieldAccess) Node {
	return &FieldAccess{Pos_: e.Pos_, Object: l.lowerExpr(e.Object), Field: l.lowerIdentifier(e.Field), Type: e.Type}
}

func (l *Lowerer) MapStructLiteral(e *tast.StructLiteral) Node {
	fields := make([]StructField, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = StructField{Pos_: f.Pos_, Key: l.lowerIdentifier(f.Key.(*tast.Identifier)), Value: l.lowerExpr(f.Value)}
	}
	return &StructLiteral{Pos_: e.Pos_, End_: e.End_, Fields: fields, Type: e.Type}
}

func (l *Lowerer) MapMapLiteral(e *tast.MapLiteral) Node {
	elements := make([]MapElement, len(e.Elements))
	for i, el := range e.Elements {
		elements[i] = lowerTASTMapElement(l, el)
	}
	return &MapLiteral{Pos_: e.Pos_, End_: e.End_, Elements: elements, Type: e.Type}
}

func (l *Lowerer) MapVariantLiteral(e *tast.VariantLiteral) Node {
	args := make([]Expr, len(e.Arguments))
	for i, arg := range e.Arguments {
		args[i] = l.lowerExpr(arg)
	}
	fields := make([]VariantField, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = lowerTASTVariantField(l, f)
	}
	return &VariantLiteral{Pos_: e.Pos_, End_: e.End_, Variant: e.Variant, Arguments: args, Fields: fields, Type: e.Type}
}

func (l *Lowerer) MapMatchExpr(e *tast.MatchExpr) Node {
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
		consequenceBlock := &BlockStmt{Pos_: arm.Body.Pos(), End_: arm.Body.End(), Statements: armStmts}
		finalCascade = l.chainIfCascade(arm, condition, consequenceBlock, finalCascade, e.Type)
	}

	outerBlock.Statements = append(outerBlock.Statements, &YieldStmt{Pos_: e.Pos_, Value: finalCascade})
	return outerBlock
}

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
		condition = &InfixExpr{Pos_: pat.Pos(), Operator: "==", Left: subjectIdent, Right: l.lowerExpr(pat.Value), Type: types.BoolType{}}

	case *tast.WildcardPattern:
		condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}

	case *tast.IdentifierPattern:
		if sumTy, ok := subjectType.(*types.SumType); ok && sumTy.GetVariant(pat.Name) != nil {
			variant := sumTy.GetVariant(pat.Name)
			condition = &InfixExpr{Pos_: pat.Pos(), Operator: "==", Left: &VariantDiscriminantExpr{Pos_: pat.Pos(), Object: subjectIdent, Type: types.IntType{}}, Right: &IntLiteral{Pos_: pat.Pos(), Value: int64(variant.Discriminant), Type: types.IntType{}}, Type: types.BoolType{}}
		} else {
			condition = &BoolLiteral{Pos_: pat.Pos(), Value: true, Type: types.BoolType{}}
			bindingSym := &types.Symbol{Kind: types.VarSymbol, Name: pat.Name, Type: subjectType, Mutable: false}
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
		condition = &InfixExpr{Pos_: pat.Pos(), Operator: "==", Left: &VariantDiscriminantExpr{Pos_: pat.Pos(), Object: subjectIdent, Type: types.IntType{}}, Right: &IntLiteral{Pos_: pat.Pos(), Value: int64(discriminant), Type: types.IntType{}}, Type: types.BoolType{}}

		for idx, identPat := range pat.TupleBindings {
			if identPat.Value == "_" {
				continue
			}
			sym := identPat.Symbol
			if sym == nil {
				sym = &types.Symbol{Kind: types.VarSymbol, Name: identPat.Value, Type: identPat.Type}
			}
			readExpr := &VariantReadExpr{Pos_: identPat.Pos(), Object: subjectIdent, VariantName: variantName, FieldIndex: idx, Type: identPat.Type}
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
			readExpr := &VariantReadExpr{Pos_: fieldPat.Binding.Pos(), Object: subjectIdent, VariantName: variantName, FieldIndex: payloadIndex, FieldName: fieldPat.Field, Type: fieldPat.Binding.Type}
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
		return conseq
	}
	return &IfExpr{Pos_: arm.Pos_, Condition: cond, Consequence: conseq, Alternative: altBlock, Type: exprType}
}

func (l *Lowerer) hoistMatchSubject(subject tast.Expr, subjectType types.Type, outerBlock *BlockStmt) *Identifier {
	sym := &types.Symbol{Kind: types.VarSymbol, Name: "$match_subject", Type: subjectType, Mutable: false}
	ident := &Identifier{Pos_: subject.Pos(), End_: subject.End(), Value: "$match_subject", Type: subjectType, Symbol: sym}
	outerBlock.Statements = append(outerBlock.Statements, &DeclareStmt{Pos_: subject.Pos(), Value: l.lowerExpr(subject), Symbol: sym})
	return ident
}

// =============================================================================
// Interface Satisfiers for Non-Dispatched Nodes
// =============================================================================
// These nodes are evaluated contextually, not via generic dispatch.
func (l *Lowerer) MapProgram(p *tast.Program) Node                     { return nil }
func (l *Lowerer) MapWildcardPattern(p *tast.WildcardPattern) Node     { return nil }
func (l *Lowerer) MapIdentifierPattern(p *tast.IdentifierPattern) Node { return nil }
func (l *Lowerer) MapLiteralPattern(p *tast.LiteralPattern) Node       { return nil }
func (l *Lowerer) MapCompositePattern(p *tast.CompositePattern) Node   { return nil }
func (l *Lowerer) MapVariantPattern(p *tast.VariantPattern) Node       { return nil }
