
# ============================================================

# MAML LANGUAGE GRAMMAR (UNIFIED V3)

# ============================================================

---

## 1. LEXICAL

```ebnf
Identifier      = letter { letter | digit | "_" } ;

IntLiteral      = digit { digit } ;
FloatLiteral    = digit { digit } "." digit { digit } ;
StringLiteral   = `"` { any_char_except_quote_or_escape } `"` ;
BoolLiteral     = "true" | "false" ;

Keywords =
    "package" | "import"
  | "type" | "interface" | "impl" | "self"
  | "fn" | "return"
  | "mut" | "own"
  | "async" | "await"
  | "if" | "else"
  | "match"
  | "for" 
  | "break" | "continue"
  | "static"
  | "with"
  | "using" ;

Operators / Punctuation =
    ":=" | "=" | "=>" | "|>"
  | "+" | "-" | "*" | "/" | "%"
  | "==" | "!=" | "<" | "<=" | ">" | ">="
  | "&&" | "||" | "!"
  | "." | "," | ";" | ":"
  | "(" | ")" | "{" | "}" | "[" | "]" ;
```

---

## 2. PROGRAM STRUCTURE

```ebnf
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

GlobalConstDecl = Identifier ":=" (Expr | StaticLoad) ";" ;

StaticLoad      = "static" "(" StringLiteral ")" ;
```

---

## 3. TYPES

```ebnf
TypeDecl        = "type" Identifier [ TypeParams ] "=" TypeRhs ";" ;

TypeParams      = "[" Identifier { "," Identifier } "]" ;

TypeRhs         = ProductType | SumType | AliasType ;

AliasType       = TypeExpr ;

ProductType     = "{" FieldDecl { "," FieldDecl } "}" ;

FieldDecl       = Identifier TypeExpr ;

SumType         = Variant { "|" Variant } ;

Variant         = Identifier
                | Identifier "(" [ VariantArgs ] ")" ;

VariantArgs     = TypeExpr { "," TypeExpr } ;
```

---

## 4. INTERFACES (STRUCTURAL)

```ebnf
InterfaceDecl   = "interface" Identifier
                  "{" { MethodSig ";" } "}" ;

MethodSig       = Identifier "(" [ ParamList ] ")"
                  [ "using" "(" ParamList ")" ]
                  "=>" TypeExpr ;
```

---

## 5. IMPLEMENTATION BLOCKS

```ebnf
ImplDecl        = "impl" Identifier [ TypeParams ]
                  "{" { MethodDecl } "}" ;

MethodDecl      = "fn" Identifier
                  "(" [ Receiver ] [ "," ParamList ] ")"
                  [ "using" "(" ParamList ")" ]
                  [ "async" ]
                  "=>" TypeExpr Block ;

Receiver        = [ "mut" ] "self" ;
```

---

## 6. FUNCTIONS

```ebnf
FuncDecl        = "fn" Identifier [ TypeParams ]
                  "(" [ ParamList ] ")"
                  [ "using" "(" ParamList ")" ]
                  [ "async" ]
                  "=>" TypeExpr Block ;

ParamList       = Param { "," Param } ;

Param           = [ "mut" | "own" ] Identifier TypeExpr ;
```

### Parameter Semantics (important)

* `T` → borrowed
* `mut T` → mutable borrow
* `own T` → ownership transfer (move)

---

## 7. BLOCKS & STATEMENTS

```ebnf
Block           = "{" { Stmt } "}" ;

Stmt            = VarDecl
                | ConstDecl
                | AssignStmt
                | ReturnStmt
                | IfStmt
                | MatchStmt
                | ForStmt
                | BreakStmt
                | ContinueStmt
                | ExprStmt
                | Block ;

ConstDecl       = Identifier ":=" Expr ";" ;

VarDecl         = "mut" Identifier ":=" Expr ";" ;

AssignStmt      = LValue "=" Expr ";" ;

LValue          = Identifier { "." Identifier | "[" Expr "]" } ;

ReturnStmt      = "return" [ Expr ] ";" ;
```

---

## 8. CONTROL FLOW

```ebnf
IfStmt          = "if" Expr Block [ "else" (Block | IfStmt) ] ;

ForStmt         = "for" [ SimpleStmt ] ";" [ Expr ] ";" [ SimpleStmt ] Block ;

SimpleStmt      = VarDecl | AssignStmt | ExprStmt ;

MatchStmt       = "match" Expr "{" { MatchArm } "}" ;

MatchArm        = Pattern "=>" (Expr | Block) ;
```

---

## 9. PATTERNS

```ebnf
Pattern         = "_"
                | Identifier
                | Literal
                | VariantPattern ;

VariantPattern  = Identifier [ "(" [ PatternList ] ")" ] ;

PatternList     = Pattern { "," Pattern } ;
```

---

## 10. EXPRESSIONS

```ebnf
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
```

---

## 11. PRIMARY EXPRESSIONS

```ebnf
PrimaryExpr     = Literal
                | Identifier
                | CallExpr
                | FieldAccess
                | IfExpr
                | MatchExpr
                | WithExpr
                | ParenExpr ;

ParenExpr       = "(" Expr ")" ;

CallExpr        = PrimaryExpr "(" [ ArgList ] ")"
                  [ "using" "(" ArgList ")" ] ;

ArgList         = Expr { "," Expr } ;

FieldAccess     = PrimaryExpr "." Identifier ;
```

