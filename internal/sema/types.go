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
	Alignment() int        // Boundary requirement for this type
}

// --- INT ---
type IntType struct{}

func (t IntType) String() string         { return "int" }
func (t IntType) Equals(other Type) bool { _, ok := other.(IntType); return ok }
func (t IntType) IsReferenceType() bool  { return false }
func (t IntType) SizeInBytes() int       { return 4 }
func (t IntType) Alignment() int         { return 4 }

// --- BOOL ---
type BoolType struct{}

func (t BoolType) String() string         { return "bool" }
func (t BoolType) Equals(other Type) bool { _, ok := other.(BoolType); return ok }
func (t BoolType) IsReferenceType() bool  { return false }
func (t BoolType) SizeInBytes() int       { return 1 } // 1 byte is enough for a bool
func (t BoolType) Alignment() int         { return 1 }

// --- STRING ///
type StringType struct{}

func (t StringType) String() string         { return "string" }
func (t StringType) Equals(other Type) bool { _, ok := other.(StringType); return ok }
func (t StringType) IsReferenceType() bool  { return true }
func (t StringType) SizeInBytes() int       { return 16 } // e.g., 8 bytes for pointer, 8 for length
func (t StringType) Alignment() int         { return 8 }  // Largest field is a pointer (8)

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
func (t *StructType) Alignment() int {
	maxAlign := 1 // Default minimum alignment
	for _, f := range t.Fields {
		align := f.Type.Alignment()
		if align > maxAlign {
			maxAlign = align
		}
	}
	return maxAlign
}

func (t *StructType) SizeInBytes() int {
	size := 0
	maxAlign := t.Alignment()

	for _, f := range t.Fields {
		align := f.Type.Alignment()

		// 1. Calculate internal padding:
		// If the current size isn't a multiple of this field's alignment, push it forward.
		if size%align != 0 {
			size += align - (size % align)
		}

		// 2. Add the actual field size
		size += f.Type.SizeInBytes()
	}

	// 3. Calculate trailing padding:
	// The entire struct must be padded to a multiple of its largest alignment requirement.
	if size%maxAlign != 0 {
		size += maxAlign - (size % maxAlign)
	}

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
func (t ArrayType) Alignment() int {
	return t.Base.Alignment()
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
func (t SliceType) SizeInBytes() int      { return 24 } // 8 + 8 + 4 + 4 = 24
func (t SliceType) Alignment() int        { return 8 }  // Largest field is a pointer

// --- FUNCTION ---
type FunctionType struct {
	Params     []Type
	ParamModes []ParamMode
	Return     Type
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
func (t *FunctionType) Alignment() int        { return 8 }

// --- UNKNOWN ---
type UnknownType struct{}

func (t UnknownType) String() string         { return "unknown" }
func (t UnknownType) Equals(other Type) bool { _, ok := other.(UnknownType); return ok }
func (t UnknownType) IsReferenceType() bool  { return false }
func (t UnknownType) SizeInBytes() int       { return 0 }
func (t UnknownType) Alignment() int         { return 1 }
