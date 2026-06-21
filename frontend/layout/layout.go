package layout

import (
	"sort"

	"github.com/mattcarp12/maml/frontend/types"
)

// Target defines the physical architecture constraints.
type Target struct {
	PointerSize  int // e.g., 8 for 64-bit
	PointerAlign int
	IntSize      int
}

var DefaultTarget = &Target{
	PointerSize:  8,
	PointerAlign: 8,
	IntSize:      4,
}

// ============================================================================
// Public Layout API
// ============================================================================

// SizeOf computes the total byte size of a type.
// If isEscaped is true, it accounts for the 8-byte ARC RefCount header.
func SizeOf(t types.Type, target *Target, isEscaped bool) int {
	if t == nil {
		return 0
	}

	// 1. Calculate internal payload size
	var baseSize int
	if t.IsReferenceType() {
		baseSize = sizeOfReference(t, target)
	} else {
		baseSize = sizeOfDynamic(t, target, false) // Recursive fields are never 'escaped' themselves
	}

	// 2. Add header if this specific instance is escaped
	if isEscaped {
		return baseSize + target.PointerSize
	}
	return baseSize
}

// OffsetOf returns the byte offset of a field.
// If isEscaped is true, offsets are shifted by 8 bytes to account for the ARC header.
func OffsetOf(t *types.StructType, fieldIndex int, target *Target, isEscaped bool) int {
	if t == nil || fieldIndex < 0 || fieldIndex >= len(t.Fields) {
		return 0
	}

	headerShift := 0
	if isEscaped {
		headerShift = target.PointerSize
	}

	// Note: We compute layout with isEscaped=false because fields are packed
	// relative to the struct start, not the potential ARC header.
	offsets, _ := computeStructLayout(t, target)
	return headerShift + offsets[fieldIndex]
}

// AlignOf computes alignment based on contents.
func AlignOf(t types.Type, target *Target) int {
	if t == nil {
		return 1
	}
	if t.IsReferenceType() {
		return target.PointerAlign
	}

	switch v := t.(type) {
	case types.IntType:
		return target.IntSize
	case types.BoolType:
		return 1
	case *types.StructType:
		maxAlign := 1
		for _, f := range v.Fields {
			if a := AlignOf(f.Type, target); a > maxAlign {
				maxAlign = a
			}
		}
		return maxAlign
	case *types.SumType:
		return 4 // Discriminant i32
	case *types.ArrayType:
		return AlignOf(v.Base, target)
	}
	return 1
}

// ============================================================================
// Internal Calculators
// ============================================================================

func sizeOfDynamic(t types.Type, target *Target, isEscaped bool) int {
	switch v := t.(type) {
	case types.IntType:
		return target.IntSize
	case types.BoolType:
		return 1
	case types.UnitType, types.UnknownType:
		return 0
	case *types.StructType:
		return sizeOfStruct(v, target)
	case *types.SumType:
		return sizeOfSumType(v, target)
	case *types.ArrayType:
		return SizeOf(v.Base, target, false) * v.Size
	}
	return 0
}

func sizeOfStruct(t *types.StructType, target *Target) int {
	_, totalSize := computeStructLayout(t, target)
	return totalSize
}

func computeStructLayout(t *types.StructType, target *Target) ([]int, int) {
	offsets := make([]int, len(t.Fields))
	if len(t.Fields) == 0 {
		return offsets, 0
	}

	maxAlign := AlignOf(t, target)
	currentOffset := 0

	// We use field reordering (MAML ABI)
	type indexedField struct {
		originalIndex int
		align         int
		size          int
	}

	sortedFields := make([]indexedField, len(t.Fields))
	for i, f := range t.Fields {
		sortedFields[i] = indexedField{
			originalIndex: i,
			align:         AlignOf(f.Type, target),
			size:          SizeOf(f.Type, target, false), // Fields are packed
		}
	}

	sort.Slice(sortedFields, func(i, j int) bool {
		return sortedFields[i].align > sortedFields[j].align
	})

	for _, sf := range sortedFields {
		currentOffset = padToAlign(currentOffset, sf.align)
		offsets[sf.originalIndex] = currentOffset
		currentOffset += sf.size
	}

	return offsets, padToAlign(currentOffset, maxAlign)
}

func sizeOfSumType(t *types.SumType, target *Target) int {
	maxPayloadSize := 0
	for _, variant := range t.Variants {
		payloadSize := 0
		for _, f := range variant.Fields {
			payloadSize = padToAlign(payloadSize, AlignOf(f.Type, target))
			payloadSize += SizeOf(f.Type, target, false)
		}
		if payloadSize > maxPayloadSize {
			maxPayloadSize = payloadSize
		}
	}
	return padToAlign(4+maxPayloadSize, AlignOf(t, target))
}

func sizeOfReference(t types.Type, target *Target) int {
	switch t.(type) {
	case types.StringType:
		// A string is a fat pointer: { ptr, i32 }
		return target.PointerSize + target.IntSize + 4 // +4 for padding to 16 bytes
	default:
		return target.PointerSize
	}
}

func padToAlign(offset int, align int) int {
	if align <= 1 {
		return offset
	}
	rem := offset % align
	if rem != 0 {
		return offset + (align - rem)
	}
	return offset
}
