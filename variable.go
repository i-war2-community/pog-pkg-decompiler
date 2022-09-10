package main

type Variable struct {
	typeName      string
	variableName  string
	stackIndex    uint32
	possibleTypes map[string]bool
	setCount      int
}

type Scope struct {
	function                 *FunctionDeclaration
	functionEndOffset        uint32
	variables                []Variable
	localVariableIndexOffset uint32
}

func (s Scope) GetVariableByStackIndex(stackIndex uint32) *Variable {
	for ii := 0; ii < len(s.variables); ii++ {
		lv := &s.variables[ii]
		if lv.stackIndex == stackIndex {
			return lv
		}
	}
	return nil
}