---

## 12. SPECIAL EXPRESSIONS

### If expression

```ebnf
IfExpr          = "if" Expr Block "else" Block ;
```

### Match expression

```ebnf
MatchExpr       = "match" Expr "{" { MatchArm } "}" ;
```

### Functional update (CoW-optimized)

```ebnf
WithExpr        = PrimaryExpr "with"
                  "{" FieldUpdate { "," FieldUpdate } "}" ;

FieldUpdate     = Identifier "=" Expr ;
```

---

## 13. LITERALS

```ebnf
Literal         = IntLiteral
                | FloatLiteral
                | StringLiteral
                | BoolLiteral ;
```

---

# ============================================================

# SEMANTIC / DESIGN MODEL (UNIFIED)

# ============================================================


---

## 3. MUTABILITY RULES

* Immutable by default
* Mutation requires `mut`
* Immutable values are freely shared (ARC-safe)


---

## 4. ASYNC MODEL

* Single-threaded event loop
* `async/await` compiles to state machines
* async tasks have **local arenas**
* values crossing `await` may be promoted to heap (ARC)

---

## 5. ARENAS

* Each async task has a stack-like arena
* Arena values cannot escape task unless promoted via `own`
* Efficient allocation for short-lived computation

---

## 6. FUNCTIONS & CLOSURES

* Closures capture by borrow unless `own`
* Captured values follow same ownership rules
* No implicit heap escaping without `own`

---

## 7. INTERFACES

* Structural (Go-like)
* No inheritance
* Used for polymorphism and DI

---

## 8. IMPLEMENTATION BLOCKS

* `impl` attaches methods to types
* methods may use:

  * `self`
  * `mut self`
* `self` is borrowed unless explicitly consumed via `own` (if used)

---

## 9. CONCURRENCY MODEL (OPTIONAL STD LIB)

* Channels / actors are runtime library features
* Messages must be:

  * immutable OR
  * explicitly moved (`own`)
* No shared mutable state across tasks

---

## 10. TOOLCHAIN FEATURES

* `maml build --containerize`

  * produces distroless container
  * generates Kubernetes manifests

* Built-in:

  * OpenTelemetry instrumentation
  * SBOM generation
  * deterministic builds
  * WASM target support

---

## 11. GLOBAL RULES

* No global mutable state
* No pointer syntax exposed
* No nulls (use ADTs)
* Exhaustive match recommended for ADTs
* Failures expressed via `Result` / `Option`

---

# END OF UNIFIED GRAMMAR

# ============================================================

1. Core Syntax & Defaults
Immutable by Default: Variables are immutable unless explicitly declared with mut (e.g., x := 10 vs mut y := 10).

No Global Mutable State: Enforces pure functions and dependency injection.

Implicit References (No & symbol): * Value Types (primitives, fixed arrays, static structs) are always passed by copy.

Reference Types (dynamic structs, slices, maps) are always implicitly passed by reference.

Call-Site Mutability Signaling: To pass a mutable reference to a function, the programmer must explicitly type mut at the call site (e.g., myfunc(mut x)).

2. The Semantic Rules (Compile-Time Safety)
Aliasing XOR Mutability: The compiler maintains a Lock Table. A reference type can have multiple immutable readers OR exactly one mutable writer, but never both.

Implicit Move Semantics: Assigning a mutable variable to another (mut y := x) or passing it to a channel automatically Moves the Exclusive Write Lock. The compiler invalidates x and throws a compile-time error if the user tries to use x again.

No weak References Needed: By avoiding traditional ARC cycles through Region-Based Memory, the language avoids the need for weak keywords.

3. Memory Management (The Hybrid Engine)
No Garbage Collector: All memory is managed deterministically.

Task-Based Arenas (Region-Based Memory): Every async task gets its own contiguous block of memory. All local variables and complex data structures (including cyclic graphs) are allocated here. When the task ends, the entire Arena is instantly dropped. Zero ARC overhead locally.

The Global ARC Heap: A shared memory space managed by Automatic Reference Counting (ARC) for data that needs to survive across different tasks.

Escape Analysis (The Secret Sauce): The compiler automatically analyzes the AST. If a variable stays local to its task, it goes in the Task Arena. If it escapes (e.g., sent over a channel), the compiler promotes it to the Global ARC Heap and injects the +1/-1 ARC counters.

Explicit Arenas (Standard Library): For advanced data structures (like global graph databases), programmers can manually instantiate Arena.new() to manage cycles without ARC leaks.

4. Concurrency & Closures
Single-Threaded Async/Await: The runtime compiles tasks down to state machines running on a single thread, completely eliminating hardware data races.

Message Passing (Channels): Tasks communicate via channels. Sending a Value Type copies it. Sending an Immutable Reference Type increments its ARC counter. Sending a Mutable Reference Type moves the lock to the receiving task.

Ergonomic Closures: Lambdas implicitly capture their environment. If the lambda stays local, it uses the Arena. If it escapes (e.g., spawned as a new task), Escape Analysis bumps the captured variables to the ARC heap.
