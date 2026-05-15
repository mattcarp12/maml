# Complete Language Implementation Roadmap

---

## High-Level Philosophy

Each milestone produces a **working, testable program** in your language. No milestone ends with half-implemented features. The order is chosen so each milestone's foundations are used by the next — you never throw away work.

---

## Full Milestone Overview

```
M1  → Basic functions, types, arithmetic, control flow (YOU ARE HERE)
M2  → Arrays, slices, strings (real operations)
M3  → Algebraic Data Types + match
M4  → Interfaces + impl blocks
M5  → Closures + first-class functions
M6  → Ownership + mutability semantics (Lock Table)
M7  → Arena allocator + task memory model
M8  → Async/await + event loop (libuv)
M9  → ARC heap + escape analysis
M10 → Channels + message passing
M11 → Worker process pool + IPC + shared memory
M12 → WASM target
M13 → Standard library
M14 → Toolchain (containerize, OTEL, SBOM, deterministic builds)
```

---

## Milestone Detail: M1 through M14

### M1 — Core Language (Current State + Gaps)
Already partially done. Complete before moving on.
- Basic functions, arithmetic, if expressions, structs, field access
- Fix the bugs from the previous reviews
- A working program: a small math utility (fibonacci, factorial)

### M2 — Arrays, Slices, Strings
- Fixed arrays (value type, stack allocated)
- Slices (reference type, heap allocated for now)
- String operations (concatenation, length, indexing)
- `for` loops (index-based and iterator-based)
- Working program: string processing utility, e.g. word count

### M3 — Algebraic Data Types + Match
- `enum` with associated data (sum types)
- `Option<T>` and `Result<T, E>` as built-in ADTs
- `match` expression with exhaustiveness checking
- Pattern destructuring in match arms
- Working program: a small expression evaluator using ADTs

### M4 — Interfaces + Impl Blocks
- `impl` blocks attaching methods to types
- `self` and `mut self`
- Structural interfaces (Go-style, no explicit `implements`)
- Interface dispatch (vtable)
- Working program: a polymorphic data pipeline using interfaces

### M5 — Closures + First-Class Functions
- Lambda syntax
- Capturing environment (by borrow for now, `own` later)
- Functions as values, higher-order functions
- `map`, `filter`, `reduce` in stdlib
- Working program: functional data transformation pipeline

### M6 — Ownership + Mutability Semantics
- `mut` enforcement in checker
- Lock Table: aliasing XOR mutability
- Call-site `mut` signaling
- Move semantics: invalidate after move
- No-returning-borrows rule
- Working program: a stateful server request handler (no async yet)

### M7 — Arena Allocator + Task Memory
- Zig runtime: arena allocator implementation
- CGo bridge from compiled programs to Zig runtime
- Arena-backed allocation for local variables
- `Arena.new()` explicit arenas in stdlib
- Working program: a graph data structure using explicit arenas

### M8 — Async/Await + Event Loop
- LLVM coroutine lowering for `async fn`
- State machine code generation
- libuv integration in Zig runtime
- `await` expression in codegen
- Task spawning
- Working program: an async TCP echo server

### M9 — ARC Heap + Escape Analysis
- ARC ref-counted heap allocation in Zig runtime
- Conservative escape analysis in compiler
- Automatic promotion of escaping values to ARC heap
- `own` keyword for explicit promotion
- ARC retain/release injection in codegen
- Working program: data shared across async tasks

### M10 — Channels + Message Passing
- Channel type in type system
- Send/receive operations
- Move semantics on send for mutable values
- ARC increment on send for immutable reference types
- Copy on send for value types
- Working program: a producer/consumer pipeline

### M11 — Worker Process Pool + IPC
- Process pool manager in Zig runtime
- `SharedMemory<T>` type
- wasmtime embedding for WASM worker tasks
- IPC primitives (mmap-based shared memory)
- Working program: CPU-bound work offloaded to worker pool

