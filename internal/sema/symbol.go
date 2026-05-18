// sema/symbol.go
package sema

type SymbolKind string

const (
	VarSymbol     SymbolKind = "var"
	FuncSymbol    SymbolKind = "func"
	VariantSymbol SymbolKind = "variant"
)

type Symbol struct {
	Kind        SymbolKind
	Name        string
	Mutable     bool
	Type        Type
	Invalidated bool
	ParamMode   ParamMode
	SumType     *SumType    // for variant symbols, the sum type they belong to
	Variant     *SumVariant // for variant symbols, the variant they represent
}

type ParamMode int

const (
	ParamBorrow    ParamMode = iota // fn f(x T)
	ParamMutBorrow                  // fn f(mut x T)
	ParamOwned                      // fn f(own x T)
)
