package decompiler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/iancoleman/strcase"
)

type NameProvider interface {
	GetPriority() int
	GetName(v *Variable) string
	ResolveConflict(v *Variable, index int) string
}

type VariableFilterFunc func(v *Variable) bool

func GetHighestPriorityProvider(v *Variable, providers []NameProvider) NameProvider {
	var highestProvider NameProvider = nil
	highestPriority := 0

	// Find the name with the highest priority
	for _, provider := range providers {
		name := provider.GetName(v)
		if len(name) > 0 {
			priority := provider.GetPriority()
			if priority > highestPriority {
				highestPriority = priority
				highestProvider = provider
			}
		}
	}

	return highestProvider
}

func simpleNameConflictResolution(name string, index int) string {
	return fmt.Sprintf("%s_%d", name, index)
}

type ConstantNameProvider struct{}

func (np *ConstantNameProvider) GetPriority() int {
	return 10
}

func (np *ConstantNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *ConstantNameProvider) GetName(v *Variable) string {
	switch v.typeName {
	case "int", "float":
		if v.assignmentCount == 1 {
			return "constant"
		}
	}

	return ""
}

type EnumTypeNameProvider struct{}

func (np *EnumTypeNameProvider) GetPriority() int {
	return 100
}

func (np *EnumTypeNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *EnumTypeNameProvider) GetName(v *Variable) string {
	if IsEnumType(v.typeName) {
		return ConvertToIdentifier(strings.TrimPrefix(v.typeName, "e"))
	}

	return ""
}

type CollectionTypeNameProvider struct{}

func (np *CollectionTypeNameProvider) GetPriority() int {
	return 10
}

func (np *CollectionTypeNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *CollectionTypeNameProvider) GetName(v *Variable) string {
	if IsCollectionType(v.typeName) {
		return fmt.Sprintf("local%s", strcase.ToCamel(v.typeName))
	}

	return ""
}

type GlobalNameProvider struct {
	funcCall *OpGraph
}

func (np *GlobalNameProvider) GetPriority() int {
	return 100
}

func (np *GlobalNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *GlobalNameProvider) GetName(v *Variable) string {
	// Look for the Global call while allowing some other calls along the way
	for node := np.funcCall; node.operation.IsFunctionCall() && len(node.children) > 0; node = node.children[0] {
		fd := node.operation.GetFunctionDeclaration()
		if fd.pkg == "Global" {
			switch fd.name {
			case "Int", "Float", "Bool", "String", "Handle", "List", "Set":
				if node.children[0].operation.opcode == OP_UNKNOWN_3B && node.children[0].children[0].operation.opcode == OP_LITERAL_STRING {
					value := node.children[0].children[0].operation.data.(LiteralStringData).String()
					// Strip the quotes
					value = value[1 : len(value)-1]

					return ConvertToIdentifier(value)
				}
			}
		}
	}
	return ""
}

type funcRegexp struct {
	pkg regexp.Regexp
	fnc regexp.Regexp
}

func newFuncRegexp(pkgExp, fncExp string) *funcRegexp {
	return &funcRegexp{
		pkg: *regexp.MustCompile(pkgExp),
		fnc: *regexp.MustCompile(fncExp),
	}
}

func (fr funcRegexp) IsMatch(fd *FunctionDeclaration) bool {
	return fr.pkg.MatchString(fd.pkg) && fr.fnc.MatchString(fd.name)
}

func allowedAssignmentOperation(op *OpGraph) bool {
	return op.operation.IsFunctionCall() || op.operation.opcode == OP_UNKNOWN_3B || op.operation.IsCast()
}

func opCallsFunction(op *OpGraph, targetFunc, nestedFuncs *funcRegexp) *FunctionDeclaration {
	for node := op; allowedAssignmentOperation(node); node = node.children[len(node.children)-1] {
		if node.operation.opcode == OP_UNKNOWN_3B || node.operation.IsCast() {
			continue
		}
		fd := node.operation.GetFunctionDeclaration()
		// Check if the function matches
		if targetFunc.IsMatch(fd) {
			return fd
		} else if !nestedFuncs.IsMatch(fd) || len(node.children) == 0 {
			break
		}
	}
	return nil
}

