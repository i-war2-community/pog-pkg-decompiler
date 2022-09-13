package main

import (
	"fmt"
	"strings"
)

type HandleTypeInfo struct {
	baseType      string
	sourcePackage string
}

var HANDLE_MAP map[string]HandleTypeInfo = map[string]HandleTypeInfo{
	"htask": {
		baseType:      "hobject",
		sourcePackage: SYSTEM_PACKAGE,
	},
	"hobject": {
		baseType:      "",
		sourcePackage: SYSTEM_PACKAGE,
	},
	"list": {
		baseType:      "",
		sourcePackage: "List",
	},
}

func IsHandleType(typeName string) bool {
	_, ok := HANDLE_MAP[typeName]
	return ok
}

type Variable struct {
	typeName        string
	variableName    string
	stackIndex      uint32
	assignedTypes   map[string]bool
	referencedTypes map[string]bool
	refCount        int
	id              int
}

var VARIABLE_ID_COUNTER int = 0

func (v *Variable) AddAssignedType(typeName string) {
	v.assignedTypes[typeName] = true
}

func (v *Variable) AddReferencedType(typeName string) {
	v.referencedTypes[typeName] = true
}

func (v *Variable) ResetPossibleTypes() {
	v.assignedTypes = map[string]bool{}
	v.referencedTypes = map[string]bool{}
	v.refCount = 0
}

func (v *Variable) GetPossibleTypes() map[string]bool {
	result := map[string]bool{}

	for k, v := range v.assignedTypes {
		result[k] = v
	}

	for k, v := range v.referencedTypes {
		result[k] = v
	}

	return result
}

func (v *Variable) GetAssignedTypes() []string {
	result := []string{}

	for k := range v.assignedTypes {
		result = append(result, k)
	}

	return result
}

func (v *Variable) GetReferencedTypes() []string {
	result := []string{}

	for k := range v.referencedTypes {
		result = append(result, k)
	}

	return result
}

type EnumTypeInfo struct {
	nameToValue map[string]uint32
	valueToName map[uint32]string
}

var ENUM_MAP map[string]EnumTypeInfo = map[string]EnumTypeInfo{}

func IsEnumType(typeName string) bool {
	_, ok := ENUM_MAP[typeName]
	return ok
}

type Scope struct {
	function                 *FunctionDeclaration
	functionEndOffset        uint32
	variables                []*Variable
	localVariableIndexOffset uint32
}

func (s *Scope) GetVariableByStackIndex(stackIndex uint32) *Variable {
	for ii := 0; ii < len(s.variables); ii++ {
		lv := s.variables[ii]
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

func GetCastFunctionForHandleType(handleType string) string {
	hdata, ok := HANDLE_MAP[handleType]
	if !ok {
		fmt.Printf("ERROR: Failed to get cast function for handle type %s: handle type not found.\n", handleType)
		return UNKNOWN_TYPE
	}
	packageData, ok := PACKAGES[strings.ToLower(hdata.sourcePackage)]
	if !ok {
		fmt.Printf("ERROR: Failed to get cast function for handle type %s: source package %s not found.\n", handleType, hdata.sourcePackage)
		return UNKNOWN_TYPE
	}

	for _, fnc := range packageData.functions {
		if fnc.name == "Cast" {
			return fnc.GetScopedName()
		}
	}

	fmt.Printf("ERROR: Failed to get cast function for handle type %s: \"Cast\" function not found in source package %s.\n", handleType, hdata.sourcePackage)
	return UNKNOWN_TYPE
}