### M12 — WASM Target
- WASM/WASI codegen backend
- WASI-compatible stdlib subset
- `maml build --target wasm32-wasi`
- Working program: a WASM plugin loaded by the worker pool

### M13 — Standard Library
- Written in your language itself
- Collections: `Map<K,V>`, `Set<T>`, `Vec<T>`
- I/O: file, network (built on libuv)
- `Option`, `Result` combinators
- String utilities
- Working program: a real HTTP server

### M14 — Toolchain
- `maml build --containerize` (distroless image + K8s manifests)
- OpenTelemetry instrumentation (auto-injected spans)
- SBOM generation
- Deterministic builds (content-addressed artifacts)
- Working program: a production-deployable service

---

## M1 Detailed Breakdown

This is where you are now. Here is every remaining gap and exactly where to fix it.

---

### 1. Parser

**Implement `for` loop (needed for M2, build the parser now)**
- Index-based: `for i := 0; i < n; i++ { ... }`
- Iterator-based: `for x in collection { ... }`
- AST nodes: `ForStmt`, `ForInStmt`

**Implement `mut` in variable declarations**
- Already partially in AST (`DeclareStmt.Mutable`), confirm parser sets it correctly
- `x := 10` → immutable
- `mut x := 10` → mutable

**Implement call-site `mut` signaling**
- `myfunc(mut x)` — parser must mark the argument as a mut-pass
- AST: add `Mutable bool` to `CallArg` or similar

**Implement pipe operator**
- `x |> f` desugars to `f(x)` in the AST during parsing
- This is purely a parse-time transformation, no sema/codegen work needed

**Implement `Result` and `Option` syntax (reserved identifiers)**
- These are just built-in enum types — no special parser work needed beyond ensuring their names are reserved

---

### 2. Semantic Analyzer

**Fix the 6 bugs from the previous review** (all in sema):

*Bug 1* — `analyzer.go`: Remove the `pushScope`/`popScope` pair from `Analyze()`:
```go
func (a *Analyzer) Analyze(program *ast.Program) ([]ast.CompileError, map[ast.Node]Type) {
    a.discoverTypes(program)
    a.resolveTypeBodies(program)
    a.registerFunctions(program)
    a.analyzeFunctionBodies(program)
    return a.errors, a.TypeMap
}
```

*Bug 2+4* — `expression.go`: Analyze branches before extracting yield type:
```go
a.analyzeBlockStmt(e.Consequence)
thenYield := a.extractYieldType(e.Consequence)
if e.Alternative != nil {
    a.analyzeBlockStmt(e.Alternative)
    elseYield = a.extractYieldType(e.Alternative)
}
```

*Bug 3* — `expression.go`: Delete the `fmt.Printf` debug line and remove the `"fmt"` import if unused.

*Bug 5* — `types.go`: Add parameter type comparison to `FunctionType.Equals`:
```go
for i := range t.Params {
    if !t.Params[i].Equals(o.Params[i]) {
        return false
    }
}
```

*Bug 6* — `expression.go`: Return `BoolType{}` for comparison operators in `analyzeInfixExpr`:
```go
switch e.Operator {
case "==", "!=", "<", ">", "<=", ">=":
    return BoolType{}
default:
    return left
}
```

**Implement mutability enforcement** — `statement.go`:
- Track whether a symbol is `mut` (already on `Symbol.Mutable`)
- Add `AssignStmt` handler that errors if `!sym.Mutable`:
```go
func (a *Analyzer) analyzeAssignStmt(s *ast.AssignStmt) bool {
    sym := a.resolve(s.Name)
    if sym == nil {
        a.errorf(s.Pos(), "undefined variable '%s'", s.Name)
        return false
    }
    if !sym.Mutable {
        a.errorf(s.Pos(), "cannot assign to immutable variable '%s'", s.Name)
        return false
    }
    exprType := a.analyzeExpr(s.Value)
    if !exprType.Equals(sym.Type) && !exprType.Equals(UnknownType{}) {
        a.errorf(s.Pos(), "type mismatch in assignment to '%s'", s.Name)
    }
    return false
}
```

