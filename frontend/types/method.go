package types

// MethodKind represents a unique identifier for a built-in method operation.
// This allows downstream compiler passes (like MIR lowering) to identify the
// operation without parsing string names or mangled symbols.
type MethodKind int

const (
	MethodNone MethodKind = iota

	// Vector methods
	MethodVecPush
	MethodVecPop
	MethodVecLen

	// Map methods
	MethodMapPut
	MethodMapGet
	MethodMapRemove
)

// String returns a readable representation of the built-in method kind.
func (m MethodKind) String() string {
	switch m {
	case MethodVecPush:
		return "push"
	case MethodVecPop:
		return "pop"
	case MethodVecLen:
		return "len"
	case MethodMapPut:
		return "put"
	case MethodMapGet:
		return "get"
	case MethodMapRemove:
		return "remove"
	default:
		return "none"
	}
}