package sema

import "strings"

type Type interface {
	String() string
	Equals(Type) bool
}

type IntType struct{}

func (t IntType) String() string         { return "int" }
func (t IntType) Equals(other Type) bool { _, ok := other.(IntType); return ok }

type BoolType struct{}

func (t BoolType) String() string         { return "bool" }
func (t BoolType) Equals(other Type) bool { _, ok := other.(BoolType); return ok }

type StringType struct{}

func (t StringType) String() string         { return "string" }
func (t StringType) Equals(other Type) bool { _, ok := other.(StringType); return ok }

type StructType struct {
	Name   string
	Fields []StructField
}
type StructField struct {
	Name string
	Type Type
}

func (t *StructType) String() string { return t.Name }
func (t *StructType) Equals(other Type) bool {
	o, ok := other.(*StructType)
	return ok && t.Name == o.Name
}
func (t *StructType) GetFieldIndex(name string) int {
	for i, f := range t.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

type FunctionType struct {
	Params []Type
	Return Type
}

func (t *FunctionType) String() string {
	var params []string
	for _, p := range t.Params {
		params = append(params, p.String())
	}
	return "fn(" + strings.Join(params, ", ") + ") " + t.Return.String()
}
func (t *FunctionType) Equals(other Type) bool {
	o, ok := other.(*FunctionType)
	if !ok || len(t.Params) != len(o.Params) || !t.Return.Equals(o.Return) {
		return false
	}
	return true
}

type UnknownType struct{}

func (t UnknownType) String() string         { return "unknown" }
func (t UnknownType) Equals(other Type) bool { _, ok := other.(UnknownType); return ok }
