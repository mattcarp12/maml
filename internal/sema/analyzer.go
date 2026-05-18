// sema/analyzer.go
package sema

import (
	"fmt"

	"github.com/mattcarp12/maml/internal/ast"
)

type Analyzer struct {
	scope          *Scope
	errors         []ast.CompileError
	TypeMap        map[ast.Node]Type
	expectedReturn Type
	// aliasMap tracks immutable aliases: aliasMap[name] = the canonical source name.
	// When x is declared as an immutable alias of y (x := y where y is immutable),
	// aliasMap[x] = y. Both x and y are valid, shared, immutable references.
	aliasMap map[string]string
	// aliasRefCount tracks how many live immutable aliases a given canonical name has,
	// including itself. When refCount[name] > 1, mutable acquisition is illegal.
	aliasRefCount map[string]int
}

func New() *Analyzer {
	globalScope := newGlobalScope()

	return &Analyzer{
		scope:          globalScope,
		errors:         []ast.CompileError{},
		TypeMap:        make(map[ast.Node]Type),
		expectedReturn: nil,
		aliasMap:       make(map[string]string),
		aliasRefCount:  make(map[string]int),
	}
}

func (a *Analyzer) Analyze(program *ast.Program) ([]ast.CompileError, map[ast.Node]Type) {
	a.discoverTypes(program)
	a.resolveTypeBodies(program)
	a.registerFunctions(program)
	a.analyzeFunctionBodies(program)
	return a.errors, a.TypeMap
}

func (a *Analyzer) errorf(pos ast.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, ast.CompileError{
		Stage: "Sema",
		Pos:   pos,
		Msg:   fmt.Sprintf(format, args...),
	})
}

// canonicalName resolves a name to its ultimate alias source.
// If x -> y and y is not itself an alias, returns y.
func (a *Analyzer) canonicalName(name string) string {
	for {
		src, ok := a.aliasMap[name]
		if !ok {
			return name
		}
		name = src
	}
}

// recordAlias registers `alias` as an immutable alias of `source`.
// Both names are incremented against the canonical source's ref count.
func (a *Analyzer) recordAlias(alias, source string) {
	canon := a.canonicalName(source)
	a.aliasMap[alias] = canon
	// Ensure the canonical name has a ref count entry for itself if first time.
	if _, ok := a.aliasRefCount[canon]; !ok {
		a.aliasRefCount[canon] = 1
	}
	a.aliasRefCount[canon]++
}

// removeAlias removes a name from the alias tracking structures.
// Called when the name leaves scope.
func (a *Analyzer) removeAlias(name string) {
	canon, isAlias := a.aliasMap[name]
	if isAlias {
		a.aliasRefCount[canon]--
		delete(a.aliasMap, name)
	} else {
		// name may itself be a canonical source; decrement its own count.
		if count, ok := a.aliasRefCount[name]; ok {
			if count <= 1 {
				delete(a.aliasRefCount, name)
			} else {
				a.aliasRefCount[name] = count - 1
			}
		}
	}
}

// hasActiveAliases returns true if the given name has more than one live
// immutable reference (i.e. it has been aliased and those aliases are still
// in scope).
func (a *Analyzer) hasActiveAliases(name string) bool {
	canon := a.canonicalName(name)
	return a.aliasRefCount[canon] > 1
}
