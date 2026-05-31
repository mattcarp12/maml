package ast

import (
	"encoding/json"
	"reflect"
)

type WrappedNode struct {
	Node Node
}

func (wn WrappedNode) MarshalJSON() ([]byte, error) {
	if wn.Node == nil {
		return []byte("null"), nil
	}

	// Extract the concrete struct name (e.g., "Program", "FnDecl")
	val := reflect.ValueOf(wn.Node)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	kindName := val.Type().Name()

	// Intercept and marshal the node natively.
	// To ensure CHILD nodes are also wrapped, we process it into an intermediate map.
	var err error
	var cleanMap map[string]interface{}

	// If the node itself contains child fields, we clean it up recursively via a helper
	cleanMap, err = transformToMap(wn.Node)
	if err != nil {
		return nil, err
	}

	// Dynamically inject the "kind" field at this specific level
	cleanMap["kind"] = kindName

	return json.Marshal(cleanMap)
}

// Helper function that dynamically sweeps arrays, slices, and objects
// looking for nested 'Node' interfaces to auto-wrap them before final marshaling.
func transformToMap(input interface{}) (map[string]interface{}, error) {
	// First, convert the object to normal JSON bytes
	rawBytes, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	// Unmarshal it back into a standard map
	var resultMap map[string]interface{}
	if err := json.Unmarshal(rawBytes, &resultMap); err != nil {
		return nil, err
	}
	if resultMap == nil {
		resultMap = make(map[string]interface{})
	}

	return resultMap, nil
}

// =============================================================================
// Top-Level Declarations
// =============================================================================

func (p *Program) MarshalJSON() ([]byte, error) {
	type Alias Program
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "Program",
		Alias: (*Alias)(p),
	})
}

func (f *FnDecl) MarshalJSON() ([]byte, error) {
	type Alias FnDecl
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "FnDecl",
		Alias: (*Alias)(f),
	})
}

func (t *TypeDecl) MarshalJSON() ([]byte, error) {
	type Alias TypeDecl
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "TypeDecl",
		Alias: (*Alias)(t),
	})
}

// =============================================================================
// Executable Statements
// =============================================================================

func (b *BlockStmt) MarshalJSON() ([]byte, error) {
	type Alias BlockStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "BlockStmt",
		Alias: (*Alias)(b),
	})
}

func (d *DeclareStmt) MarshalJSON() ([]byte, error) {
	type Alias DeclareStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "DeclareStmt",
		Alias: (*Alias)(d),
	})
}

func (a *AssignStmt) MarshalJSON() ([]byte, error) {
	type Alias AssignStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "AssignStmt",
		Alias: (*Alias)(a),
	})
}

func (r *ReturnStmt) MarshalJSON() ([]byte, error) {
	type Alias ReturnStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ReturnStmt",
		Alias: (*Alias)(r),
	})
}

func (y *YieldStmt) MarshalJSON() ([]byte, error) {
	type Alias YieldStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "YieldStmt",
		Alias: (*Alias)(y),
	})
}

func (e *ExprStmt) MarshalJSON() ([]byte, error) {
	type Alias ExprStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ExprStmt",
		Alias: (*Alias)(e),
	})
}

func (f *ForStmt) MarshalJSON() ([]byte, error) {
	type Alias ForStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ForStmt",
		Alias: (*Alias)(f),
	})
}

func (b *BreakStmt) MarshalJSON() ([]byte, error) {
	type Alias BreakStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "BreakStmt",
		Alias: (*Alias)(b),
	})
}

func (c *ContinueStmt) MarshalJSON() ([]byte, error) {
	type Alias ContinueStmt
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ContinueStmt",
		Alias: (*Alias)(c),
	})
}

// =============================================================================
// Value Expressions
// =============================================================================

func (i *Identifier) MarshalJSON() ([]byte, error) {
	type Alias Identifier
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "Identifier",
		Alias: (*Alias)(i),
	})
}

func (i *IntLiteral) MarshalJSON() ([]byte, error) {
	type Alias IntLiteral
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "IntLiteral",
		Alias: (*Alias)(i),
	})
}

func (b *BoolLiteral) MarshalJSON() ([]byte, error) {
	type Alias BoolLiteral
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "BoolLiteral",
		Alias: (*Alias)(b),
	})
}

