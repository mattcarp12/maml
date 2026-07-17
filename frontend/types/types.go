package types

// Cap represents a memory reference capability (passing convention / alias rule).
type Cap string

const (
	CapNone Cap = ""    // Primitives or unannotated (default to CapRo)
	CapOwn  Cap = "own" // Ownership transfer
	CapMut  Cap = "mut" // Exclusive mutable borrow
	CapRo   Cap = "ro"  // Shared read-only borrow
)

func (t *StructType) GetFieldIndex(name string) int {
	for i, f := range t.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func (v SumVariant) IsUnit() bool { return len(v.Fields) == 0 }
func (t *SumType) GetVariant(name string) *SumVariant {
	for i := range t.Variants {
		if t.Variants[i].Name == name {
			return &t.Variants[i]
		}
	}
	return nil
}

// --- FUNCTION ---
type FunctionType struct {
	Params []Type
	Caps   []Cap
	Return Type
}

func (t *FunctionType) Equals(other Type) bool {
	o, ok := other.(*FunctionType)
	if !ok || len(t.Params) != len(o.Params) || !t.Return.Equals(o.Return) {
		return false
	}
	for i := range t.Params {
		if !t.Params[i].Equals(o.Params[i]) {
			return false
		}
	}
	return true
}
func (t *FunctionType) isType()               {}
func (t *FunctionType) IsReferenceType() bool { return true }
func (t *FunctionType) SizeInBytes() int      { return 8 }
func (t *FunctionType) Alignment() int        { return 8 }

// --- UNKNOWN ---

func IsUnknown(other Type) bool {
	_, ok := other.(UnknownType)
	return ok
}

// --- ANY ---

func IsAny(other Type) bool {
	_, ok := other.(AnyType)
	return ok
}

func MergeTypes(t1, t2 Type) Type {
	if t1 == nil || t2 == nil {
		return nil
	}
	if t1.Equals(t2) {
		return t1
	}
	if !IsUnknown(t1) && !IsUnknown(t2) {
		return UnknownType{}
	}
	if IsUnknown(t1) {
		return t2
	}
	return t1
}

func IsIntegerType(t Type) bool {
	switch t.(type) {
	case I8Type, I16Type, I32Type, I64Type, I128Type,
		U8Type, U16Type, U32Type, U64Type, U128Type:
		return true
	}
	return false
}

func CanRepresentInt(value int64, t Type) bool {
	switch t.(type) {
	case I8Type:
		return value >= -128 && value <= 127
	case I16Type:
		return value >= -32768 && value <= 32767
	case I32Type:
		return value >= -2147483648 && value <= 2147483647
	case I64Type, I128Type:
		return true
	case U8Type:
		return value >= 0 && value <= 255
	case U16Type:
		return value >= 0 && value <= 65535
	case U32Type:
		return value >= 0 && value <= 4294967295
	case U64Type, U128Type:
		return value >= 0
	default:
		return false
	}
}