func getNameFromFunctionChainParameter(op *OpGraph, targetFuncChain []*funcRegexp, optional *funcRegexp, targetParam *regexp.Regexp) (string, *FunctionDeclaration) {
	chainIdx := 0
	for node := op; allowedAssignmentOperation(node) && len(node.children) > 0; node = node.children[len(node.children)-1] {
		if node.operation.opcode == OP_UNKNOWN_3B || node.operation.IsCast() {
			continue
		}
		fd := node.operation.GetFunctionDeclaration()

		// Check if the function matches
		if targetFuncChain[chainIdx].IsMatch(fd) {

			if chainIdx == len(targetFuncChain)-1 {
				// Find the target parameter
				parameterIdx, _ := fd.FindParameter(targetParam)

				// Find the corresponding operation
				nameOpGraph := node.GetFunctionParameterChild(parameterIdx)

				// If it is a string literal, convert it to a valid identifier
				if nameOpGraph != nil && nameOpGraph.operation.opcode == OP_UNKNOWN_3B && nameOpGraph.children[0].operation.opcode == OP_LITERAL_STRING {
					value := STRING_TABLE[nameOpGraph.children[0].operation.data.(LiteralStringData).index]
					return ConvertToIdentifier(value), fd
				}
				return "", nil
			}
			chainIdx++
		} else if chainIdx != 0 {
			return "", nil
		} else if !optional.IsMatch(fd) {
			// If this doesn't match our allowed nested functions we are done
			break
		}
	}
	return "", nil
}

func getNameFromFunctionParameter(op *OpGraph, targetFunc, nestedFuncs *funcRegexp, targetParam *regexp.Regexp) (string, *FunctionDeclaration) {
	for node := op; allowedAssignmentOperation(node) && len(node.children) > 0; node = node.children[len(node.children)-1] {
		if node.operation.opcode == OP_UNKNOWN_3B || node.operation.IsCast() {
			continue
		}
		fd := node.operation.GetFunctionDeclaration()
		// Check if the function matches
		if targetFunc.IsMatch(fd) {

			// Find the target parameter
			parameterIdx, _ := fd.FindParameter(targetParam)

			// Find the corresponding operation
			nameOpGraph := node.GetFunctionParameterChild(parameterIdx)

			// If it is a string literal, convert it to a valid identifier
			if nameOpGraph != nil && nameOpGraph.operation.opcode == OP_UNKNOWN_3B && nameOpGraph.children[0].operation.opcode == OP_LITERAL_STRING {
				value := STRING_TABLE[nameOpGraph.children[0].operation.data.(LiteralStringData).index]
				return ConvertToIdentifier(value), fd
			}
		} else if !nestedFuncs.IsMatch(fd) {
			// If this doesn't match our allowed nested functions we are done
			break
		}
	}
	return "", nil
}

type SingleFunctionVariableNameFunc func(v *Variable, fd *FunctionDeclaration) string

type SingleFunctionProvider struct {
	funcCall     *OpGraph
	variableName SingleFunctionVariableNameFunc
	function     *funcRegexp
	nested       *funcRegexp
	filter       VariableFilterFunc
	priority     int
}

func (np *SingleFunctionProvider) GetPriority() int {
	if np.priority == 0 {
		return 100
	}
	return np.priority
}

func (np *SingleFunctionProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *SingleFunctionProvider) GetName(v *Variable) string {
	if np.filter != nil && !np.filter(v) {
		return ""
	}

	nested := np.nested
	if nested == nil {
		nested = newFuncRegexp(`.*`, `Cast`)
	}

	fd := opCallsFunction(np.funcCall, np.function, nested)

	if fd != nil {
		return np.variableName(v, fd)
	}

	return ""
}

type FunctionParameterVariableNameFunc func(v *Variable, parameterValue string, fd *FunctionDeclaration) string

type FunctionParameterProvider struct {
	funcCall     *OpGraph
	function     *funcRegexp
	nested       *funcRegexp
	parameter    *regexp.Regexp
	filter       VariableFilterFunc
	variableName FunctionParameterVariableNameFunc
	priority     int
}

func (np *FunctionParameterProvider) GetPriority() int {
	if np.priority == 0 {
		return 100
	}
	return np.priority
}

