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
}

func IsHandleType(typeName string) bool {
	_, ok := HANDLE_MAP[typeName]
	return ok
}

var COLLECTION_MAP = map[string]bool{
	"set":  true,
	"list": true,
}

func IsCollectionType(typeName string) bool {
	_, ok := COLLECTION_MAP[typeName]
	return ok
}

type Variable struct {
	typeName        string
	variableName    string
	stackIndex      uint32
	hasInit         bool
	assignedTypes   map[string]bool
	referencedTypes map[string]bool
	potentialNames  []NameProvider
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

func getHandleTypes(types []string) []string {
	result := []string{}
	for _, t := range types {
		if IsHandleType(t) {
			result = append(result, t)
		}
	}
	return result
}

func getEnumType(types []string) string {
	enumTypeCount := 0
	var enumType string

	for _, typeName := range types {
		if IsEnumType(typeName) {
			enumTypeCount++
			enumType = typeName
		}
	}

	if enumTypeCount == 1 {
		return enumType
	}

	return UNKNOWN_TYPE
}

func getCollectionType(types []string) string {
	collectionTypeCount := 0
	var collectionType string

	for _, typeName := range types {
		if IsCollectionType(typeName) {
			collectionTypeCount++
			collectionType = typeName
		}
	}

	if collectionTypeCount == 1 {
		return collectionType
	}

	return UNKNOWN_TYPE
}

func getBestNonHandleType(types []string) string {
	hasBool := false
	hasInt := false
	hasFloat := false
	hasString := false

	// Check to see if we are an enum
	enumType := getEnumType(types)
	if enumType != UNKNOWN_TYPE {
		return enumType
	}

	// Check to see if we are a collection
	collectionType := getCollectionType(types)
	if collectionType != UNKNOWN_TYPE {
		return collectionType
	}

	for _, t := range types {
		switch t {
		case "bool":
			hasBool = true
		case "int":
			hasInt = true
		case "float":
			hasFloat = true
		case "string":
			hasString = true
		}
	}

	if hasString {
		return "string"
	}
	if hasFloat {
		return "float"
	}
	if hasInt {
		return "int"
	}
	if hasBool {
		return "bool"
	}

	return UNKNOWN_TYPE
}

func getTypeFromAssignedTypes(assigned []string) string {
	handleTypes := getHandleTypes(assigned)

	if len(handleTypes) > 0 {
		return "hobject"
	}

	return getBestNonHandleType(assigned)
}

func getTypeFromReferencedTypes(referenced []string) string {
	handleTypes := getHandleTypes(referenced)

	if len(handleTypes) > 0 {
		// Find the highest referenced type
		highestType := UNKNOWN_TYPE
		for _, handle := range handleTypes {

			if highestType == UNKNOWN_TYPE {
				highestType = handle
			}

			if HandleIsDerivedFrom(highestType, handle) {
				continue
			}
			if HandleIsDerivedFrom(handle, highestType) {
				highestType = handle
				continue
			}
			highestType = UNKNOWN_TYPE
			break
		}
		return highestType
	}
	return getBestNonHandleType(referenced)
}

func (v *Variable) ResolveType() bool {
	// if v.typeName != UNKNOWN_TYPE {
	// 	return false
	// }
	detectedType := UNKNOWN_TYPE
	assigned := v.GetAssignedTypes()
	referenced := v.GetReferencedTypes()

	// If we have no referenced types, we must be what was assigned to us
	if len(referenced) == 0 {
		detectedType = getTypeFromAssignedTypes(assigned)
	} else {
		detectedType = getTypeFromReferencedTypes(referenced)
	}

	if detectedType != UNKNOWN_TYPE && v.typeName != detectedType {
		//fmt.Printf("%d changed from %s to %s\n", v.id, v.typeName, detectedType)
		v.typeName = detectedType
		return true
	}

	return false
}

func (v *Variable) ResolveName() {
	resolved := ResolveToName(v.potentialNames)
	if len(resolved) > 0 {
		v.variableName = resolved
	}
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
