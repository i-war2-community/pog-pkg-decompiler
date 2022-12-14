package decompiler

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
	typeName               string
	variableName           string
	stackIndex             uint32
	hasInit                bool
	assignedTypes          map[string]bool
	referencedTypes        map[string]bool
	parameterAssignedTypes map[string]bool
	handleEqualsTypes      map[string]bool
	potentialNames         []NameProvider
	nameProvider           NameProvider
	refCount               int
	assignmentCount        int
	id                     int
}

func NewVariable(variableName string, typeName string, uniqueId bool) *Variable {
	v := &Variable{
		typeName:               typeName,
		variableName:           variableName,
		stackIndex:             0xFFFFFFFF,
		assignedTypes:          map[string]bool{},
		referencedTypes:        map[string]bool{},
		parameterAssignedTypes: map[string]bool{},
		handleEqualsTypes:      map[string]bool{},
	}
	if uniqueId {
		v.id = VARIABLE_ID_COUNTER
		VARIABLE_ID_COUNTER++
	}
	return v
}

var VARIABLE_ID_COUNTER int = 0

func (v *Variable) AddAssignedType(typeName string) {
	v.assignedTypes[typeName] = true
}

func (v *Variable) AddParameterAssignedType(typeName string) {
	v.parameterAssignedTypes[typeName] = true
}

func (v *Variable) AddReferencedType(typeName string) {
	v.referencedTypes[typeName] = true
}

func (v *Variable) AddHandleEqualsType(typeName string) {
	v.handleEqualsTypes[typeName] = true
}

func (v *Variable) AddNameProvider(provider NameProvider) {
	v.potentialNames = append(v.potentialNames, provider)
}

func (v *Variable) ResetPossibleTypes() {
	v.assignedTypes = map[string]bool{}
	v.referencedTypes = map[string]bool{}
	v.parameterAssignedTypes = map[string]bool{}
	v.handleEqualsTypes = map[string]bool{}
	v.refCount = 0
	v.assignmentCount = 0
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

func (v *Variable) GetParameterAssignedTypes() []string {
	result := []string{}

	for k := range v.parameterAssignedTypes {
		result = append(result, k)
	}

	return result
}

func (v *Variable) GetHandleEqualsTypes() []string {
	result := []string{}

	for k := range v.handleEqualsTypes {
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
		// Find the highest common ancestor among all the types
		highest := handleTypes[0]

		// Loop through the remaining types and make sure we find the highest common ancestor
		for _, h := range handleTypes[1:] {
			highest = HighestCommonAncestorType(highest, h)
			if highest == UNKNOWN_TYPE {
				return UNKNOWN_TYPE
			}
		}

		return highest
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
	detectedType := UNKNOWN_TYPE
	assigned := v.GetAssignedTypes()
	referenced := v.GetReferencedTypes()
	parameterAssigned := v.GetParameterAssignedTypes()
	handleEquals := v.GetHandleEqualsTypes()

	assignedType := getTypeFromAssignedTypes(assigned)
	referencedType := getTypeFromReferencedTypes(referenced)
	parameterAssignedType := getTypeFromAssignedTypes(parameterAssigned)

	if len(handleEquals) > 0 {
		detectedType = handleEquals[0]
	} else {
		if IsHandleType(assignedType) && IsHandleType(referencedType) {
			if referencedType == assignedType {
				detectedType = referencedType
			} else if HandleIsDerivedFrom(assignedType, referencedType) {
				detectedType = assignedType
			} else {
				detectedType = referencedType
				// In order to use the referenced type, ALL our assigned types must derive from it
				for _, atype := range assigned {
					if !HandleIsDerivedFrom(referencedType, atype) {
						detectedType = assignedType
						break
					}
				}
			}

			if parameterAssignedType != UNKNOWN_TYPE {
				detectedType = HighestCommonAncestorType(detectedType, parameterAssignedType)
			}
		} else if len(referenced) == 0 {
			detectedType = assignedType
		} else {
			// Handle the case where a handle is cast to a bool in an if statement
			if referencedType == "bool" && IsHandleType(assignedType) {
				detectedType = assignedType
			} else {
				detectedType = referencedType
			}
		}
	}

	if detectedType != UNKNOWN_TYPE && v.typeName != detectedType {
		//fmt.Printf("%d changed from %s to %s\n", v.id, v.typeName, detectedType)
		v.typeName = detectedType
		return true
	}

	return false
}

func (v *Variable) ResolveName() bool {
	provider := GetHighestPriorityProvider(v, v.potentialNames)
	if provider == nil {
		return false
	}
	name := provider.GetName(v)
	if len(name) > 0 {
		v.nameProvider = provider
		v.variableName = name
		return true
	}

	return false
}

func (v *Variable) ResolveNamingConflict(index int) {
	if v.nameProvider != nil {
		v.variableName = v.nameProvider.ResolveConflict(v, index)
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

func HighestCommonAncestorType(leftType string, rightType string) string {
	for leftIter := leftType; len(leftIter) > 0; leftIter = HANDLE_MAP[leftIter].baseType {
		for rightIter := rightType; len(rightIter) > 0; rightIter = HANDLE_MAP[rightIter].baseType {
			if leftIter == rightIter {
				return leftIter
			}
		}
	}

	return UNKNOWN_TYPE
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