func (np *FunctionParameterProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *FunctionParameterProvider) GetName(v *Variable) string {
	if np.filter != nil && !np.filter(v) {
		return ""
	}

	nested := np.nested
	if nested == nil {
		nested = newFuncRegexp(`.*`, `Cast`)
	}

	parameter := np.parameter
	if parameter == nil {
		parameter = regexp.MustCompile(`name`)
	}

	name, fd := getNameFromFunctionParameter(np.funcCall, np.function, nested, parameter)

	if len(name) == 0 {
		return ""
	}

	if np.variableName != nil {
		return np.variableName(v, name, fd)
	}
	return name
}

type FunctionChainParameterProvider struct {
	funcCall      *OpGraph
	functionChain []*funcRegexp
	nested        *funcRegexp
	parameter     *regexp.Regexp
	filter        VariableFilterFunc
	variableName  FunctionParameterVariableNameFunc
	priority      int
}

func (np *FunctionChainParameterProvider) GetPriority() int {
	if np.priority == 0 {
		return 100
	}
	return np.priority
}

func (np *FunctionChainParameterProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *FunctionChainParameterProvider) GetName(v *Variable) string {
	if np.filter != nil && !np.filter(v) {
		return ""
	}

	nested := np.nested
	if nested == nil {
		nested = newFuncRegexp(`.*`, `Cast`)
	}

	parameter := np.parameter
	if parameter == nil {
		parameter = regexp.MustCompile(`name`)
	}

	name, fd := getNameFromFunctionChainParameter(np.funcCall, np.functionChain, nested, parameter)

	if len(name) == 0 {
		return ""
	}

	if np.variableName != nil {
		return np.variableName(v, name, fd)
	}
	return name
}

var EXCLUDED_HANDLE_TYPES = map[string]bool{
	"task":   true,
	"sim":    true,
	"isim":   true,
	"object": true,
}

var OVERRIDE_HANDLE_TYPES = map[string]string{
	"task": "taskHandle",
}

type HandleTypeNameProvider struct {
	handleType string
}

func (np *HandleTypeNameProvider) GetPriority() int {
	return 10
}

func (np *HandleTypeNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *HandleTypeNameProvider) GetName(v *Variable) string {
	handleType := strings.TrimPrefix(np.handleType, "h")

	if _, ok := EXCLUDED_HANDLE_TYPES[handleType]; !ok {
		if override, ok := OVERRIDE_HANDLE_TYPES[handleType]; ok {
			return override
		}
		return handleType
	}
	return ""
}

var ITERATOR_NAMES = []string{
	"ii",
	"jj",
	"kk",
	"mm",
	"oo",
}

type IteratorNameProvider struct {
}

func (np *IteratorNameProvider) GetPriority() int {
	return 1000
}

func (np *IteratorNameProvider) ResolveConflict(v *Variable, index int) string {
	nameCount := len(ITERATOR_NAMES)
	nameIdx := index % nameCount
	nameSuffix := index / nameCount

	newName := ITERATOR_NAMES[nameIdx]

	if nameSuffix > 0 {
		newName = fmt.Sprintf("%s_%d", newName, nameSuffix)
	}
	return newName
}

func (np *IteratorNameProvider) GetName(v *Variable) string {
	return ITERATOR_NAMES[0]
}

