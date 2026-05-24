package ast

import (
	"bytes"
	"encoding/json"
)

// =============================================================================
// Type DTOs (Replaces map-based marshalType)
// =============================================================================

// TypeDTO represents a fully serialized semantic type.
type TypeDTO struct {
	Kind         string           `json:"kind"`
	Size         int              `json:"size,omitempty"`
	Base         *TypeDTO         `json:"base,omitempty"`
	Key          *TypeDTO         `json:"key,omitempty"`
	Value        *TypeDTO         `json:"value,omitempty"`
	Result       *TypeDTO         `json:"result,omitempty"`
	Name         string           `json:"name,omitempty"`
	Fields       []StructFieldDTO `json:"fields,omitempty"`
	TypeArgs     []*TypeDTO       `json:"type_args,omitempty"`
	Variants     []VariantDTO     `json:"variants,omitempty"`
	Discriminant int              `json:"discriminant,omitempty"`
	Return       *TypeDTO         `json:"return,omitempty"`
}

type StructFieldDTO struct {
	Name string   `json:"name"`
	Type *TypeDTO `json:"type"`
}

type VariantDTO struct {
	Name         string           `json:"name"`
	Discriminant int              `json:"discriminant"`
	Fields       []StructFieldDTO `json:"fields,omitempty"`
}

// =============================================================================
// Node DTO (The Polymorphic Wrapper)
// =============================================================================

// NodeDTO is the universal wrapper that injects backend requirements.
type NodeDTO struct {
	// For standard AST Expressions and Statements
	NodeType string `json:"node_type,omitempty"`
	
	// For ast.Pattern matching (the backend expects "kind" instead of "node_type")
	Kind string `json:"kind,omitempty"`

	// Analysis Metadata
	MamlType *TypeDTO `json:"maml_type,omitempty"`
	IsHeap   *bool    `json:"is_heap,omitempty"`

	// Data holds the underlying AST struct or a map of wrapped children.
	// We hide this from default marshalling to flatten it manually.
	Data interface{} `json:"-"`
}

// =============================================================================
// Custom JSON Flattening
// =============================================================================

// MarshalJSON merges the wrapper fields with the dynamically serialized AST node fields.
func (n *NodeDTO) MarshalJSON() ([]byte, error) {
	// 1. Marshal the metadata wrapper structure
	type Meta struct {
		NodeType string   `json:"node_type,omitempty"`
		Kind     string   `json:"kind,omitempty"`
		MamlType *TypeDTO `json:"maml_type,omitempty"`
		IsHeap   *bool    `json:"is_heap,omitempty"`
	}

	metaBytes, err := json.Marshal(Meta{
		NodeType: n.NodeType,
		Kind:     n.Kind,
		MamlType: n.MamlType,
		IsHeap:   n.IsHeap,
	})
	if err != nil {
		return nil, err
	}

	// If there's no underlying data to merge, just return the metadata.
	if n.Data == nil {
		return metaBytes, nil
	}

	// 2. Marshal the underlying AST node fields
	dataBytes, err := json.Marshal(n.Data)
	if err != nil {
		return nil, err
	}

	// If the data serialized to null or an empty object, ignore it.
	if string(dataBytes) == "null" || string(dataBytes) == "{}" {
		return metaBytes, nil
	}

	// 3. Byte Stitching Optimization
	// metaBytes is guaranteed to be a JSON object like {"node_type":"IfExpr"}
	// dataBytes is guaranteed to be a JSON object like {"condition":{...}}
	
	// Strip the trailing '}' from metaBytes
	metaBytes = metaBytes[:len(metaBytes)-1]
	
	// Strip the leading '{' from dataBytes
	dataBytes = dataBytes[1:]

	// Join them with a comma
	var buf bytes.Buffer
	buf.Write(metaBytes)
	
	// Only add the comma if the metadata object wasn't empty (i.e., not just `{`)
	if len(metaBytes) > 1 {
		buf.WriteByte(',')
	}
	
	buf.Write(dataBytes)

	return buf.Bytes(), nil
}