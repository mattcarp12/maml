// sema/symbol.go
package sema

type SymbolKind string

const (
	VarSymbol  SymbolKind = "var"
	FuncSymbol SymbolKind = "func"
)

type Symbol struct {
	Kind        SymbolKind
	Name        string
	Mutable     bool
	Type        Type
	Invalidated bool
	ParamMode   ParamMode
}

type ParamMode int

const (
	ParamBorrow    ParamMode = iota // fn f(x T)
	ParamMutBorrow                  // fn f(mut x T)
	ParamOwned                      // fn f(own x T)
)