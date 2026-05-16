package sema

import (
	"fmt"
	"strings"
)

type Type interface {
	String() string
	Equals(Type) bool
	IsReferenceType() bool // True if it's heap-allocated or passed via pointer under the hood
	SizeInBytes() int      // Used by CodeGen to allocate the right amount of space
}

// --- INT ---
type IntType struct{}

func (t IntType) String() string         { return "int" }
func (t IntType) Equals(other Type) bool { _, ok := other.(IntType); return ok }
func (t IntType) IsReferenceType() bool  { return false }
func (t IntType) SizeInBytes() int       { return 8 } // 64-bit int

// --- BOOL ---
type BoolType struct{}

func (t BoolType) String() string         { return "bool" }
func (t BoolType) Equals(other Type) bool { _, ok := other.(BoolType); return ok }
func (t BoolType) IsReferenceType() bool  { return false }
func (t BoolType) SizeInBytes() int       { return 1 } // 1 byte is enough for a bool

// --- STRING ///
type StringType struct{}

func (t StringType) String() string         { return "string" }
func (t StringType) Equals(other Type) bool { _, ok := other.(StringType); return ok }
func (t StringType) IsReferenceType() bool  { return true }
func (t StringType) SizeInBytes() int       { return 16 } // e.g., 8 bytes for pointer, 8 for length

// --- STRUCT ---
// Structs are generally value types (allocated on the stack/inline) unless explicitly boxed.
type StructType struct {
	Name   string
	Fields []StructField
}

func (t *StructType) String() string { return t.Name }
func (t *StructType) Equals(other Type) bool {
	o, ok := other.(*StructType)
	return ok && t.Name == o.Name
}
func (t *StructType) GetFieldIndex(name string) int {
	for i, f := range t.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}
func (t *StructType) IsReferenceType() bool { return false }
func (t *StructType) SizeInBytes() int {
	size := 0
	for _, f := range t.Fields {
		size += f.Type.SizeInBytes()
	}
	// Note: A real compiler would also calculate padding/alignment here!
	return size
}

type StructField struct {
	Name string
	Type Type
}

// --- ARRAY ---
type ArrayType struct {
	Base Type
	Size int
}

func (t ArrayType) String() string {
	return fmt.Sprintf("[%d]%s", t.Size, t.Base.String())
}
func (t ArrayType) Equals(other Type) bool {
	o, ok := other.(ArrayType)
	return ok && t.Size == o.Size && t.Base.Equals(o.Base)
}

// Fixed-size arrays are value types. They take up contiguous space.
func (t ArrayType) IsReferenceType() bool { return false }
func (t ArrayType) SizeInBytes() int {
	return t.Base.SizeInBytes() * t.Size
}

// --- SLICE ---
type SliceType struct {
	Base Type
}

func (t SliceType) String() string { return "[]" + t.Base.String() }
func (t SliceType) Equals(other Type) bool {
	o, ok := other.(SliceType)
	return ok && t.Base.Equals(o.Base)
}

// Slices are dynamic, meaning they are just lightweight headers pointing to a heap array.
func (t SliceType) IsReferenceType() bool { return true }
func (t SliceType) SizeInBytes() int {
	// e.g., 24 bytes: 8 for Pointer, 8 for Length, 8 for Capacity
	return 24
}

// --- FUNCTION ---
type FunctionType struct {
	Params []Type
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

// Function variables are essentially function pointers.
func (t *FunctionType) IsReferenceType() bool { return true }
func (t *FunctionType) SizeInBytes() int      { return 8 } // Just a pointer

// --- UNKNOWN ---
type UnknownType struct{}

func (t UnknownType) String() string         { return "unknown" }
func (t UnknownType) Equals(other Type) bool { _, ok := other.(UnknownType); return ok }
func (t UnknownType) IsReferenceType() bool  { return false }
func (t UnknownType) SizeInBytes() int       { return 0 }
