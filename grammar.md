# Grammer v1
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

9. Cloud Native Features
   - Automatic OTel insertion
   - One-step "containerize" command to build binary into distroless container
      -- Ballerina: Generates Kubernetes YAML services and deployments directly from network listener definitions.
   - Native SBOM generation
   - WebAssembly (Wasm) Component Model Support
   - Reproducible & Hermetic Builds
   - Remote Build Execution (RBE) Protocol

============================================================
END DOCUMENT
============================================================
```

# Grammar v2 

```ebnf
============================================================
MAML LANGUAGE GRAMMAR (VERSION 2.0 - CLOUD-NATIVE)
============================================================

------------------------------------------------------------
1. LEXICAL
------------------------------------------------------------

Identifier      = letter { letter | digit | "_" } ;
IntLiteral      = digit { digit } ;
FloatLiteral    = digit { digit } "." digit { digit } ;
StringLiteral   = `"` { any_char_except_quote_or_escape | escape_seq } `"` ;
BoolLiteral     = "true" | "false" ;

Keywords =
    "package" | "import" | "static"
  | "type" | "interface" | "impl" | "self"
  | "fn" | "return" | "using"
  | "mut" | "with"
  | "if" | "else" | "match"
  | "async" | "await"
  | "for" | "while" | "break" | "continue" ;

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
                | ImplDecl
                | FuncDecl
                | GlobalConstDecl ;

PackageDecl     = "package" Identifier ";" ;

ImportDecl      = "import" StringLiteral ";"
                | "import" "(" { StringLiteral ";" } ")" ;

GlobalConstDecl = Identifier ":=" ( Expr | StaticLoad ) ";" ;

(* Cloud-native asset embedding *)
StaticLoad      = "static" "(" StringLiteral ")" ;


------------------------------------------------------------
3. TYPES & IMPLEMENTATIONS
------------------------------------------------------------

TypeDecl        = "type" Identifier [ TypeParams ] "=" TypeRhs ";" ;

TypeParams      = "[" Identifier { "," Identifier } "]" ;

TypeRhs         = ProductType | SumType | AliasType ;

AliasType       = TypeExpr ;

ProductType     = "{" FieldDecl { "," FieldDecl } "}" ;

FieldDecl       = Identifier TypeExpr ;

SumType         = Variant { "|" Variant } ;

Variant         = Identifier | Identifier "(" [ VariantArgs ] ")" ;

VariantArgs     = TypeExpr { "," TypeExpr } ;

(* Open Implementation blocks (package-scoped) *)
ImplDecl        = "impl" Identifier [ TypeParams ] "{" { MethodDecl } "}" ;

MethodDecl      = "fn" Identifier "(" [ Receiver ] [ "," ParamList ] ")" 
                  [ "using" "(" ParamList ")" ]
                  [ "async" ] "=>" TypeExpr Block ;

Receiver        = [ "mut" ] "self" ;


------------------------------------------------------------
4. INTERFACES (STRUCTURAL / DI)
------------------------------------------------------------

InterfaceDecl   = "type" Identifier "=" "interface" "{" { MethodSig ";" } "}" ;

MethodSig       = Identifier "(" [ ParamList ] ")" [ "using" "(" ParamList ")" ] 
                  [ "async" ] "=>" TypeExpr ;


------------------------------------------------------------
5. FUNCTIONS
------------------------------------------------------------

FuncDecl        = "fn" Identifier [ TypeParams ] "(" [ ParamList ] ")"
                  [ "using" "(" ParamList ")" ]
                  [ "async" ] "=>" TypeExpr Block ;

ParamList       = Param { "," Param } ;

Param           = [ "mut" ] Identifier TypeExpr ;


------------------------------------------------------------
6. STATEMENTS
------------------------------------------------------------

Block           = "{" { Stmt } "}" ;

Stmt            = VarDecl | ConstDecl | AssignStmt | ReturnStmt 
                | IfStmt | MatchStmt | ForStmt | WhileStmt 
                | BreakStmt | ContinueStmt | ExprStmt | Block ;

ConstDecl       = Identifier ":=" Expr ";" ;

VarDecl         = "mut" Identifier ":=" Expr ";" ;

AssignStmt      = LValue "=" Expr ";" ;

LValue          = Identifier { "." Identifier | "[" Expr "]" } ;

ReturnStmt      = "return" [ Expr ] ";" ;


------------------------------------------------------------
7. EXPRESSIONS
------------------------------------------------------------

Expr            = PipeExpr ;

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
                | WithExpr
                | FieldAccess
                | ParenExpr
                | IfExpr
                | MatchExpr ;

(* Functional Update: Optimized to in-place mutation by compiler if unique *)
WithExpr        = PrimaryExpr "with" "{" FieldUpdate { "," FieldUpdate } "}" ;

FieldUpdate     = Identifier "=" Expr ;

CallExpr        = PrimaryExpr "(" [ ArgList ] ")" [ "using" "(" ArgList ")" ] ;

ArgList         = Expr { "," Expr } ;

MatchExpr       = "match" Expr "{" { MatchArm } "}" ;

MatchArm        = Pattern "=>" ( Expr | Block ) ;


============================================================
SEMANTIC / DESIGN DECISIONS
============================================================

1. Mechanical Transparency (The Go Philosophy)
   - Value Semantics: Structs are stack-allocated values by default.
   - Explicit Mutability: No hidden pointers. 'mut' is the only way to mutate.
   - Single-Threaded: One MAML runtime per core. No locking overhead.

2. ADT Memory Layout (The Rust/Zig Strategy)
   - Unboxed ADTs: Tagged unions are contiguous memory.
   - Exhaustiveness: Match statements are verified at compile-time.
   - Flat Match: Error handling is explicit via Result[T, E] and divergent match arms.

3. Capability-Based Security (The Cloud-Native Innovation)
   - No Ambient Authority: No global 'fs' or 'net'.
   - Capability Objects: I/O requires passing a Capability handle (e.g., NetCap).
   - Contextual Parameters: 'using' clause handles capability plumbing without 
     polluting function signatures manually.
   - maml.toml Manifest: Declarative source of truth for required capabilities.
   - Static Verification: Compiler ensures code does not exceed manifest authority.

4. Async Runtime (The Node/NGINX Model)
   - State Machines: async/await compiles to stackless state machines.
   - Task Density: Millions of idle tasks per GB of RAM.
   - Zero-Copy Workers: CPU work offloaded to OS processes via Shared Memory.

5. Toolchain
   - maml build --containerize: Emits distroless container + Seccomp/RBAC manifests.
   - Built-in OTel: Implicit trace propagation via the async state machine.
   - Static VFS: Embedding assets into the binary to simplify container deployment.

============================================================
END DOCUMENT
============================================================
```