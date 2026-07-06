package types

import (
	"fmt"
	"strings"
)

// ============================================================================
// String() and MangledName() for all types
// ============================================================================

// --- Primitives (value receivers) ---

func (I8Type) String() string      { return "i8" }
func (I8Type) MangledName() string { return "i8" }

func (I16Type) String() string      { return "i16" }
func (I16Type) MangledName() string { return "i16" }

func (I32Type) String() string      { return "i32" }
func (I32Type) MangledName() string { return "i32" }

func (I64Type) String() string      { return "i64" }
func (I64Type) MangledName() string { return "i64" }

func (I128Type) String() string      { return "i128" }
func (I128Type) MangledName() string { return "i128" }

func (U8Type) String() string      { return "u8" }
func (U8Type) MangledName() string { return "u8" }

func (U16Type) String() string      { return "u16" }
func (U16Type) MangledName() string { return "u16" }

func (U32Type) String() string      { return "u32" }
func (U32Type) MangledName() string { return "u32" }

func (U64Type) String() string      { return "u64" }
func (U64Type) MangledName() string { return "u64" }

func (U128Type) String() string      { return "u128" }
func (U128Type) MangledName() string { return "u128" }

func (F32Type) String() string      { return "f32" }
func (F32Type) MangledName() string { return "f32" }

func (F64Type) String() string      { return "f64" }
func (F64Type) MangledName() string { return "f64" }

func (BoolType) String() string      { return "bool" }
func (BoolType) MangledName() string { return "bool" }

func (CharType) String() string      { return "char" }
func (CharType) MangledName() string { return "char" }

func (UnitType) String() string      { return "unit" }
func (UnitType) MangledName() string { return "unit" }

func (StringType) String() string      { return "string" }
func (StringType) MangledName() string { return "string" }

func (AnyType) String() string      { return "any" }
func (AnyType) MangledName() string { return "any" }

func (UnknownType) String() string      { return "unknown" }
func (UnknownType) MangledName() string { return "unknown" }

func (PtrType) String() string      { return "ptr" }
func (PtrType) MangledName() string { return "ptr" }

// --- Composites (pointer receivers) ---

// StructType
func (t *StructType) String() string      { return t.Name }
func (t *StructType) MangledName() string { return t.Name }

// SumType – already present in your types.go, but I include it here for completeness.
// If you have it, you can omit this block.
func (t *SumType) String() string {
	if len(t.TypeArgs) == 0 {
		return t.BaseName
	}
	args := make([]string, len(t.TypeArgs))
	for i, a := range t.TypeArgs {
		args[i] = a.String()
	}
	return fmt.Sprintf("%s<%s>", t.BaseName, strings.Join(args, ", "))
}
func (t *SumType) MangledName() string {
	if len(t.TypeArgs) == 0 {
		return t.BaseName
	}
	args := make([]string, len(t.TypeArgs))
	for i, a := range t.TypeArgs {
		args[i] = a.MangledName()
	}
	return fmt.Sprintf("%s_%s", t.BaseName, strings.Join(args, "_"))
}

// --- Containers (pointer receivers) ---

func (t *ArrayType) String() string {
	return fmt.Sprintf("[%d]%s", t.Size, t.Base.String())
}
func (t *ArrayType) MangledName() string {
	return fmt.Sprintf("array_%d_%s", t.Size, t.Base.MangledName())
}

func (t *ViewType) String() string {
	return fmt.Sprintf("[]%s", t.Base.String())
}
func (t *ViewType) MangledName() string {
	if t.IsMut {
		return fmt.Sprintf("view_%s_mut", t.Base.MangledName())
	}
	return fmt.Sprintf("view_%s", t.Base.MangledName())
}

func (t *VectorType) String() string {
	return fmt.Sprintf("Vec<%s>", t.Base.String())
}
func (t *VectorType) MangledName() string {
	return fmt.Sprintf("Vec_%s", t.Base.MangledName())
}

func (t *MapType) String() string {
	return fmt.Sprintf("Map<%s, %s>", t.Key.String(), t.Value.String())
}
func (t *MapType) MangledName() string {
	return fmt.Sprintf("Map_%s_%s", t.Key.MangledName(), t.Value.MangledName())
}

func (t *FutureType) String() string {
	return fmt.Sprintf("Future<%s>", t.Base.String())
}
func (t *FutureType) MangledName() string {
	return fmt.Sprintf("Future_%s", t.Base.MangledName())
}

func (t *RefType) String() string {
	return fmt.Sprintf("Ref<%s>", t.Base.String())
}
func (t *RefType) MangledName() string {
	return fmt.Sprintf("Ref_%s", t.Base.MangledName())
}

func (t *WeakRefType) String() string {
	return fmt.Sprintf("WeakRef<%s>", t.Base.String())
}
func (t *WeakRefType) MangledName() string {
	return fmt.Sprintf("WeakRef_%s", t.Base.MangledName())
}

func (t *SenderType) String() string {
	return fmt.Sprintf("Sender<%s>", t.Base.String())
}
func (t *SenderType) MangledName() string {
	return fmt.Sprintf("Sender_%s", t.Base.MangledName())
}

func (t *ReceiverType) String() string {
	return fmt.Sprintf("Receiver<%s>", t.Base.String())
}
func (t *ReceiverType) MangledName() string {
	return fmt.Sprintf("Receiver_%s", t.Base.MangledName())
}

// --- FunctionType (if not already defined) ---
func (t *FunctionType) String() string {
	var params []string
	for _, p := range t.Params {
		params = append(params, p.String())
	}
	return "fn(" + strings.Join(params, ", ") + ") " + t.Return.String()
}
func (t *FunctionType) MangledName() string {
	params := make([]string, len(t.Params))
	for i, p := range t.Params {
		params[i] = p.MangledName()
	}
	return fmt.Sprintf("fn_%s_%s", strings.Join(params, "_"), t.Return.MangledName())
}
