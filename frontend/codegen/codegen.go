package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/escape"
	"github.com/mattcarp12/maml/frontend/sema"
)

type VarInfo struct {
	V      value.Value
	IsHeap bool
}

type Env struct {
	parent *Env
	vars   map[string]VarInfo
}

type Codegen struct {
	module       *ir.Module
	currentBlock *ir.Block
	currentEnv   *Env
	typeMap      map[ast.Node]sema.Type
	escapeMap    map[ast.Node]escape.EscapeState
	typeCache    map[string]types.Type
	labelCounter int
	runtimeFuncs map[RuntimeFunc]*ir.Func
	scopeStack   [][]value.Value
	bindingTypes map[string]sema.Type
	memcpyFunc   *ir.Func
}

type CGValue struct {
	V         value.Value
	Type      sema.Type
	IsAddress bool
	IsHeap    bool
}

func New() *Codegen {
	c := &Codegen{
		module:       ir.NewModule(),
		currentEnv:   &Env{vars: make(map[string]VarInfo)},
		typeCache:    make(map[string]types.Type),
		runtimeFuncs: make(map[RuntimeFunc]*ir.Func),
		scopeStack:   make([][]value.Value, 0),
		bindingTypes: make(map[string]sema.Type),
	}
	c.scopeStack = append(c.scopeStack, []value.Value{})
	c.setupRuntimeFuncs()
	return c
}

func (c *Codegen) Module() *ir.Module {
	return c.module
}

func (c *Codegen) pushEnv() {
	c.currentEnv = &Env{parent: c.currentEnv, vars: make(map[string]VarInfo)}
}

func (c *Codegen) popEnv() {
	if c.currentEnv != nil {
		c.currentEnv = c.currentEnv.parent
	}
}

func (c *Codegen) resolveVar(name string) (VarInfo, bool) {
	for e := c.currentEnv; e != nil; e = e.parent {
		if val, ok := e.vars[name]; ok {
			return val, true
		}
	}
	return VarInfo{}, false
}

func (c *Codegen) setVar(name string, val value.Value, isHeap bool) {
	c.currentEnv.vars[name] = VarInfo{V: val, IsHeap: isHeap}
}

// Generate is the main entry point
func (c *Codegen) Generate(node ast.Node, typeMap map[ast.Node]sema.Type, escapeMap map[ast.Node]escape.EscapeState) error {
	if typeMap != nil {
		c.typeMap = typeMap
	}

	if escapeMap != nil {
		c.escapeMap = escapeMap
	}

	var err error
	switch n := node.(type) {
	case *ast.Program:
		for _, decl := range n.Decls {
			if err := c.Generate(decl, nil, nil); err != nil {
				return err
			}
		}

	case *ast.FnDecl:
		err = c.compileFnDecl(n)

	case *ast.TypeDecl:
		// Type declarations (structs, etc.) are purely for the type checker.
		// Codegen ignores them because type information is in c.typeMap.
		return nil

	default:
		return fmt.Errorf("codegen error: unsupported top-level node: %T", node)
	}

	return err
}

func (c *Codegen) String() string {
	return c.module.String()
}

// Validate performs structural sanity check on the generated IR.
func (c *Codegen) Validate() error {
	for _, f := range c.module.Funcs {
		for _, block := range f.Blocks {
			if block.Term == nil {
				return fmt.Errorf("codegen error: block '%s' in function '%s' is missing a terminator",
					block.LocalName, f.Ident())
			}
		}
	}
	return nil
}
