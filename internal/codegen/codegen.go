package codegen

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/internal/ast"
	"github.com/mattcarp12/maml/internal/sema"
)

type Env struct {
	parent *Env
	vars   map[string]value.Value
}

type Codegen struct {
	module       *ir.Module
	currentBlock *ir.Block
	currentEnv   *Env
	typeMap      map[ast.Node]sema.Type
}

func New() *Codegen {
	return &Codegen{
		module:     ir.NewModule(),
		currentEnv: &Env{vars: make(map[string]value.Value)},
	}
}

func (c *Codegen) pushEnv() {
	c.currentEnv = &Env{parent: c.currentEnv, vars: make(map[string]value.Value)}
}

func (c *Codegen) popEnv() {
	if c.currentEnv != nil {
		c.currentEnv = c.currentEnv.parent
	}
}

func (c *Codegen) resolveVar(name string) (value.Value, bool) {
	for e := c.currentEnv; e != nil; e = e.parent {
		if val, ok := e.vars[name]; ok {
			return val, true
		}
	}
	return nil, false
}

func (c *Codegen) setVar(name string, val value.Value) {
	c.currentEnv.vars[name] = val
}

// Generate is the entry point. We now accept the typeMap!
func (c *Codegen) Generate(node ast.Node, typeMap map[ast.Node]sema.Type) error {
	// Save the type map on the struct so other codegen functions can use it
	if c.typeMap == nil && typeMap != nil {
		c.typeMap = typeMap
	}

	var err error
	switch n := node.(type) {
	case *ast.Program:
		for _, decl := range n.Decls {
			if err := c.Generate(decl, typeMap); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		err = c.compileFnDecl(n)

	case *ast.BlockStmt:
		// FIX 1: compileBlockStmt now returns (value.Value, error).
		// We use `_` to ignore the value here at the top level.
		_, err = c.compileBlockStmt(n)

	case *ast.DeclareStmt:
		err = c.compileDeclareStmt(n)

	case *ast.ReturnStmt:
		err = c.compileReturnStmt(n)

	case *ast.YieldStmt:
		// FIX 2: We deleted compileYieldStmt because blocks handle yields directly.
		// If Generate hits a stray yield, we just evaluate the expression.
		_, err = c.evaluateExpression(n.Value)

	case *ast.ExprStmt:
		err = c.compileExprStmt(n)
	}

	return err
}

func (c *Codegen) String() string {
	return c.module.String()
}

// llvmTypeFor dynamically translates our semantic types into LLVM memory layouts.
func (c *Codegen) llvmTypeFor(t sema.Type) types.Type {
	switch t := t.(type) {

	case sema.IntType:
		return types.I32

	case sema.BoolType:
		return types.I1

	case sema.StringType:
		// A MAML string is a Fat Pointer: { i8*, i32 }
		return types.NewStruct(types.I8Ptr, types.I32)

	case *sema.StructType:
		// Dynamically build the LLVM struct based on the semantic fields!
		var llvmFields []types.Type
		for _, f := range t.Fields {
			llvmFields = append(llvmFields, c.llvmTypeFor(f.Type))
		}
		return types.NewStruct(llvmFields...)
	}

	// Fallback
	return types.I32
}
