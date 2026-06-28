// ==========================================================================
// frontend/types/types.go
// --------------------------------------------------------------------------
// This file defines the structs that represent the various types in MAML.
// These structs are attached by the semantic checker during TAST construction.
// ==========================================================================

package types

import (
	"fmt"
	"strings"
)

// Cap represents a memory reference capability (passing convention / alias rule).
type Cap string

const (
	CapNone Cap = ""     // Primitives or unannotated
	CapOwn  Cap = "own"  // Ownership transfer
	CapMut  Cap = "mut"  // Exclusive mutable borrow
	CapRo   Cap = "ro"   // Shared read-only borrow
	CapCopy Cap = "copy" // Independent deep copy
)

// --- BOOL ---

func (t BoolType) String() string { return "bool" }

// --- STRING ---

func (t StringType) String() string { return "string" }

// --- UNIT ---

func (t UnitType) String() string { return "unit" }

// --- PtrType ---
func (p PtrType) String() string { return "ptr" }

// --- STRUCT ---

func (t *StructType) String() string { return t.Name }
func (t *StructType) GetFieldIndex(name string) int {
	for i, f := range t.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// --- ARRAY ---

func (t ArrayType) String() string {
	return fmt.Sprintf("[%d]%s", t.Size, t.Base.String())
}

// --- VIEW ---

func (t ViewType) String() string { return "[]" + t.Base.String() }

// --- VECTOR ---

func (t VectorType) String() string { return "Vec<" + t.Base.String() + ">" }

// --- MAP ---

func (t MapType) String() string {
	return fmt.Sprintf("Map<%s, %s>", t.Key.String(), t.Value.String())
}

// --- OPTION ---
func NewOptionType(base Type) *SumType {
	return &SumType{
		BaseName: "Option",
		TypeArgs: []Type{base},
		Variants: []SumVariant{
			{Name: "Some", Discriminant: 0, TupleTypes: []Type{base}},
			{Name: "None", Discriminant: 1, TupleTypes: []Type{}},
		},
	}
}

// --- RESULT ---
func NewResultType(value Type, err Type) *SumType {
	return &SumType{
		BaseName: "Result",
		TypeArgs: []Type{value, err},
		Variants: []SumVariant{
			{Name: "Ok", Discriminant: 0, TupleTypes: []Type{value}},
			{Name: "Err", Discriminant: 1, TupleTypes: []Type{err}},
		},
	}
}

func (v SumVariant) IsUnit() bool { return len(v.Fields) == 0 }

func (t *SumType) String() string {
	if len(t.TypeArgs) == 0 {
		return t.BaseName
	}
	var args []string
	for _, arg := range t.TypeArgs {
		args = append(args, arg.String())
	}
	return fmt.Sprintf("%s<%s>", t.BaseName, strings.Join(args, ", "))
}

func (t *SumType) GetVariant(name string) *SumVariant {
	for i := range t.Variants {
		if t.Variants[i].Name == name {
			return &t.Variants[i]
		}
	}
	return nil
}

// --- Future ---

func (t *FutureType) String() string { return "Future<" + t.Base.String() + ">" }

// --- FUNCTION ---
type FunctionType struct {
	Params []Type
	Caps   []Cap
	Return Type
}

func (t *FunctionType) String() string {
	var params []string
	for _, p := range t.Params {
		params = append(params, p.String())
	}
	return "fn(" + strings.Join(params, ", ") + ") " + t.Return.String()
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
func (t *FunctionType) IsNeedsARC() bool      { return false }
func (t *FunctionType) IsReferenceType() bool { return true }
func (t *FunctionType) SizeInBytes() int      { return 8 }
func (t *FunctionType) Alignment() int        { return 8 }

// --- UNKNOWN ---

func (t UnknownType) String() string { return "unknown" }
func IsUnknown(other Type) bool {
	_, ok := other.(UnknownType)
	return ok
}

// --- ANY ---

func (t AnyType) String() string { return "any" }
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

func (t I8Type) String() string   { return "i8" }
func (t I16Type) String() string  { return "i16" }
func (t I32Type) String() string  { return "i32" }
func (t I64Type) String() string  { return "i64" }
func (t I128Type) String() string { return "i128" }
func (t U8Type) String() string   { return "u8" }
func (t U16Type) String() string  { return "u16" }
func (t U32Type) String() string  { return "u32" }
func (t U64Type) String() string  { return "u64" }
func (t U128Type) String() string { return "u128" }
func (t F32Type) String() string  { return "f32" }
func (t F64Type) String() string  { return "f64" }
func (t CharType) String() string { return "char" }

func (t *RefType) String() string      { return "Ref<" + t.Base.String() + ">" }
func (t *WeakRefType) String() string  { return "WeakRef<" + t.Base.String() + ">" }
func (t *SenderType) String() string   { return "Sender<" + t.Base.String() + ">" }
func (t *ReceiverType) String() string { return "Receiver<" + t.Base.String() + ">" }
