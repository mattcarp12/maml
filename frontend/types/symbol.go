package types

type SymbolKind string

const (
	VarSymbol     SymbolKind = "var"
	FuncSymbol    SymbolKind = "func"
	ParamSymbol   SymbolKind = "param"
	VariantSymbol SymbolKind = "variant"
)

type Symbol struct {
	Kind    SymbolKind
	Name    string
	Type    Type
	Mutable bool        // True if declared with 'mut' locally (e.g., mut x := 10)
	Cap     Cap         // The boundary annotation (own, mut, ro, copy, or "")
	SumType *SumType    // For variant symbols, the sum type they belong to
	Variant *SumVariant // For variant symbols, the variant they represent
}
