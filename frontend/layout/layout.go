package layout

import (
	"sort"

	"github.com/mattcarp12/maml/frontend/types"
)

// Target defines the physical architecture constraints.
type Target struct {
	PointerSize  int
	PointerAlign int
	IntSize      int
}

var DefaultTarget = &Target{
	PointerSize:  8,
	PointerAlign: 8,
	IntSize:      8,
}

// ============================================================================
// Public Layout API
// ============================================================================

func SizeOf(t types.Type, target *Target) int {
	if t == nil {
		return 0
	}
	// We no longer check t.IsReferenceType() for ARC headers
	return sizeOfDynamic(t, target)
}

func OffsetOf(t *types.StructType, fieldIndex int, target *Target) int {
	if t == nil || fieldIndex < 0 || fieldIndex >= len(t.Fields) {
		return 0
	}
	offsets, _ := computeStructLayout(t, target)
	return offsets[fieldIndex]
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
	case types.I64Type:
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

func sizeOfDynamic(t types.Type, target *Target) int {
	switch v := t.(type) {
	case types.I8Type, types.U8Type:
		return 1
	case types.I16Type, types.U16Type:
		return 2
	case types.I32Type, types.U32Type, types.F32Type:
		return 4
	case types.I64Type, types.U64Type, types.F64Type:
		return 8
	case types.I128Type, types.U128Type:
		return 16
	case types.BoolType:
		return 1
	case types.CharType:
		return 4
	case types.UnitType, types.UnknownType:
		return 0
	case *types.StructType:
		return sizeOfStruct(v, target)
	case *types.SumType:
		return sizeOfSumType(v, target)
	case *types.ArrayType:
		return SizeOf(v.Base, target) * v.Size
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
			size:          SizeOf(f.Type, target), // Fields are packed
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
			payloadSize += SizeOf(f.Type, target)
		}
		if payloadSize > maxPayloadSize {
			maxPayloadSize = payloadSize
		}
	}
	return padToAlign(4+maxPayloadSize, AlignOf(t, target))
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
