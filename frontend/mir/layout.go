package mir

import (
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

// TODO - Figure out better place to put these.
const (
	FUTURE_SIZE  = 8
	FUTURE_ALIGN = 8
)

// ============================================================================
// Public Layout API
// ============================================================================

func SizeOf(t types.Type, target *Target) int {
	if t == nil {
		return 0
	}
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
	switch t.(type) {
	case types.StringType, *types.StringType:
		return STRING_ALIGN
	case *types.VectorType:
		return VECTOR_ALIGN
	case *types.ViewType:
		return VIEW_ALIGN
	case *types.MapType:
		return MAP_ALIGN
	case *types.FutureType:
		return FUTURE_ALIGN
	case *types.RefType:
		return REF_ALIGN
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
	// Built‑in container types – sizes from ABI constants
	case types.StringType, *types.StringType:
		return STRING_SIZE
	case *types.VectorType:
		return VECTOR_SIZE
	case *types.ViewType:
		return VIEW_SIZE
	case *types.MapType:
		return MAP_SIZE
	case *types.FutureType:
		return FUTURE_SIZE
	case *types.RefType:
		return REF_SIZE

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

	for i, f := range t.Fields {
		align := AlignOf(f.Type, target)
		size := SizeOf(f.Type, target)

		currentOffset = padToAlign(currentOffset, align)
		offsets[i] = currentOffset
		currentOffset += size
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
