package types

func (t IntType) MarshalJSON() ([]byte, error) {
	return []byte(`"IntType"`), nil
}

func (t BoolType) MarshalJSON() ([]byte, error) {
	return []byte(`"BoolType"`), nil
}

func (t UnitType) MarshalJSON() ([]byte, error) {
	return []byte(`"UnitType"`), nil
}

func (t StringType) MarshalJSON() ([]byte, error) {
	return []byte(`"StringType"`), nil
}