# List of TODOs


## Return Checking 
Your sema logic:

alwaysReturns := a.analyzeBlockStmt(v.Body)
if !alwaysReturns {
    a.errorf(v.Pos(), "function '%s' is missing a return statement", v.Name)
}

But your analyzeBlockStmt is:

alwaysReturns := false
for _, stmt := range body.Statements {
    if a.analyzeStmt(stmt) {
        alwaysReturns = true
    }
}
return alwaysReturns

This means:

if any statement returns, you treat the whole block as “always returns”
even if the return is inside an if branch that doesn’t always execute

So this program will incorrectly pass:

fn f() int {
    if true { return 1 }
    x := 2
}

Your checker is “does there exist a return” not “do all paths return”.

Fix

You need proper control flow analysis:

a block “always returns” if execution cannot fall through the end
statements after a guaranteed return should be marked unreachable

Right now your sema return checking is giving you false confidence.

## Methods - Calling functions defined on objects
- Currently only direct function calls are supported

Looking through your codegen, here are the bugs and improvements I'd suggest:

# Code Generator

## Bugs

**2. `FieldAccess` panics if the object is not already an address (`expr.go`)**

You do `objPtr := objCG.V` and pass it directly to `NewGetElementPtr`, assuming it's always a pointer. But if the object came from e.g. a function return value or a phi node, `IsAddress` will be false and GEP will receive a value instead of a pointer, producing invalid IR or a panic:

```go
objCG, err := c.evaluateExpression(e.Object)
if err != nil {
    return CGValue{}, err
}

// If the object is a value (not already on the stack), spill it first
var objPtr value.Value
if objCG.IsAddress {
    objPtr = objCG.V
} else {
    structLLVMType := c.llvmTypeFor(objCG.Type)
    objPtr = c.currentBlock.NewAlloca(structLLVMType)
    c.currentBlock.NewStore(objCG.V, objPtr)
}
```

**3. `compileDeclareStmt` aliases struct pointers instead of copying them (`stmt.go`)**

When `valCG.IsAddress` is true (structs, strings), you store the raw pointer directly into the variable env:

```go
if valCG.IsAddress {
    c.setVar(n.Name, valCG.V)  // ← alias, not a copy!
    return nil
}
```

This means two variables can point to the same alloca. Mutating one silently mutates the other. You need to copy the data into a fresh alloca:

```go
llvmType := c.llvmTypeFor(valCG.Type)
alloc := c.currentBlock.NewAlloca(llvmType)
data := c.load(valCG) // load the struct value
c.currentBlock.NewStore(data, alloc)
c.setVar(n.Name, alloc)
```

**4. `compileFnDecl` doesn't call `NewRet` for void-returning functions (`fn.go`)**

`compileBlockStmt` is called for the body, but if the function has no explicit `return` statement (or you add void functions later), the entry block will have no terminator. `Validate()` will catch this at runtime, but it's better to emit an implicit `ret void` defensively:

```go
_, err := c.compileBlockStmt(n.Body)
if err != nil {
    return err
}

// Ensure the block is terminated even if no return statement was emitted
if c.currentBlock.Term == nil {
    c.currentBlock.NewRet(nil) // ret void
}
return nil
```

**5. `StringLiteral` creates a new array type on every GEP call (`expr.go`)**

Here you call `types.NewArray(...)` inline inside `NewGetElementPtr`:

```go
charPtr := c.currentBlock.NewGetElementPtr(
    types.NewArray(uint64(len(strBytes)), types.I8),
    globalDef, zero, zero,
)
```

This creates a fresh anonymous type object every call. The type passed to GEP must match the type the global was defined with. You should capture the array type once and reuse it:

```go
arrType := types.NewArray(uint64(len(strBytes)), types.I8)
strConst := constant.NewCharArray(strBytes)
globalDef := c.module.NewGlobalDef(c.newLabel("str"), strConst)
globalDef.Immutable = true

charPtr := c.currentBlock.NewGetElementPtr(arrType, globalDef, zero, zero)
```

**6. `InfixExpr` produces `nil` for unrecognized operators (`expr.go`)**

If `e.Operator` is something unexpected (e.g. a comparison operator that your sema layer now correctly handles after the bug fix in the previous review), `res` stays `nil` and `CGValue{V: nil, ...}` gets returned, which will panic downstream when LLVM tries to use it:

```go
var res value.Value
switch e.Operator {
case "+":
    res = c.currentBlock.NewAdd(left, right)
case "-":
    res = c.currentBlock.NewSub(left, right)
case "*":
    res = c.currentBlock.NewMul(left, right)
case "/":
    res = c.currentBlock.NewSDiv(left, right)
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
}
```

---

## Improvements

**7. `typeCache` uses `t.String()` as the key, which is ambiguous**

Two different struct types with the same name in different scopes would collide. Keying on the string representation of a `*StructType` is fine for now since your language likely enforces unique type names, but keying on the `sema.Type` pointer itself would be more robust:

```go
typeCache map[sema.Type]types.Type
```

**8. `getPutsFunc` is called on every `puts` invocation (`libc.go`)**