**Implement proper CFG-based return checking** — `statement.go`:

Replace the linear `alwaysReturns` bool with branch-aware logic. The key case to fix: if *both* branches of an `if` return, the whole if statement counts as returning:
```go
case *ast.IfStmt:
    thenReturns := a.analyzeBlockStmt(s.Consequence)
    elseReturns := s.Alternative != nil && a.analyzeBlockStmt(s.Alternative)
    return thenReturns && elseReturns
```

**Implement unused variable detection** — `scope.go`:
- Add `Used bool` to `Symbol`
- Set `Used = true` in `analyzeIdentifier`
- In `popScope`, warn for any symbol where `!sym.Used && sym.Kind == VarSymbol`

**Implement no-returning-borrows rule** — `type_resolution.go`:
- When resolving a function return type, reject any reference-type borrow
- For M1 this is simple: if a return type annotation is a reference type, emit a warning/error that borrows cannot be returned. Full enforcement comes in M6.

---

### 3. Code Generator

**Fix the 6 bugs from the previous codegen review**:

*Bug 1* — `expr.go`: Guard against non-identifier function calls:
```go
ident, isIdent := e.Function.(*ast.Identifier)
if !isIdent {
    return CGValue{}, fmt.Errorf("only direct function calls are currently supported")
}
```

*Bug 2* — `expr.go`: Spill non-address struct to stack before GEP in `FieldAccess`:
```go
var objPtr value.Value
if objCG.IsAddress {
    objPtr = objCG.V
} else {
    alloca := c.currentBlock.NewAlloca(c.llvmTypeFor(objCG.Type))
    c.currentBlock.NewStore(objCG.V, alloca)
    objPtr = alloca
}
```

*Bug 3* — `stmt.go`: Deep copy structs/strings in `compileDeclareStmt` instead of aliasing:
```go
llvmType := c.llvmTypeFor(valCG.Type)
alloc := c.currentBlock.NewAlloca(llvmType)
data := c.load(valCG)
c.currentBlock.NewStore(data, alloc)
c.setVar(n.Name, alloc)
```

*Bug 4* — `fn.go`: Emit implicit `ret void` if block has no terminator:
```go
if c.currentBlock.Term == nil {
    c.currentBlock.NewRet(nil)
}
```

*Bug 5* — `expr.go`: Capture array type once in `StringLiteral`:
```go
arrType := types.NewArray(uint64(len(strBytes)), types.I8)
strConst := constant.NewCharArray(strBytes)
globalDef := c.module.NewGlobalDef(c.newLabel("str"), strConst)
charPtr := c.currentBlock.NewGetElementPtr(arrType, globalDef, zero, zero)
```

*Bug 6* — `expr.go`: Handle comparison operators and error on unknown operators:
```go
case "==":
    res = c.currentBlock.NewICmp(enum.IPredEQ, left, right)
case "!=":
    res = c.currentBlock.NewICmp(enum.IPredNE, left, right)
case "<":
    res = c.currentBlock.NewICmp(enum.IPredSLT, left, right)
case ">":
    res = c.currentBlock.NewICmp(enum.IPredSGT, left, right)
default:
    return CGValue{}, fmt.Errorf("unsupported operator: %s", e.Operator)
```

**Implement `AssignStmt` codegen** — `stmt.go`:
```go
case *ast.AssignStmt:
    if err := c.compileAssignStmt(s); err != nil {
        return nil, err
    }
```
This already exists in `stmt.go` but confirm it's wired into `compileBlockStmt`.

**Implement `for` loop codegen** — `stmt.go`:

