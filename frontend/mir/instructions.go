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
	RValue hir.Expr
}

func (AssignInst) isInstruction() {}
func (i *AssignInst) String() string {
	return fmt.Sprintf("%s = %s", i.RValue.String(), i.RValue.String())
}

type IndexAssignInst struct {
	Target string
	Index  hir.Expr
	Value  hir.Expr
}

func (i *IndexAssignInst) isInstruction() {}
func (i *IndexAssignInst) String() string { return "index_set " + i.Target }

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