func AddAssignmentBasedNamingProviders(v *Variable, assignment *OpGraph) {
	// Constant provider
	v.AddNameProvider(&ConstantNameProvider{})
	// Global provider
	v.AddNameProvider(&GlobalNameProvider{
		funcCall: assignment,
	})
	// Find provider
	v.AddNameProvider(&FunctionParameterProvider{
		funcCall: assignment,
		function: newFuncRegexp(`.*`, `Find`),
	})
	// Create Ship provider
	v.AddNameProvider(&FunctionParameterProvider{
		funcCall:  assignment,
		function:  newFuncRegexp(`iShip`, `Create`),
		parameter: regexp.MustCompile(`template`),
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("ship%s", strcase.ToCamel(parameterValue))
		},
		priority: 1000,
	})
	// Create Sim provider
	v.AddNameProvider(&FunctionParameterProvider{
		funcCall:  assignment,
		function:  newFuncRegexp(`Sim`, `Create`),
		parameter: regexp.MustCompile(`template`),
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("sim%s", strcase.ToCamel(parameterValue))
		},
		priority: 1000,
	})
	// Player Ship provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "playerShip" },
		function:     newFuncRegexp("iShip", "FindPlayerShip"),
		priority:     1000,
	})
	// Distance provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "distance" },
		function:     newFuncRegexp(`.*`, `.*Distance.*`),
	})
	// Count provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return ConvertToIdentifier(fd.name) },
		function:     newFuncRegexp(`.*`, `.*Count[^a-z]?.*`),
	})
	// Name provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "name" },
		function:     newFuncRegexp(`.*`, `.*Name[^a-z]?.*`),
		filter:       func(v *Variable) bool { return v.typeName == "string" },
	})
	// Object Property provider
	v.AddNameProvider(&FunctionParameterProvider{
		funcCall:  assignment,
		function:  newFuncRegexp(`Object`, `.*Property`),
		parameter: regexp.MustCompile(`property`),
		priority:  200,
	})
	// Task State provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`State`, `Find`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "taskState" },
		priority:     300,
	})
	// Screen Class provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`GUI`, `CurrentScreenClassname`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "screenClass" },
		priority:     300,
	})
	// Group Leader provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`Group`, `Leader`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "groupLeader" },
		priority:     300,
	})
	// Waypoint provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`.*`, `.*Waypoint[^a-z]?.*`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "waypoint" },
		priority:     50,
	})
	// Named Waypoint provider
	v.AddNameProvider(&FunctionChainParameterProvider{
		funcCall: assignment,
		functionChain: []*funcRegexp{
			newFuncRegexp(`.*`, `CreateWaypointRelativeTo|WaypointForEntity`),
			newFuncRegexp(`iMapEntity`, `FindByName`),
		},
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("waypoint%s", strcase.ToCamel(parameterValue))
		},
		priority: 1000,
	})
	// Group Iter provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`Group`, `NthSim`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "groupIter" },
		priority:     50,
	})
	// Lagrange Points provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`iMapEntity`, `SystemLagrangePoints`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "lagrangePoints" },
		priority:     100,
	})
	// Random provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`Math`, `Random.*`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "random" },
		priority:     100,
	})
	// Target provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`.*`, `.*Target[^a-z]?.*`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return ConvertToIdentifier(fd.name) },
		filter:       func(v *Variable) bool { return IsHandleType(v.typeName) },
		priority:     50,
	})
	// Conversation Ask provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`iConversation`, `Ask`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "convoResponse" },
		filter:       func(v *Variable) bool { return v.typeName == "int" || IsEnumType(v.typeName) },
		priority:     50,
	})
	// Current Task provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall:     assignment,
		function:     newFuncRegexp(`Task`, `Current`),
		nested:       newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "currentTask" },
		priority:     100,
	})
	// Cast provider
	v.AddNameProvider(&SingleFunctionProvider{
		funcCall: assignment,
		function: newFuncRegexp(`.*`, `Cast`),
		nested:   newFuncRegexp(`none`, `none`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string {
			pkg := strings.TrimPrefix(fd.pkg, "i")

			if EXCLUDED_HANDLE_TYPES[strings.ToLower(pkg)] {
				return ""
			}

			return ConvertToIdentifier(pkg)
		},
		priority: 50,
	})
}

func AddParameterPassingBasedNamingProviders(v *Variable, funcCall *OpGraph) {
	fd := funcCall.operation.GetFunctionDeclaration()

	if fd == nil {
		return
	}

	var varParameter *FunctionParameter = nil

	for idx := 0; idx < len(funcCall.children); idx++ {
		if funcCall.children[idx].operation.GetVariableStackIndex() == v.stackIndex {
			varParameter = &(*fd.parameters)[len(funcCall.children)-1-idx]
		}
	}

	// Only use this one if the variable was passed in as the property
	if varParameter.parameterName == "property" {
		// Object Property provider
		v.AddNameProvider(&FunctionParameterProvider{
			funcCall:  funcCall,
			function:  newFuncRegexp(`Object`, `Add.*Property`),
			nested:    newFuncRegexp(`none`, `none`),
			parameter: regexp.MustCompile(`property`),
			variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {

				return parameterValue
			},
			priority: 1000,
		})
	}
}