It does a linear scan over `c.module.Funcs` every time to check if `puts` is already declared. This is fine for small programs but you could cache it as a field on `Codegen`, or generalize into a `declareExternalFunc` helper that checks the cache once.

**9. `Validate()` only checks for missing terminators (`validate.go`)**

You could also verify that phi nodes only reference predecessor blocks, and that every block referenced by a branch actually exists in the function. These are things LLVM's own verifier would catch if you pipe the IR through `opt -verify`, which would be worth doing in tests.

**10. `newLabel` produces non-unique names across functions**

`labelCounter` is a single global counter on `Codegen`, so the first function gets `then_1`, `else_2` etc. and the second gets `then_5`, `then_6`. These are valid but make the IR harder to read. Resetting the counter at the start of each `compileFnDecl` would make each function's IR self-contained and easier to debug.


# Semantic Analyzer

Great question. Let me break this down into what's standard across real compilers and what your analyzer is currently missing.

## What Your Analyzer Already Does

Your 4-pass structure covers the basics well:
- **Type discovery** — register struct names as shells
- **Type body resolution** — fill in struct fields
- **Function signature registration** — so functions can call each other
- **Function body analysis** — type-check expressions and statements

---

## Standard Passes in Real Compilers

Here's the full picture of what production semantic analyzers do, roughly in order:

### Pass 1 — Name/Symbol Collection
Register all top-level names (types, functions, globals) before analyzing any bodies. You do this. The key insight is doing it in *two sub-passes* (shells first, then bodies) to handle mutual recursion — you already do this for types, but **not for functions**. Two mutually recursive functions will work in your analyzer only because you register all signatures before any bodies, which is correct.

### Pass 2 — Import / Module Resolution
Resolve `import` or `use` statements, bring external symbols into scope, and handle circular import detection. **You don't have this**, but your language may not have modules yet.

### Pass 3 — Type Resolution
Fully resolve all type expressions (named types → concrete types), including generics/templates if applicable. You do this for structs and function signatures.

### Pass 4 — Type Inference
For languages with `let x = 5` (no annotation), infer the type of `x` from its initializer. You do this implicitly in `analyzeDeclareStmt` by propagating the RHS type. Full languages like Go, Haskell, or Rust run a unification-based inference pass (Hindley-Milner or similar) that is substantially more complex.

### Pass 5 — Type Checking
Walk every expression and statement, verify types are compatible, check operator applicability, validate function call arity and argument types. You do this.

### Pass 6 — Control Flow Analysis (CFA)
Build a **Control Flow Graph (CFG)** and answer questions like:
- Does every code path return a value? (you partially do this)
- Is there unreachable code after a `return`?
- Does a `break`/`continue` appear outside a loop?

Real CFG analysis handles things your current linear `alwaysReturns` bool cannot, like:

```
fn foo(x: int) int {
    if x > 0 {
        return 1
    } else {
        return -1
    }
    // your analyzer would wrongly warn here — both branches return
}
```

### Pass 7 — Definite Assignment Analysis
Prove that every variable is assigned before it's used. Related to CFA. Go and Java do this statically and reject programs that might read an uninitialized variable. **You don't have this.**

### Pass 8 — Mutability / Borrow Checking
Enforce `let` vs `var` (or Rust's borrow rules). You track `Mutable` on symbols but **never enforce it** — this was flagged in the previous review.

### Pass 9 — Exhaustiveness Checking
For `match`/`switch` on enum or union types, verify all cases are covered. Relevant if your language has sum types.

### Pass 10 — Dead Code / Unreachable Code Warnings
Warn on statements after `return`, unused variables, unused imports. Go famously makes unused imports and variables *errors*, not warnings.

### Pass 11 — Escape Analysis
Determine whether a value outlives its scope. Needed if your language has stack allocation or ownership semantics. Rust does this explicitly; Go does it implicitly to decide stack vs heap.

---

## What Your Analyzer Is Specifically Missing

Given what your language appears to support today, these are the gaps that actually matter:

**Immutability enforcement** — highest priority since you already track it. Add a check in assignment.

**Proper CFG-based return checking** — your current `alwaysReturns` bool fails on if-expressions where both branches return:

```
fn foo(x: int) int {
    if x > 0 { return 1 } else { return -1 }
    // currently emits "missing return" even though it's fine
}
```

**Definite assignment** — catch use-before-assign. Without this, your codegen will hit undefined variables at runtime rather than giving a clean compile error.

**Unused variable detection** — easy to add: track a `used` flag on `Symbol`, set it in `analyzeIdentifier`, warn at scope pop if any symbol was never read.

**Loop context tracking** — if you add loops, you'll need a pass to reject `break`/`continue` outside of one. A simple `loopDepth int` counter on the analyzer is sufficient.

---

## Priority Order for Your Language

If I were sequencing what to add next:

1. **Mutability enforcement** — already modeled, just not checked
2. **CFG-based return analysis** — fixes real false positives today  
3. **Definite assignment** — prevents codegen crashes on undefined vars
4. **Unused variable warnings** — low effort, high value for usability
5. **Module/import resolution** — when your language needs it
6. **Loop context validation** — when you add loops

The core type-checking you have is solid. The gaps are mostly around control flow and variable lifecycle, which are the next natural layer to build.