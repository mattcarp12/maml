package ast

// Node is the base interface for all AST nodes.
type Node interface {
	TokenLiteral() string
}

// Program represents the entire source file.
type Program struct {
	Declarations []Declaration
}

func (p *Program) TokenLiteral() string {
	if len(p.Declarations) > 0 {
		return p.Declarations[0].TokenLiteral()
	}
	return ""
}

// ================================================
// Declarations (top-level)
// ================================================

type Declaration interface {
	Node
	declNode()
}

// FnDecl represents a function declaration: `fn name(params) retType { ... }`
type FnDecl struct {
	Name    string
	Params  []Param
	RetType string // TODO: replace with proper TypeExpr later
	Body    *Block
	IsAsync bool
}

func (fd *FnDecl) TokenLiteral() string { return "fn" }
func (fd *FnDecl) declNode()            {}

// Param represents a function parameter: `name: type`
type Param struct {
	Name string
	Type string // TODO: replace with proper TypeExpr later
}

// TypeDecl represents a type declaration (for ADTs, structs, etc.)
type TypeDecl struct {
	Name     string
	Variants []Variant
}

func (td *TypeDecl) TokenLiteral() string { return "type" }
func (td *TypeDecl) declNode()            {}

// Variant is used for sum types (e.g., `type Option { Some(T) None }`)
type Variant struct {
	Name   string
	Fields []string
}

// ================================================
// Statements
// ================================================

type Statement interface {
	Node
	stmtNode()
}

// DeclareStmt represents immutable (`:=`) or mutable (`~=`) declarations
type DeclareStmt struct {
	Name    string
	Mutable bool
	Value   Expr
}

func (ds *DeclareStmt) TokenLiteral() string { return ds.Name }
func (ds *DeclareStmt) stmtNode()            {}
func (ds *DeclareStmt) declNode()            {} // can appear at top level

// UpdateStmt represents assignment: `x = value`
type UpdateStmt struct {
	Name  string
	Value Expr
}

func (us *UpdateStmt) TokenLiteral() string { return us.Name }
func (us *UpdateStmt) stmtNode()            {}

// ExprStmt represents an expression used as a statement (e.g. function calls)
type ExprStmt struct {
	Expr Expr
}

func (es *ExprStmt) TokenLiteral() string { return "" } // or es.Expr.TokenLiteral()
func (es *ExprStmt) stmtNode()            {}

// ================================================
// Expressions
// ================================================

type Expr interface {
	Node
	exprNode()
}

// Literals
type IntLiteral struct {
	Value int64
}

func (il *IntLiteral) TokenLiteral() string { return "INT" }
func (il *IntLiteral) exprNode()            {}

type FloatLiteral struct {
	Value float64
}

func (fl *FloatLiteral) TokenLiteral() string { return "FLOAT" }
func (fl *FloatLiteral) exprNode()            {}

type StringLiteral struct {
	Value string
}

func (sl *StringLiteral) TokenLiteral() string { return "STRING" }
func (sl *StringLiteral) exprNode()            {}

type BoolLiteral struct {
	Value bool
}

func (bl *BoolLiteral) TokenLiteral() string { return "BOOL" }
func (bl *BoolLiteral) exprNode()            {}

type NilLiteral struct{}

func (nl *NilLiteral) TokenLiteral() string { return "nil" }
func (nl *NilLiteral) exprNode()            {}

// Identifier
type Identifier struct {
	Name string
}

func (i *Identifier) TokenLiteral() string { return i.Name }
func (i *Identifier) exprNode()            {}

// Operators
type BinaryExpr struct {
	Left  Expr
	Op    string
	Right Expr
}

func (be *BinaryExpr) TokenLiteral() string { return be.Op }
func (be *BinaryExpr) exprNode()            {}

type UnaryExpr struct {
	Op      string
	Operand Expr
}

func (ue *UnaryExpr) TokenLiteral() string { return ue.Op }
func (ue *UnaryExpr) exprNode()            {}

// Function call and method access
type CallExpr struct {
	Function Expr   // can be Identifier or FieldAccess for UFCS (foo.bar())
	Args     []Expr
}

func (ce *CallExpr) TokenLiteral() string { return "" }
func (ce *CallExpr) exprNode()            {}

type FieldAccess struct {
	Object Expr
	Field  string
}

func (fa *FieldAccess) TokenLiteral() string { return "." }
func (fa *FieldAccess) exprNode()            {}

// Control flow expressions
type IfExpr struct {
	Condition Expr
	Then      *Block
	Else      *Block
}

func (ie *IfExpr) TokenLiteral() string { return "if" }
func (ie *IfExpr) exprNode()            {}

type MatchExpr struct {
	Subject Expr
	Cases   []*MatchCase
}

func (me *MatchExpr) TokenLiteral() string { return "match" }
func (me *MatchExpr) exprNode()            {}

type MatchCase struct {
	Pattern Pattern
	Body    *Block
}

type YieldExpr struct { // =>
	Value Expr
}

func (ye *YieldExpr) TokenLiteral() string { return "=>" }
func (ye *YieldExpr) exprNode()            {}

type AwaitExpr struct {
	Value Expr
}

func (ae *AwaitExpr) TokenLiteral() string { return "await" }
func (ae *AwaitExpr) exprNode()            {}

type PipeExpr struct {
	Left  Expr
	Right Expr
}

func (pe *PipeExpr) TokenLiteral() string { return "|>" }
func (pe *PipeExpr) exprNode()            {}

// Block of statements (used in functions, if, match, etc.)
type Block struct {
	Statements []Statement
}

func (b *Block) TokenLiteral() string {
	if len(b.Statements) > 0 {
		return b.Statements[0].TokenLiteral()
	}
	return ""
}

// ================================================
// Patterns (for match expressions)
// ================================================

type Pattern interface {
	Node
	patternNode()
}

type WildcardPattern struct{} // _

func (wp *WildcardPattern) TokenLiteral() string { return "_" }
func (wp *WildcardPattern) patternNode()         {}

type LiteralPattern struct {
	Value Expr
}

func (lp *LiteralPattern) TokenLiteral() string { return "literal" }
func (lp *LiteralPattern) patternNode()         {}

type IdentPattern struct {
	Name string
}

func (ip *IdentPattern) TokenLiteral() string { return ip.Name }
func (ip *IdentPattern) patternNode()         {}

type ConstructorPattern struct {
	Name   string
	Fields []Pattern
}

func (cp *ConstructorPattern) TokenLiteral() string { return cp.Name }
func (cp *ConstructorPattern) patternNode()         {}