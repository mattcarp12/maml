package layout

import (
	"sort"

	"github.com/mattcarp12/maml/frontend/types"
)

// ============================================================================
// Target Configuration
// ============================================================================

// Target defines the physical architecture constraints.
type Target struct {
	PointerSize  int // e.g., 8 for 64-bit, 4 for 32-bit
	PointerAlign int
	IntSize      int // e.g., 4 for i32
}

// DefaultTarget provides standard 64-bit architecture assumptions.
var DefaultTarget = &Target{
	PointerSize:  8,
	PointerAlign: 8,
	IntSize:      4,
}

// ============================================================================
// Public Layout API
// ============================================================================

// SizeOf computes the total byte size of a type, including tail padding.
func SizeOf(t types.Type, target *Target) int {
	if t == nil {
		return 0
	}

	if t.IsReferenceType() {
		return sizeOfReference(t, target)
	}

	switch v := t.(type) {
	case types.IntType:
		return 4
	case types.BoolType:
		return 1
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

// AlignOf computes the strict memory boundary requirement for a type.
func AlignOf(t types.Type, target *Target) int {
	if t == nil {
		return 1
	}

	if t.IsReferenceType() {
		return target.PointerAlign
	}

	switch v := t.(type) {
	case types.IntType:
		return 4
	case types.BoolType:
		return 1
	case types.UnitType, types.UnknownType:
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
		maxAlign := 4 // Discriminant is i32
		for _, variant := range v.Variants {
			for _, f := range variant.Fields {
				if a := AlignOf(f.Type, target); a > maxAlign {
					maxAlign = a
				}
			}
			for _, t := range variant.TupleTypes {
				if a := AlignOf(t, target); a > maxAlign {
					maxAlign = a
				}
			}
		}
		return maxAlign
	case *types.ArrayType:
		return AlignOf(v.Base, target)
	}
	return 1
}

// OffsetOf returns the byte offset of a specific field within a struct.
func OffsetOf(t *types.StructType, fieldIndex int, target *Target) int {
	if t == nil || fieldIndex < 0 || fieldIndex >= len(t.Fields) {
		return 0
	}
	offsets, _ := computeStructLayout(t, target)
	return offsets[fieldIndex]
}

// ============================================================================
// Internal Calculators
// ============================================================================

func sizeOfReference(t types.Type, target *Target) int {
	// Must exactly mirror TypeLowering.cpp lowerPrimitiveType logic
	switch t.(type) {
	case types.StringType:
		// {ptr, i32} -> 8 + 4 = 12, padded to 16 on 64-bit
		size := target.PointerSize + 4
		return padToAlign(size, target.PointerAlign)
	case *types.ViewType:
		// {ptr, ptr, i32, i32}
		size := (target.PointerSize * 2) + 8
		return padToAlign(size, target.PointerAlign)
	case types.AnyType, *types.MapType, *types.VectorType, *types.FutureType:
		return target.PointerSize
	default:
		return target.PointerSize
	}
}

func sizeOfStruct(t *types.StructType, target *Target) int {
	_, totalSize := computeStructLayout(t, target)
	return totalSize
}

// computeStructLayout is the core dual-layout routing engine.
// It returns an array mapping the original field indices to their byte offsets,
// alongside the total padded size of the struct.
func computeStructLayout(t *types.StructType, target *Target) ([]int, int) {
	offsets := make([]int, len(t.Fields))
	if len(t.Fields) == 0 {
		return offsets, 0
	}

	maxAlign := AlignOf(t, target)
	currentOffset := 0

	if t.IsReprC {
		// Strict C ABI: Sequential processing
		for i, f := range t.Fields {
			align := AlignOf(f.Type, target)
			currentOffset = padToAlign(currentOffset, align)
			offsets[i] = currentOffset
			currentOffset += SizeOf(f.Type, target)
		}
	} else {
		// Native MAML ABI: Packed field reordering
		// We sort fields by alignment descending to eliminate internal padding.
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
				size:          SizeOf(f.Type, target),
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
	}

	// Tail padding to satisfy array alignment
	totalSize := padToAlign(currentOffset, maxAlign)
	return offsets, totalSize
}

func sizeOfSumType(t *types.SumType, target *Target) int {
	// MAML SumType layout: { i32 discriminant, payload }
	// Must exactly mirror TypeLowering.cpp lowerSumType logic
	maxPayloadSize := 0

	for _, variant := range t.Variants {
		payloadSize := 0
		for _, f := range variant.Fields {
			payloadSize = padToAlign(payloadSize, AlignOf(f.Type, target))
			payloadSize += SizeOf(f.Type, target)
		}
		for _, typ := range variant.TupleTypes {
			payloadSize = padToAlign(payloadSize, AlignOf(typ, target))
			payloadSize += SizeOf(typ, target)
		}

		// Align payload's internal structure
		payloadAlign := 1
		if len(variant.Fields) > 0 {
			payloadAlign = AlignOf(variant.Fields[0].Type, target)
		} else if len(variant.TupleTypes) > 0 {
			payloadAlign = AlignOf(variant.TupleTypes[0], target)
		}
		payloadSize = padToAlign(payloadSize, payloadAlign)

		if payloadSize > maxPayloadSize {
			maxPayloadSize = payloadSize
		}
	}

	discriminantSize := 4
	align := AlignOf(t, target)

	// Layout: [discriminant (4 bytes)] [padding?] [payload]
	totalSize := padToAlign(discriminantSize, align) + maxPayloadSize
	return padToAlign(totalSize, align)
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
