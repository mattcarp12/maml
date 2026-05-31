package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/tast"
	"github.com/mattcarp12/maml/frontend/types"
)

// Instruction represents a single, flat, executable operation in a BasicBlock.
type Instruction interface {
	isInstruction()
	String() string
}

// =============================================================================
// Structural Instructions
// =============================================================================

// TempDeclInst declares a new explicit temporary variable to hold an intermediate value.
type TempDeclInst struct {
	Name string
	Type types.Type
}

func (TempDeclInst) isInstruction()    {}
func (i *TempDeclInst) String() string { return fmt.Sprintf("let %s: %s", i.Name, i.Type.String()) }

// AssignInst assigns an evaluated, flattened expression to a destination.
type AssignInst struct {
	Dst    string
	RValue tast.Expr `json:"expr"`
}

func (AssignInst) isInstruction() {}
func (i *AssignInst) String() string {
	return fmt.Sprintf("%s = %s", i.Dst, i.RValue.String())
}

type IndexAssignInst struct {
	Target     string
	TargetType types.Type
	Index      tast.Expr
	Value      tast.Expr
}

func (i *IndexAssignInst) isInstruction() {}
func (i *IndexAssignInst) String() string { return "index_set " + i.Target }

// StructInitInst initializes a single named field of an already-declared struct
// temporary by writing a flat expression value into the field slot.
//
// The backend lowers this into a getelementptr + store pair:
//
//	%ptr = getelementptr inbounds %StructType, ptr %dst, i32 0, i32 FieldIndex
//	store <FieldType> <value>, ptr %ptr
//
// One StructInitInst is emitted per field of every struct literal.  Emitting
// per-field instructions (rather than one monolithic AssignInst wrapping the
// entire StructLiteral) keeps all MIR instructions uniform and backend-agnostic.
type StructInitInst struct {
	// Dst is the name of the temporary that holds the struct being initialized.
	Dst string
	// FieldName is the source-level identifier (preserved for debug info).
	FieldName string
	// FieldIndex is the zero-based declaration-order index into the struct's
	// field list, consumed by the backend to compute the GEP constant offset.
	FieldIndex int
	// Value is a fully-flattened expression — always an *tast.Identifier or a
	// primitive literal after flattenExpr processing.
	Value         tast.Expr
	VariantLayout []types.Type
}

func (s *StructInitInst) isInstruction() {}
func (s *StructInitInst) String() string {
	return fmt.Sprintf("%s.%s[%d] = %s", s.Dst, s.FieldName, s.FieldIndex, s.Value.String())
}

// FieldReadInst reads a single field out of a struct value and stores the
// result in a new temporary.
//
// The backend lowers this into a getelementptr + load pair:
//
//	%ptr = getelementptr inbounds %StructType, ptr %object, i32 0, i32 FieldIndex
//	%dst = load <FieldType>, ptr %ptr
//
// Using a dedicated FieldReadInst (rather than embedding a raw FieldAccess in
// an AssignInst) means the backend never needs to inspect tast nodes directly;
// all structural information is present on the instruction itself.
type FieldReadInst struct {
	// Dst is the temporary that receives the loaded field value.
	Dst string
	// Object is the flat (already-declared) expression whose struct field is
	// being read — always an *tast.Identifier after flattenExpr processing.
	Object tast.Expr
	// FieldName is the source-level identifier (preserved for debug info).
	FieldName string
	// FieldIndex is the zero-based declaration-order index used for GEP.
	FieldIndex int
	// Type is the type of the field being read (used for the LLVM load type).
	Type          types.Type
	VariantLayout []types.Type
}

func (f *FieldReadInst) isInstruction() {}
func (f *FieldReadInst) String() string {
	return fmt.Sprintf("%s = %s.%s[%d]", f.Dst, f.Object.String(), f.FieldName, f.FieldIndex)
}

