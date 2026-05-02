```ebnf
============================================================
MAML LANGUAGE GRAMMAR SKETCH 
============================================================

------------------------------------------------------------
1. LEXICAL (INFORMAL)
------------------------------------------------------------

Identifier      = letter { letter | digit | "_" } ;
IntLiteral      = digit { digit } ;
FloatLiteral    = digit { digit } "." digit { digit } ;
StringLiteral   = `"` { any_char_except_quote_or_escape | escape_seq } `"` ;
BoolLiteral     = "true" | "false" ;

Keywords =
    "package" | "import"
  | "type" | "interface"
  | "fn" | "return"
  | "mut"
  | "if" | "else"
  | "match"
  | "async" | "await"
  | "for" | "while"
  | "break" | "continue" ;

Operators / Punctuation =
    ":=" | "=" | "=>" | "|>" | "|"
  | "+" | "-" | "*" | "/" | "%"
  | "==" | "!=" | "<" | "<=" | ">" | ">="
  | "&&" | "||" | "!"
  | "." | "," | ";" | ":" 
  | "(" | ")" | "{" | "}" | "[" | "]" ;


------------------------------------------------------------
2. PROGRAM STRUCTURE
------------------------------------------------------------

Program         = { TopDecl } ;

TopDecl         = PackageDecl
                | ImportDecl
                | TypeDecl
                | InterfaceDecl
                | FuncDecl
                | GlobalConstDecl ;

PackageDecl     = "package" Identifier ";" ;

ImportDecl      = "import" StringLiteral ";"
                | "import" "(" { StringLiteral ";" } ")" ;

(* Global mutable variables are not allowed. *)
GlobalConstDecl = Identifier ":=" Expr ";" ;


------------------------------------------------------------
3. TYPES
------------------------------------------------------------

TypeDecl        = "type" Identifier [ TypeParams ] "=" TypeRhs ";" ;

TypeParams      = "[" Identifier { "," Identifier } "]" ;

TypeRhs         = ProductType
                | SumType
                | AliasType ;

AliasType       = TypeExpr ;

ProductType     = "{" FieldDecl { "," FieldDecl } "}" ;

FieldDecl       = Identifier TypeExpr ;

SumType         = Variant { "|" Variant } ;

Variant         = Identifier
                | Identifier "(" [ VariantArgs ] ")" ;

VariantArgs     = TypeExpr { "," TypeExpr } ;


TypeExpr        = NamedType
                | GenericType
                | FuncType ;

NamedType       = Identifier ;

GenericType     = Identifier "[" TypeExpr { "," TypeExpr } "]" ;

FuncType        = "fn" "(" [ ParamTypes ] ")" [ "async" ] "=>" TypeExpr ;

ParamTypes      = TypeExpr { "," TypeExpr } ;


------------------------------------------------------------
4. INTERFACES (GO-STYLE STRUCTURAL)
------------------------------------------------------------

InterfaceDecl   = "interface" Identifier "{" { MethodSig ";" } "}" ;

MethodSig       = Identifier "(" [ ParamList ] ")" "=>" TypeExpr ;


------------------------------------------------------------
5. FUNCTIONS
------------------------------------------------------------

FuncDecl        = "fn" Identifier [ TypeParams ] "(" [ ParamList ] ")"
                  [ "async" ] "=>" TypeExpr Block ;

ParamList       = Param { "," Param } ;

Param           = Identifier TypeExpr ;


------------------------------------------------------------
6. BLOCKS AND STATEMENTS
------------------------------------------------------------

Block           = "{" { Stmt } "}" ;

Stmt            = VarDecl
                | ConstDecl
                | AssignStmt
                | ReturnStmt
                | IfStmt
                | MatchStmt
                | ForStmt
                | WhileStmt
                | BreakStmt
                | ContinueStmt
                | ExprStmt
                | Block ;

(* Immutable binding declaration *)
ConstDecl       = Identifier ":=" Expr ";" ;

(* Mutable binding declaration *)
VarDecl         = "mut" Identifier ":=" Expr ";" ;

(* Assignment/update only allowed on mutable bindings *)
AssignStmt      = Identifier "=" Expr ";" ;

ReturnStmt      = "return" [ Expr ] ";" ;

BreakStmt       = "break" ";" ;
ContinueStmt    = "continue" ";" ;

ExprStmt        = Expr ";" ;


------------------------------------------------------------
7. CONTROL FLOW
------------------------------------------------------------

(* If can be used as statement or expression (expression form below). *)
IfStmt          = "if" Expr Block [ "else" (Block | IfStmt) ] ;

ForStmt         = "for" [ SimpleStmt ] ";" [ Expr ] ";" [ SimpleStmt ] Block ;

WhileStmt       = "while" Expr Block ;

SimpleStmt      = VarDecl
                | AssignStmt
                | ExprStmt ;

MatchStmt       = MatchExpr ";" ;

MatchExpr       = "match" Expr "{" { MatchArm } "}" ;

MatchArm        = Pattern "=>" ( Expr | Block ) ;


------------------------------------------------------------
8. PATTERNS (MATCH)
------------------------------------------------------------

Pattern         = "_"
                | Identifier
                | Literal
                | VariantPattern ;

VariantPattern  = Identifier [ "(" [ PatternList ] ")" ] ;

PatternList     = Pattern { "," Pattern } ;


------------------------------------------------------------
9. EXPRESSIONS
------------------------------------------------------------

Expr            = PipeExpr ;

(* Pipe operator: x |> f  means f(x) *)
PipeExpr        = LogicOrExpr { "|>" LogicOrExpr } ;

LogicOrExpr     = LogicAndExpr { "||" LogicAndExpr } ;
LogicAndExpr    = EqualityExpr { "&&" EqualityExpr } ;

EqualityExpr    = RelExpr { ("==" | "!=") RelExpr } ;
RelExpr         = AddExpr { ("<" | "<=" | ">" | ">=") AddExpr } ;

AddExpr         = MulExpr { ("+" | "-") MulExpr } ;
MulExpr         = UnaryExpr { ("*" | "/" | "%") UnaryExpr } ;

UnaryExpr       = ("!" | "-") UnaryExpr
                | AwaitExpr
                | PrimaryExpr ;

AwaitExpr       = "await" UnaryExpr ;

PrimaryExpr     = Literal
                | Identifier
                | CallExpr
                | LambdaExpr
                | FieldAccess
                | ParenExpr
                | IfExpr
                | MatchExpr ;

ParenExpr       = "(" Expr ")" ;

CallExpr        = PrimaryExpr "(" [ ArgList ] ")" ;

ArgList         = Expr { "," Expr } ;

FieldAccess     = PrimaryExpr "." Identifier ;

(* If expression form: must have else *)
IfExpr          = "if" Expr Block "else" Block ;

(* Go-style explicit lambda *)
LambdaExpr      = "fn" "(" [ ParamList ] ")" [ "async" ] "=>"
                  ( Expr | Block ) ;


------------------------------------------------------------
10. LITERALS
------------------------------------------------------------

Literal         = IntLiteral
                | FloatLiteral
                | StringLiteral
                | BoolLiteral ;


============================================================
SEMANTIC / DESIGN NOTES (NOT GRAMMAR)
============================================================

1. General Style
   - Go-like syntax.
   - "Working man" explicitness and constraints.
   - No pointer syntax exposed (*, & not part of surface language).
   - No tuples.
   - No destructuring assignment.
   - No multiple return values.

2. Mutability
   - Identifier ":=" Expr  declares an immutable binding.
   - "mut" Identifier ":=" Expr declares a mutable binding.
   - Identifier "=" Expr updates an existing mutable binding.
   - Deep immutability is the default: immutable product values cannot have
     their fields mutated.

3. ADTs
   - Sum types and product types are first-class.
   - match is expression-oriented and yields a value.
   - match arms may contain blocks; last expression in a block is the value.

4. Async / Concurrency
   - Single-threaded event loop model (JS-like).
   - async/await exists.
   - All functions may perform effects (no purity/effect typing).

5. Interfaces
   - Structural interfaces (Go-like).
   - Interface values represented as fat pointers internally (data + vtable).
   - Interfaces allowed as fields for DI-style architecture.

6. Memory Model (implementation)
   - Stack vs heap decided by compiler (escape analysis).
   - Implicit boxing/indirection allowed; no explicit Box type exposed.
   - Tracing GC (likely generational) planned for runtime.
   - LLVM backend to native machine code.

7. Error Handling
   - Result/Option ADTs encouraged.
   - Explicit match required for handling errors (no implicit try/? sugar).

8. Globals
   - No global mutable variables.
   - Global immutable constants may exist.

============================================================
END DOCUMENT
============================================================
```