Index-based for loop compiles to 4 basic blocks:
```
[init] → [cond] → [body] → [post] → back to [cond]
                         ↘ [exit]
```
```go
func (c *Codegen) compileForStmt(n *ast.ForStmt) error {
    fn := c.currentBlock.Parent
    condBlk := fn.NewBlock(c.newLabel("for.cond"))
    bodyBlk := fn.NewBlock(c.newLabel("for.body"))
    postBlk := fn.NewBlock(c.newLabel("for.post"))
    exitBlk := fn.NewBlock(c.newLabel("for.exit"))

    // init
    if err := c.compileDeclareStmt(n.Init); err != nil {
        return err
    }
    c.currentBlock.NewBr(condBlk)

    // cond
    c.currentBlock = condBlk
    condCG, err := c.evaluateExpression(n.Condition)
    if err != nil {
        return err
    }
    c.currentBlock.NewCondBr(c.load(condCG), bodyBlk, exitBlk)

    // body
    c.currentBlock = bodyBlk
    if _, err := c.compileBlockStmt(n.Body); err != nil {
        return err
    }
    c.currentBlock.NewBr(postBlk)

    // post
    c.currentBlock = postBlk
    if err := c.compileAssignStmt(n.Post); err != nil {
        return err
    }
    c.currentBlock.NewBr(condBlk)

    c.currentBlock = exitBlk
    return nil
}
```

**Reset label counter per function** — `fn.go`:
```go
func (c *Codegen) compileFnDecl(n *ast.FnDecl) error {
    c.labelCounter = 0  // add this line
    // ... rest of function
}
```

---

### 4. CLI / Driver

**Implement a minimal `maml` CLI** — new file `cmd/maml/main.go`:

```
maml build <file.maml>     → compiles to native binary
maml run <file.maml>       → compiles and runs immediately
maml check <file.maml>     → runs sema only, prints errors
```

The build pipeline in order:
1. Read source file
2. Lex + parse → AST
3. Sema → errors + TypeMap
4. If errors, print and exit
5. Codegen → LLVM IR text (`.ll` file)
6. Shell out to `clang` to compile `.ll` → binary
7. Optionally run the binary

```go
func build(srcPath string) error {
    src, _ := os.ReadFile(srcPath)
    tokens := lexer.Tokenize(string(src))
    program, parseErrs := parser.Parse(tokens)
    if len(parseErrs) > 0 {
        printErrors(parseErrs); return fmt.Errorf("parse failed")
    }
    analyzer := sema.New()
    semaErrs, typeMap := analyzer.Analyze(program)
    if len(semaErrs) > 0 {
        printErrors(semaErrs); return fmt.Errorf("type check failed")
    }
    cg := codegen.New()
    if err := cg.Generate(program, typeMap); err != nil {
        return err
    }
    if err := cg.Validate(); err != nil {
        return err
    }
    llFile := strings.TrimSuffix(srcPath, ".maml") + ".ll"
    os.WriteFile(llFile, []byte(cg.String()), 0644)
    outFile := strings.TrimSuffix(srcPath, ".maml")
    return exec.Command("clang", llFile, "-o", outFile).Run()
}
```

---

### 5. Test Suite

**Set up an integration test harness** — `tests/` directory:

Each test is a `.maml` source file paired with an expected stdout file:
```
tests/
  fibonacci/
    main.maml
    expected.txt
  structs/
    main.maml
    expected.txt
  if_expr/
    main.maml
    expected.txt
```

Test runner in Go (`tests/run_tests.go`):
1. For each test directory, run `maml build` + execute binary
2. Compare stdout to `expected.txt`
3. Print pass/fail

This is the most important infrastructure investment in M1. Every future milestone adds test cases here. Regressions are caught immediately.

---

### M1 Exit Criteria

You are done with M1 when all of the following work end-to-end:

```
// test 3: mutability enforcement
fn main() int {
    let x := 10
    x = 20    // should be a compile error: "cannot assign to immutable variable"
    return x
}
```

```
// test 4: both if branches return (CFG check)
fn abs(n: int) int {
    if n < 0 {
        return -n
    } else {
        return n
    }
    // should NOT warn "missing return" — both branches return
}
```

```
// test 5: string output
fn greet(name: string) int {
    puts("hello")
    return 0
}

fn main() int {
    return greet("world")
}
```

TODO:
- E2E tests support checking standard output
- E2E tests support checking compiler errors
- Refactor E2E test rig to remove code duplication