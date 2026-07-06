package types

// Cap represents a memory reference capability (passing convention / alias rule).
type Cap string

const (
	CapNone Cap = ""     // Primitives or unannotated
	CapOwn  Cap = "own"  // Ownership transfer
	CapMut  Cap = "mut"  // Exclusive mutable borrow
	CapRo   Cap = "ro"   // Shared read-only borrow
	CapCopy Cap = "copy" // Independent deep copy
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