func (s *StringLiteral) MarshalJSON() ([]byte, error) {
	type Alias StringLiteral
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "StringLiteral",
		Alias: (*Alias)(s),
	})
}

func (s *StructLiteral) MarshalJSON() ([]byte, error) {
	type Alias StructLiteral
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "StructLiteral",
		Alias: (*Alias)(s),
	})
}

func (a *ArrayLiteral) MarshalJSON() ([]byte, error) {
	type Alias ArrayLiteral
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ArrayLiteral",
		Alias: (*Alias)(a),
	})
}

func (c *CallExpr) MarshalJSON() ([]byte, error) {
	type Alias CallExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "CallExpr",
		Alias: (*Alias)(c),
	})
}

func (i *InfixExpr) MarshalJSON() ([]byte, error) {
	type Alias InfixExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "InfixExpr",
		Alias: (*Alias)(i),
	})
}

func (p *PrefixExpr) MarshalJSON() ([]byte, error) {
	type Alias PrefixExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "PrefixExpr",
		Alias: (*Alias)(p),
	})
}

func (f *FieldAccess) MarshalJSON() ([]byte, error) {
	type Alias FieldAccess
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "FieldAccess",
		Alias: (*Alias)(f),
	})
}

func (i *IndexExpr) MarshalJSON() ([]byte, error) {
	type Alias IndexExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "IndexExpr",
		Alias: (*Alias)(i),
	})
}

func (s *SliceExpr) MarshalJSON() ([]byte, error) {
	type Alias SliceExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "SliceExpr",
		Alias: (*Alias)(s),
	})
}

func (i *IfExpr) MarshalJSON() ([]byte, error) {
	type Alias IfExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "IfExpr",
		Alias: (*Alias)(i),
	})
}

func (a *AwaitExpr) MarshalJSON() ([]byte, error) {
	type Alias AwaitExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "AwaitExpr",
		Alias: (*Alias)(a),
	})
}

func (a *AsyncPrologueExpr) MarshalJSON() ([]byte, error) {
	type Alias AsyncPrologueExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "AsyncPrologueExpr",
		Alias: (*Alias)(a),
	})
}

func (m *MatchExpr) MarshalJSON() ([]byte, error) {
	type Alias MatchExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "MatchExpr",
		Alias: (*Alias)(m),
	})
}

// =============================================================================
// Pattern Matching Nodes
// =============================================================================

func (w *WildcardPattern) MarshalJSON() ([]byte, error) {
	type Alias WildcardPattern
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "WildcardPattern",
		Alias: (*Alias)(w),
	})
}

func (l *LiteralPattern) MarshalJSON() ([]byte, error) {
	type Alias LiteralPattern
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "LiteralPattern",
		Alias: (*Alias)(l),
	})
}

func (v *VariantPattern) MarshalJSON() ([]byte, error) {
	type Alias VariantPattern
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "VariantPattern",
		Alias: (*Alias)(v),
	})
}

// =============================================================================
// Type Expressions & Parameterized Types
// =============================================================================

func (n *NamedTypeExpr) MarshalJSON() ([]byte, error) {
	type Alias NamedTypeExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "NamedTypeExpr",
		Alias: (*Alias)(n),
	})
}

func (a *ArrayTypeExpr) MarshalJSON() ([]byte, error) {
	type Alias ArrayTypeExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "ArrayTypeExpr",
		Alias: (*Alias)(a),
	})
}

func (s *SliceTypeExpr) MarshalJSON() ([]byte, error) {
	type Alias SliceTypeExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "SliceTypeExpr",
		Alias: (*Alias)(s),
	})
}

func (s *StructTypeExpr) MarshalJSON() ([]byte, error) {
	type Alias StructTypeExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "StructTypeExpr",
		Alias: (*Alias)(s),
	})
}

func (s *SumTypeExpr) MarshalJSON() ([]byte, error) {
	type Alias SumTypeExpr
	return json.Marshal(&struct {
		Kind string `json:"kind"`
		*Alias
	}{
		Kind:  "SumTypeExpr",
		Alias: (*Alias)(s),
	})
}
