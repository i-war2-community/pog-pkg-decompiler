package main

type Variable struct {
	typeName     string
	variableName string
	stackIndex   uint32
}

func GetVariableByStackIndex(variables []Variable, stackIndex uint32) *Variable {
	for ii := 0; ii < len(variables); ii++ {
		lv := &variables[ii]
		if lv.stackIndex == stackIndex {
			return lv
		}
	}
	return nil
}
