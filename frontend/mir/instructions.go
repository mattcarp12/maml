package mir

import (
	"fmt"

	"github.com/mattcarp12/maml/frontend/hir"
	"github.com/mattcarp12/maml/frontend/types"
)

// Instruction represents a single, flat, executable operation in a BasicBlock.
type Instruction interface {
	isInstruction()
	String() string
}

type MIRCallArg struct {
	Argument hir.Operand
	Mut      bool
}

type AssignInst struct {
	Dst    string
	RValue hir.Operand
}

type IndexAssignInst struct {
	Target     string
	TargetType types.Type
	Index      hir.Operand
	Value      hir.Operand
}

type StructInitInst struct {
	Dst           string
	FieldName     string
	FieldIndex    int
	Value         hir.Operand
	VariantLayout []types.Type
}

type FieldReadInst struct {
	Dst           string
	Object        hir.Operand
	FieldName     string
	FieldIndex    int
	Type          types.Type
	VariantLayout []types.Type
}

type ArrayInitInst struct {
	Dst   string
	Index int
	Value hir.Operand
}

type SliceInst struct {
	Dst           string
	Left          hir.Operand
	ContainerType types.Type
	Low           hir.Operand
	High          hir.Operand
	ResultType    types.Type
}

type BinaryOpInst struct {
	Dst      string
	Operator string
	Left     hir.Operand
	Right    hir.Operand
	Type     types.Type
}

type UnaryOpInst struct {
	Dst      string
	Operator string
	Operand  hir.Operand
	Type     types.Type
}

type IndexReadInst struct {
	Dst        string
	Source     hir.Operand
	SourceType types.Type
	Index      hir.Operand
	Type       types.Type
}

type CallInst struct {
	Dst       string
	Function  hir.Operand
	Arguments []MIRCallArg // Use the new decoupled struct!
	Type      types.Type
}

type VariantInitInst struct {
	Dst          string
	VariantName  string
	Discriminant int
	Payloads     []hir.Operand
	Type         types.Type
}

type VariantReadInst struct {
	Dst          string
	Object       hir.Operand
	VariantName  string
	PayloadIndex int
	Type         types.Type
}

type VariantDiscriminantInst struct {
	Dst    string
	Object hir.Operand
	Type   types.Type
}

type CastInst struct {
	Dst  string
	Src  hir.Operand
	Type types.Type
}

type LoadPtrInst struct {
	Dst  string
	Ptr  hir.Operand
	Type types.Type
}

type StoreInst struct {
	DstPtr string
	Value  hir.Operand
	Type   types.Type
}

// TempDeclInst declares a new explicit temporary variable to hold an intermediate value.
type TempDeclInst struct {
	Name string
	Type types.Type
}

func (TempDeclInst) isInstruction()    {}
func (i *TempDeclInst) String() string { return fmt.Sprintf("let %s: %s", i.Name, i.Type.String()) }

func (AssignInst) isInstruction() {}
func (i *AssignInst) String() string {
	return fmt.Sprintf("%s = %s", i.Dst, i.RValue.String())
}

func (i *IndexAssignInst) isInstruction() {}
func (i *IndexAssignInst) String() string { return "index_set " + i.Target }

func (s *StructInitInst) isInstruction() {}
func (s *StructInitInst) String() string {
	return fmt.Sprintf("%s.%s[%d] = %s", s.Dst, s.FieldName, s.FieldIndex, s.Value.String())
}

func (f *FieldReadInst) isInstruction() {}
func (f *FieldReadInst) String() string {
	return fmt.Sprintf("%s = %s.%s[%d]", f.Dst, f.Object.String(), f.FieldName, f.FieldIndex)
}

func (a *ArrayInitInst) isInstruction() {}
func (a *ArrayInitInst) String() string {
	return fmt.Sprintf("%s[%d] = %s", a.Dst, a.Index, a.Value.String())
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

func (BinaryOpInst) isInstruction() {}
func (i *BinaryOpInst) String() string {
	return fmt.Sprintf("%s = %s %s %s", i.Dst, i.Left.String(), i.Operator, i.Right.String())
}

func (UnaryOpInst) isInstruction() {}
func (i *UnaryOpInst) String() string {
	return fmt.Sprintf("%s = %s%s", i.Dst, i.Operator, i.Operand.String())
}

func (IndexReadInst) isInstruction() {}
func (i *IndexReadInst) String() string {
	return fmt.Sprintf("%s = %s[%s]", i.Dst, i.Source.String(), i.Index.String())
}

func (CallInst) isInstruction() {}
func (i *CallInst) String() string {
	return fmt.Sprintf("%s = call %s(...)", i.Dst, i.Function.String())
}

func (v *VariantInitInst) isInstruction() {}
func (v *VariantInitInst) String() string {
	return fmt.Sprintf("%s = init_variant %s (disc: %d)", v.Dst, v.VariantName, v.Discriminant)
}

func (v *VariantReadInst) isInstruction() {}
func (v *VariantReadInst) String() string {
	return fmt.Sprintf("%s = read_variant %s as %s[%d]", v.Dst, v.Object.String(), v.VariantName, v.PayloadIndex)
}

func (v *VariantDiscriminantInst) isInstruction() {}
func (v *VariantDiscriminantInst) String() string {
	return fmt.Sprintf("%s = read_discriminant %s", v.Dst, v.Object.String())
}

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
	Dst string
}

func (MutBorrowInst) isInstruction()    {}
func (i *MutBorrowInst) String() string { return fmt.Sprintf("mut_borrow %s", i.Src) }

func (CastInst) isInstruction() {}
func (i *CastInst) String() string {
	return fmt.Sprintf("%s = cast %s to %s", i.Dst, i.Src.String(), i.Type.String())
}

func (LoadPtrInst) isInstruction() {}
func (i *LoadPtrInst) String() string {
	return fmt.Sprintf("%s = load_ptr %s as %s", i.Dst, i.Ptr.String(), i.Type.String())
}

func (StoreInst) isInstruction() {}
func (i *StoreInst) String() string {
	return fmt.Sprintf("store %s -> %s", i.Value.String(), i.DstPtr)
}

// =============================================================================
// Explicit Coroutine Instructions
// =============================================================================

// CoroPrologueInst explicitly allocates the coroutine frame memory
// and initializes the LLVM coroutine handle and promise.
type CoroPrologueInst struct{}

func (CoroPrologueInst) isInstruction()    {}
func (i *CoroPrologueInst) String() string { return "coro_prologue" }
