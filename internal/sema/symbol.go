// sema/symbol.go
package sema

type SymbolKind string

const (
	VarSymbol  SymbolKind = "var"
	FuncSymbol SymbolKind = "func"
)

type Symbol struct {
	Kind    SymbolKind
	Name    string
	Mutable bool
	Type    Type
}
