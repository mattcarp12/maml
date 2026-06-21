package types

type SymbolKind string

const (
	VarSymbol     SymbolKind = "var"
	FuncSymbol    SymbolKind = "func"
	ParamSymbol   SymbolKind = "param"
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
	NotParam       ParamMode = iota // Default for local variables
	ParamBorrow                     // fn f(x T)
	ParamMutBorrow                  // fn f(mut x T)
	ParamOwn                        // fn f(own x T)
)
