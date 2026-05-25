package types

type SymbolKind string

const (
	VarSymbol     SymbolKind = "var"
	FuncSymbol    SymbolKind = "func"
	VariantSymbol SymbolKind = "variant"
)

type Symbol struct {
	Kind      SymbolKind
	Name      string
	Type      Type
	Mutable   bool        // Defined at point of declaration (e.g., mut x := ...)
	ParamMode ParamMode   // How this variable behaves across a function boundary
	SumType   *SumType    // For variant symbols, the sum type they belong to
	Variant   *SumVariant // For variant symbols, the variant they represent
}

type ParamMode int

const (
	ParamBorrow    ParamMode = iota // fn f(x T)
	ParamMutBorrow                  // fn f(mut x T)
	ParamOwned                      // fn f(own x T)
)
