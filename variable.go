package main

type HandleTypeInfo struct {
	baseType      string
	sourcePackage string
}

var HANDLE_MAP map[string]HandleTypeInfo = map[string]HandleTypeInfo{
	"htask": HandleTypeInfo{
		baseType:      "hobject",
		sourcePackage: "__system",
	},
}

type Variable struct {
	typeName      string
	variableName  string
	stackIndex    uint32
	possibleTypes map[string]bool
	refCount      int
}

type Scope struct {
	function                 *FunctionDeclaration
	functionEndOffset        uint32
	variables                []Variable
	localVariableIndexOffset uint32
}

func (s *Scope) GetVariableByStackIndex(stackIndex uint32) *Variable {
	for ii := 0; ii < len(s.variables); ii++ {
		lv := &s.variables[ii]
		if lv.stackIndex == stackIndex {
			return lv
		}
	}
	return nil
}

func HandleIsDerivedFrom(handleType string, baseType string) bool {
	if handleType == baseType {
		return true
	}
	_, ok := HANDLE_MAP[handleType]
	if !ok {
		return false
	}
	return HandleIsDerivedFrom(HANDLE_MAP[handleType].baseType, baseType)
}