// ArrayInitInst initializes a single element of an already-declared array
// temporary by writing a flat expression value into the element slot.
//
// The backend lowers this into a getelementptr + store pair:
//
//	%ptr = getelementptr inbounds [N x ElemType], ptr %dst, i32 0, i32 Index
//	store <ElemType> <value>, ptr %ptr
//
// One ArrayInitInst is emitted per element of every array literal.
type ArrayInitInst struct {
	// Dst is the name of the temporary that holds the array being initialized.
	Dst string
	// Index is the zero-based element position.
	Index int
	// Value is a fully-flattened expression — always an *tast.Identifier or a
	// primitive literal after flattenExpr processing.
	Value tast.Expr
}

func (a *ArrayInitInst) isInstruction() {}
func (a *ArrayInitInst) String() string {
	return fmt.Sprintf("%s[%d] = %s", a.Dst, a.Index, a.Value.String())
}

// SliceInst creates a new slice header from a contiguous region of an existing
// container. The backend lowers this differently per container kind:
//   - array  → GEP to element 0+Low, compute len/cap from High-Low / Size-Low
//   - slice/vector → load the data pointer, offset by Low
//   - string → load the char pointer, offset by Low
//
// Left is always a flat *tast.Identifier after flattenExpr processing.
// Low and High are flat expressions or nil (meaning 0 and full-length respectively).
type SliceInst struct {
	Dst           string
	Left          tast.Expr  // flat identifier, Type = container type
	ContainerType types.Type // the type of Left (Array/Slice/Vector/String)
	Low           tast.Expr  // nil means 0
	High          tast.Expr  // nil means full length
	ResultType    types.Type // always SliceType, VectorType, or StringType
}

func (s *SliceInst) isInstruction() {}
func (s *SliceInst) String() string {
	low, high := "_", "_"
	if s.Low != nil {
		low = s.Low.String()
	}
	if s.High != nil {
		high = s.High.String()
	}
	return fmt.Sprintf("%s = %s[%s:%s]", s.Dst, s.Left.String(), low, high)
}

// =============================================================================
// Explicit Memory Instructions (The 3-Layer Model)
// =============================================================================

// CopyInst explicitly duplicates a primitive value.
type CopyInst struct {
	Dst string
	Src string
}

func (CopyInst) isInstruction()    {}
func (i *CopyInst) String() string { return fmt.Sprintf("%s = copy %s", i.Dst, i.Src) }

// MoveInst explicitly transfers ownership of an affine type.
type MoveInst struct {
	Dst string
	Src string
}

func (MoveInst) isInstruction()    {}
func (i *MoveInst) String() string { return fmt.Sprintf("%s = move %s", i.Dst, i.Src) }

// RefAllocInst explicitly creates a new reference-managed heap allocation.
type RefAllocInst struct {
	Dst  string
	Type types.Type
}

func (RefAllocInst) isInstruction() {}
func (i *RefAllocInst) String() string {
	return fmt.Sprintf("%s = ref_alloc %s", i.Dst, i.Type.String())
}

// RefIncInst increments the reference count of an aliased handle.
type RefIncInst struct {
	Src string
}

func (RefIncInst) isInstruction()    {}
func (i *RefIncInst) String() string { return fmt.Sprintf("ref_inc %s", i.Src) }

// RefDecInst decrements the reference count (and potentially frees) a handle.
type RefDecInst struct {
	Src string
}

func (RefDecInst) isInstruction()    {}
func (i *RefDecInst) String() string { return fmt.Sprintf("ref_dec %s", i.Src) }

// MutBorrowInst explicitly enforces a temporary lexical exclusivity lock in the MIR.
type MutBorrowInst struct {
	Src string
}

func (MutBorrowInst) isInstruction()    {}
func (i *MutBorrowInst) String() string { return fmt.Sprintf("mut_borrow %s", i.Src) }

// =============================================================================
// Explicit Coroutine Instructions
// =============================================================================

// CoroPrologueInst explicitly allocates the coroutine frame memory
// and initializes the LLVM coroutine handle and promise.
type CoroPrologueInst struct{}

func (CoroPrologueInst) isInstruction()    {}
func (i *CoroPrologueInst) String() string { return "coro_prologue" }
