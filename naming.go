package main

import (
	"fmt"
	"regexp"
	"strings"
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

type GlobalNameProvider struct {
	assignment *OpGraph
}

func (np *GlobalNameProvider) GetPriority() int {
	return 100
}

func (np *GlobalNameProvider) ResolveConflict(v *Variable, index int) string {
	return simpleNameConflictResolution(v.variableName, index)
}

func (np *GlobalNameProvider) GetName(v *Variable) string {
	// Look for the Global call while allowing some other calls along the way
	for node := np.assignment; node.operation.IsFunctionCall() && len(node.children) > 0; node = node.children[0] {
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
	assignment   *OpGraph
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

	fd := opCallsFunction(np.assignment, np.function, nested)

	if fd != nil {
		return np.variableName(v, fd)
	}

	return ""
}

type FunctionParameterVariableNameFunc func(v *Variable, parameterValue string, fd *FunctionDeclaration) string

type FunctionParameterProvider struct {
	assignment   *OpGraph
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

	name, fd := getNameFromFunctionParameter(np.assignment, np.function, nested, parameter)

	if len(name) == 0 {
		return ""
	}

	if np.variableName != nil {
		return np.variableName(v, name, fd)
	}
	return name
}

type FunctionChainParameterProvider struct {
	assignment    *OpGraph
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

	name, fd := getNameFromFunctionChainParameter(np.assignment, np.functionChain, nested, parameter)

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
	handleType := np.handleType
	if strings.HasPrefix(np.handleType, "h") {
		handleType = np.handleType[1:]
	}

	if _, ok := EXCLUDED_HANDLE_TYPES[handleType]; !ok {
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
	// Global provider
	v.AddNameProvider(&GlobalNameProvider{
		assignment: assignment,
	})
	// Find provider
	v.AddNameProvider(&FunctionParameterProvider{
		assignment: assignment,
		function:   newFuncRegexp(`.*`, `Find`),
	})
	// Create Ship provider
	v.AddNameProvider(&FunctionParameterProvider{
		assignment: assignment,
		function:   newFuncRegexp(`iShip`, `Create`),
		parameter:  regexp.MustCompile(`template`),
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("ship_%s", parameterValue)
		},
		priority: 1000,
	})
	// Create Sim provider
	v.AddNameProvider(&FunctionParameterProvider{
		assignment: assignment,
		function:   newFuncRegexp(`Sim`, `Create`),
		parameter:  regexp.MustCompile(`template`),
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("sim_%s", parameterValue)
		},
		priority: 1000,
	})
	// Player Ship provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "playerShip" },
		function:     newFuncRegexp("iShip", "FindPlayerShip"),
		priority:     1000,
	})
	// Distance provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "distance" },
		function:     newFuncRegexp(`.*`, `.*Distance.*`),
	})
	// Count provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return ConvertToIdentifier(fd.name) },
		function:     newFuncRegexp(`.*`, `.*Count[^a-z]?.*`),
	})
	// Name provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "name" },
		function:     newFuncRegexp(`.*`, `.*Name[^a-z]?.*`),
		filter:       func(v *Variable) bool { return v.typeName == "string" },
	})
	// Object Property provider
	v.AddNameProvider(&FunctionParameterProvider{
		assignment: assignment,
		function:   newFuncRegexp(`Object`, `.*Property`),
		parameter:  regexp.MustCompile(`property`),
		priority:   200,
	})
	// Task State provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`State`, `Find`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "taskState" },
		priority:     300,
	})
	// Screen Class provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`GUI`, `CurrentScreenClassname`),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "screenClass" },
		priority:     300,
	})
	// Waypoint provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`.*`, `.*Waypoint[^a-z]?.*`),
		nested:       newFuncRegexp(``, ``),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "waypoint" },
		priority:     50,
	})
	// Named Waypoint provider
	v.AddNameProvider(&FunctionChainParameterProvider{
		assignment: assignment,
		functionChain: []*funcRegexp{
			newFuncRegexp(`.*`, `CreateWaypointRelativeTo|WaypointForEntity`),
			newFuncRegexp(`iMapEntity`, `FindByName`),
		},
		variableName: func(v *Variable, parameterValue string, fd *FunctionDeclaration) string {
			return fmt.Sprintf("waypoint_%s", parameterValue)
		},
		priority: 1000,
	})
	// Group Iter provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`Group`, `NthSim`),
		nested:       newFuncRegexp(``, ``),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "groupIter" },
		priority:     50,
	})
	// Lagrange Points provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`iMapEntity`, `SystemLagrangePoints`),
		nested:       newFuncRegexp(``, ``),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "lagrangePoints" },
		priority:     100,
	})
	// Random provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`Math`, `Random.*`),
		nested:       newFuncRegexp(``, ``),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return "random" },
		priority:     100,
	})
	// Target provider
	v.AddNameProvider(&SingleFunctionProvider{
		assignment:   assignment,
		function:     newFuncRegexp(`.*`, `.*Target[^a-z]?.*`),
		nested:       newFuncRegexp(``, ``),
		variableName: func(v *Variable, fd *FunctionDeclaration) string { return ConvertToIdentifier(fd.name) },
		filter:       func(v *Variable) bool { return IsHandleType(v.typeName) },
		priority:     50,
	})
}
