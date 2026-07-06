package types


var (
	// genericCache stores instantiated generic types mapped by their MangledName.
	genericCache = make(map[string]Type)
)

// GetOrInstantiateOption fetches an existing Option<T> or builds and caches a new one.
func GetOrInstantiateOption(base Type) *SumType {
	mangled := "Option_" + base.MangledName()

	if cached, exists := genericCache[mangled]; exists {
		return cached.(*SumType)
	}

	newOpt := NewOptionType(base)
	genericCache[mangled] = newOpt
	return newOpt
}

// GetOrInstantiateResult fetches an existing Result<T, E> or builds and caches a new one.
func GetOrInstantiateResult(value Type, err Type) *SumType {
	mangled := "Result_" + value.MangledName() + "_" + err.MangledName()

	if cached, exists := genericCache[mangled]; exists {
		return cached.(*SumType)
	}

	newRes := NewResultType(value, err)
	genericCache[mangled] = newRes
	return newRes
}

// GetOrInstantiateVec fetches an existing Vec<T> or builds and caches a new one.
func GetOrInstantiateVec(base Type) *VectorType {
	mangled := "Vec_" + base.MangledName()

	if cached, exists := genericCache[mangled]; exists {
		return cached.(*VectorType)
	}

	newVec := &VectorType{Base: base}
	genericCache[mangled] = newVec
	return newVec
}

// GetOrInstantiateMap fetches an existing Map<K, V> or builds and caches a new one.
func GetOrInstantiateMap(key Type, value Type) *MapType {
	mangled := "Map_" + key.MangledName() + "_" + value.MangledName()

	if cached, exists := genericCache[mangled]; exists {
		return cached.(*MapType)
	}

	newMap := &MapType{Key: key, Value: value}
	genericCache[mangled] = newMap
	return newMap
}

// --- OPTION ---
func NewOptionType(base Type) *SumType {
	return &SumType{
		BaseName: "Option",
		TypeArgs: []Type{base},
		Variants: []SumVariant{
			{Name: "Some", Discriminant: 0, TupleTypes: []Type{base}},
			{Name: "None", Discriminant: 1, TupleTypes: []Type{}},
		},
	}
}

// --- RESULT ---
func NewResultType(value Type, err Type) *SumType {
	return &SumType{
		BaseName: "Result",
		TypeArgs: []Type{value, err},
		Variants: []SumVariant{
			{Name: "Ok", Discriminant: 0, TupleTypes: []Type{value}},
			{Name: "Err", Discriminant: 1, TupleTypes: []Type{err}},
		},
	}
}
