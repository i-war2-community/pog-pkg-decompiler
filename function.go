package main

import (
	"fmt"
	"os"
	"strings"
)

const PROTOTYPE_PREFIX = "prototype"

type FunctionParameter struct {
	typeName       string
	parameterName  string
	potentialTypes map[string]bool
	id             int
}

type FunctionDeclaration struct {
	pkg                 string
	name                string
	returnTypeName      string
	parameters          *[]FunctionParameter
	possibleReturnTypes map[string]bool
	autoDetectTypes     bool
}

func (fd *FunctionDeclaration) ResetPossibleTypes() {
	fd.possibleReturnTypes = map[string]bool{}
	if fd.parameters != nil {
		params := *fd.parameters
		for ii := range params {
			params[ii].potentialTypes = map[string]bool{}
		}
	}
}

var FUNC_DECLARATIONS map[string]*FunctionDeclaration = map[string]*FunctionDeclaration{}

type FunctionDefinition struct {
	declaration *FunctionDeclaration
	scope       *Scope
	body        []BlockElement

	startingIndex int
	endingIndex   int
	initialOffset int64
}

func findBaseTypeForAssignedTypes(assignedTypes []string) string {
	baseType := UNKNOWN_TYPE
	if len(assignedTypes) > 0 {
		for idx := 0; idx < len(assignedTypes); idx++ {
			assigned := assignedTypes[idx]

			if !IsHandleType(assigned) {
				continue
			}

			if baseType == UNKNOWN_TYPE {
				baseType = assigned
				continue
			}

			if HandleIsDerivedFrom(assigned, baseType) {
				continue
			}
			if HandleIsDerivedFrom(baseType, assigned) {
				baseType = assigned
				continue
			}
			common := UNKNOWN_TYPE
			for typeA := baseType; len(typeA) > 0; typeA = HANDLE_MAP[typeA].baseType {
				for typeB := assigned; len(typeB) > 0; typeB = HANDLE_MAP[typeB].baseType {
					if typeA == typeB {
						common = typeA
						break
					}
				}
				if common != UNKNOWN_TYPE {
					break
				}
			}
			if common != UNKNOWN_TYPE {
				baseType = common
				continue
			}
			break
		}
		return baseType
	}

	return baseType
}

func findBaseTypeForReferencedTypes(referencedTypes []string, startingType string) string {
	if len(referencedTypes) > 0 {
		baseType := startingType

		for idx := 0; idx < len(referencedTypes); idx++ {
			referenced := referencedTypes[idx]

			if !IsHandleType(referenced) {
				continue
			}

			if baseType == UNKNOWN_TYPE {
				baseType = "hobject"
			}

			if HandleIsDerivedFrom(baseType, referenced) {
				continue
			}
			if HandleIsDerivedFrom(referenced, baseType) {
				baseType = referenced
				continue
			}
			baseType = UNKNOWN_TYPE
			break
		}
		return baseType
	}

	return startingType
}

func pickBestNonHandleType(possibleTypes map[string]bool) string {
	_, okInt := possibleTypes["int"]
	_, okFloat := possibleTypes["float"]
	_, okBool := possibleTypes["bool"]
	if okInt && okBool {
		return "int"
	}
	if okInt && okFloat {
		return "float"
	}
	if okBool && okFloat {
		return "float"
	}
	// TODO: Maybe remove this when we add enum support
	if okInt {
		return "int"
	}

	return UNKNOWN_TYPE
}

func (fd *FunctionDefinition) ResetPossibleTypes() {
	fd.declaration.ResetPossibleTypes()
	for _, v := range fd.scope.variables {
		v.ResetPossibleTypes()
	}
}

func (fd *FunctionDefinition) ResolveBodyTypes() {
	// Resolve variable types
	ResolveTypes(fd.scope, fd.body)
}

func getEnumType(possibleTypes map[string]bool) string {
	// Check for enum type
	enumTypeCount := 0
	var enumType string

	for typeName, _ := range possibleTypes {
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

func (fd *FunctionDefinition) ResolveHeaderTypes() int {
	resolvedCount := 0

	for idx := range fd.scope.variables {
		v := fd.scope.variables[idx]
		possibleTypes := v.GetPossibleTypes()

		if idx < int(fd.scope.localVariableIndexOffset) {

			// If we had a proper function declaration, we shouldn't change our types
			if !fd.declaration.autoDetectTypes {
				continue
			}

			for key := range (*fd.declaration.parameters)[idx].potentialTypes {
				possibleTypes[key] = true
			}
		}

		if len(possibleTypes) == 1 {
			for key := range possibleTypes {
				if v.typeName != key {
					v.typeName = key
					resolvedCount++
				}
				break
			}
		} else if len(possibleTypes) > 1 {

			// Find the most basic type of all those assigned
			assignedTypes := v.GetAssignedTypes()
			referencedTypes := v.GetReferencedTypes()

			// If this is a function parameter, add the types that were assigned to it
			if idx < int(fd.scope.localVariableIndexOffset) {
				//assignedTypes = []string{}
				for key := range (*fd.declaration.parameters)[idx].potentialTypes {
					assignedTypes = append(assignedTypes, key)
				}
			}

			baseType := findBaseTypeForAssignedTypes(assignedTypes)
			baseType = findBaseTypeForReferencedTypes(referencedTypes, baseType)

			if baseType != UNKNOWN_TYPE {
				if v.typeName != baseType {
					v.typeName = baseType
					resolvedCount++
				}
			} else {

				// Check for enum type
				enumType := getEnumType(possibleTypes)
				if enumType != UNKNOWN_TYPE {
					if v.typeName != enumType {
						v.typeName = enumType
						resolvedCount++
					}
					continue
				}

				if len(assignedTypes) == 1 {
					if v.typeName != assignedTypes[0] {
						v.typeName = assignedTypes[0]
						resolvedCount++
					}
					continue
				}
				// Find the best possible non-handle type
				bestType := pickBestNonHandleType(possibleTypes)
				if v.typeName != bestType {
					v.typeName = bestType
					resolvedCount++
				}
			}
		}
	}

	// Copy over parameter types from their respective scope variable to the function declaration
	for idx := 0; idx < int(fd.scope.localVariableIndexOffset); idx++ {
		v := fd.scope.variables[idx]
		param := &(*fd.declaration.parameters)[idx]
		if v.typeName != UNKNOWN_TYPE {
			param.typeName = v.typeName
		}
	}

	if fd.declaration.autoDetectTypes {
		// See if we can resolve the return type
		if len(fd.declaration.possibleReturnTypes) == 1 {
			for key := range fd.declaration.possibleReturnTypes {
				if fd.declaration.returnTypeName != key {
					fd.declaration.returnTypeName = key
					resolvedCount++
				}
				break
			}
		} else if len(fd.declaration.possibleReturnTypes) > 1 {

			possibleTypes := fd.declaration.possibleReturnTypes

			// Check if they are handle types
			types := []string{}
			for possible := range possibleTypes {
				types = append(types, possible)
			}

			// Check for enum type
			enumType := getEnumType(possibleTypes)
			if enumType != UNKNOWN_TYPE {
				if fd.declaration.returnTypeName != enumType {
					fd.declaration.returnTypeName = enumType
					resolvedCount++
				}
			} else {

				baseType := findBaseTypeForAssignedTypes(types)

				if baseType != UNKNOWN_TYPE {
					if fd.declaration.returnTypeName != baseType {
						fd.declaration.returnTypeName = baseType
						resolvedCount++
					}
				} else {
					bestType := pickBestNonHandleType(possibleTypes)
					if fd.declaration.returnTypeName != bestType {
						fd.declaration.returnTypeName = bestType
						resolvedCount++
					}
				}
			}
		}
	}

	return resolvedCount
}

func (fd *FunctionDefinition) RenderPrototype(writer CodeWriter) {
	// Write the function prototype
	writer.Appendf("%s ", PROTOTYPE_PREFIX)
	writer.Append(renderFunctionDefinitionHeader(fd.declaration))
	writer.Append(";\n")
}

func (fd *FunctionDefinition) isLocalVariableInitialAssignment(statement *Statement) *Variable {
	op1 := statement.graph

	if len(op1.children) == 0 {
		return nil
	}

	op2 := op1.children[0]

	// Skip over any OP_UNKNOWN_3C we hit
	if op2.operation.opcode == OP_UNKNOWN_3C {
		op2 = op2.children[0]
	}

	if op1.operation.opcode == OP_POP_STACK && (op2.operation.opcode == OP_VARIABLE_WRITE || op2.operation.opcode == OP_HANDLE_VARIABLE_WRITE) {
		varData := op2.operation.data.(VariableWriteData)
		references := op2.children[0].GetAllReferencedVariableIndices()
		for idx := varData.index; idx < uint32(len(fd.scope.variables)); idx++ {
			// If this variable assignment references itself or variables that are declared after it, it can't be an initial assignment statement
			if _, ok := references[idx]; ok {
				return nil
			}
		}
		return fd.scope.variables[varData.index]
	}
	return nil
}

func (fd *FunctionDefinition) Render(writer CodeWriter) {

	if OUTPUT_ASSEMBLY {
		PrintFunctionAssembly(fd.declaration, fd.startingIndex, fd.initialOffset, writer)
	}

	// Write the function header
	writer.Append(renderFunctionDefinitionHeader(fd.declaration))
	writer.Append("\n{\n")
	writer.PushIndent()

	assignments := map[uint32]*Statement{}

	endIdx := -1

	// Try to detect initial assignments
	for _, be := range fd.body {
		if !be.IsBlock() {
			statement := be.(*Statement)
			variable := fd.isLocalVariableInitialAssignment(statement)
			if variable != nil && int(variable.stackIndex) > endIdx {
				assignments[variable.stackIndex] = statement
				endIdx = int(variable.stackIndex)
			} else {
				break
			}
		} else {
			break
		}
	}

	fd.body = fd.body[len(assignments):]

	writeLocalVariableDeclarations(fd.scope.variables[fd.scope.localVariableIndexOffset:], assignments, fd.declaration, writer)

	writer.Appendf(`debug atomic Debug.PrintString("Inside function: %s %s\n");`, EXPORTING_PACKAGE, renderFunctionDefinitionHeader(fd.declaration))
	writer.Append("\n")

	RenderBlockElements(fd.body, writer)

	writer.PopIndent()
	writer.Append("}\n\n")
}

func AddFunctionDeclaration(pkg string, name string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.pkg = pkg
	result.name = name
	result.possibleReturnTypes = map[string]bool{}
	result.autoDetectTypes = true

	// Check to see if we have this one already
	if existing, ok := FUNC_DECLARATIONS[result.GetScopedName()]; ok {
		return existing
	}

	result.returnTypeName = UNKNOWN_TYPE

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

func AddFunctionDeclarationFromPrototype(prototype string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.autoDetectTypes = false

	if !strings.HasPrefix(prototype, PROTOTYPE_PREFIX) {
		fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
		return nil
	}

	// Skip the word prototype at the start
	function := strings.TrimSpace(prototype[len(PROTOTYPE_PREFIX):])

	// Find the parameter list
	parts := strings.Split(function, "(")

	if len(parts) != 2 {
		fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
		return nil
	}

	function = parts[0]
	parameterList := parts[1]

	if !strings.HasSuffix(parameterList, ")") {
		fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
		return nil
	}
	parameterList = strings.TrimSpace(parameterList[:len(parameterList)-1])

	parts = strings.Fields(function)

	switch len(parts) {
	case 1:
		result.returnTypeName = ""
	case 2:
		result.returnTypeName = parts[0]
		function = parts[1]
	default:
		fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
		return nil
	}

	parts = strings.Split(function, ".")

	if len(parts) != 2 {
		//fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
		return nil
	}

	// Carve out the package and function name
	result.pkg = strings.TrimSpace(parts[0])
	result.name = strings.TrimSpace(parts[1])

	// Parse the parameters
	if len(parameterList) > 0 {
		params := strings.Split(parameterList, ",")

		parameters := []FunctionParameter{}

		for ii := 0; ii < len(params); ii++ {
			parts = strings.Fields(params[ii])
			if parts[0] == "ref" {
				parts = parts[1:]
			}
			if len(parts) != 2 {
				fmt.Printf("ERROR: Invalid function prototype: %s\n", prototype)
				return nil
			}
			p := FunctionParameter{
				typeName:       parts[0],
				parameterName:  parts[1],
				potentialTypes: map[string]bool{},
			}
			parameters = append(parameters, p)
		}

		result.parameters = &parameters
	} else {
		result.parameters = &[]FunctionParameter{}
	}

	if result.returnTypeName == "task" {
		result.returnTypeName = "task"
	}

	result.possibleReturnTypes = map[string]bool{}

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

func SetAllUnknownFunctionReturnTypesToVoid() {
	for idx := range FUNC_DECLARATIONS {
		f := FUNC_DECLARATIONS[idx]

		if f.returnTypeName == UNKNOWN_TYPE {
			f.returnTypeName = ""
		}
	}
}

func (f *FunctionDeclaration) GetScopedName() string {
	if len(f.pkg) > 0 {
		return fmt.Sprintf("%s.%s", f.pkg, f.name)
	} else {
		return f.name
	}
}

func writeLocalVariableDeclarations(variables []*Variable, assignments map[uint32]*Statement, declaration *FunctionDeclaration, writer CodeWriter) {
	written := 0
	for ii := 0; ii < len(variables); ii++ {
		lv := variables[ii]
		if lv.typeName == UNKNOWN_TYPE && lv.refCount == 0 {
			lv.typeName = "int"
		}

		if lv.typeName == UNKNOWN_TYPE {
			fmt.Printf("WARN: Failed to determine type for local variable %s in function %s\n", lv.variableName, declaration.GetScopedName())
		}

		if assignment, ok := assignments[lv.stackIndex]; ok {
			writer.Appendf("%s ", lv.typeName)
			assignment.Render(writer)
			writer.Append(";")
		} else {
			writer.Appendf("%s %s;", lv.typeName, lv.variableName)
		}

		if OUTPUT_ASSEMBLY {
			writer.Appendf(" // ID: %d", lv.id)
		}
		writer.Append("\n")
		written++
	}

	if written > 0 {
		writer.Append("\n")
	}
}

func renderFunctionDefinitionHeader(declaration *FunctionDeclaration) string {
	var sb strings.Builder

	if len(declaration.returnTypeName) > 0 {
		returnType := declaration.returnTypeName
		if returnType == UNKNOWN_TYPE {
			fmt.Printf("WARN: Failed to determine return type for function %s\n", declaration.GetScopedName())
		}
		sb.WriteString(fmt.Sprintf("%s ", returnType))
	}

	sb.WriteString(declaration.name)
	sb.WriteString("(")
	if declaration.parameters != nil && len(*declaration.parameters) > 0 {
		sb.WriteString(" ")
		count := len(*declaration.parameters)
		for ii := 0; ii < count; ii++ {
			p := (*declaration.parameters)[ii]
			if p.typeName == UNKNOWN_TYPE {
				fmt.Printf("WARN: Failed to determine type for function parameter %s(%s)\n", declaration.GetScopedName(), p.parameterName)
			}
			sb.WriteString(fmt.Sprintf("%s %s", p.typeName, p.parameterName))
			if ii < count-1 {
				sb.WriteString(", ")
			}
		}
		sb.WriteString(" ")
	}
	sb.WriteString(")")

	return sb.String()
}

func DecompileFunction(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) (int, *FunctionDefinition) {
	// if OUTPUT_ASSEMBLY {
	// 	PrintFunctionAssembly(declaration, startingIndex, initialOffset, writer)
	// }
	definition := &FunctionDefinition{
		startingIndex: startingIndex,
		initialOffset: initialOffset,
		declaration:   declaration,
		scope: &Scope{
			function:  declaration,
			variables: []*Variable{},
		},
	}

	var localVariableCount uint32 = 0

	firstOp := OPERATIONS[startingIndex]
	if firstOp.opcode == OP_PUSH_STACK_N {
		localVariableCount = firstOp.data.(CountDataUInt32).count
	}

	// Try to detect the function parameters
	if declaration.parameters == nil {
		maxIndex := -1
		hasSchedules := false

		for idx := startingIndex; idx < len(OPERATIONS); idx++ {
			op := &OPERATIONS[idx]
			switch op.opcode {
			case OP_FUNCTION_END:
				// We are done, so skip to the end
				idx = len(OPERATIONS)

			case OP_VARIABLE_READ:
				varData := op.data.(VariableReadData)
				index := int(varData.index)
				if index > maxIndex {
					maxIndex = index
				}

			case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
				varData := op.data.(VariableWriteData)
				index := int(varData.index)
				if index > maxIndex {
					maxIndex = index
				}

			case OP_SCHEDULE_START:
				hasSchedules = true
			}
		}

		parameterCount := (maxIndex - int(localVariableCount)) + 1

		// This can happen if there are more local variables pushed on the stack than are actually referenced anywhere
		if parameterCount < 0 {
			parameterCount = 0
		}

		params := make([]FunctionParameter, parameterCount)
		for ii := 0; ii < len(params); ii++ {
			p := &params[ii]
			p.typeName = UNKNOWN_TYPE
			p.parameterName = fmt.Sprintf("param_%d", ii)
			p.potentialTypes = map[string]bool{}
		}
		declaration.parameters = &params

		if declaration.returnTypeName == "UNKNOWN" && hasSchedules {
			declaration.returnTypeName = "task"
		}
	}

	// Add the parameters from our function declaration to the scope variables
	if declaration.parameters != nil {
		for ii := 0; ii < len(*declaration.parameters); ii++ {
			param := &(*declaration.parameters)[ii]
			v := &Variable{
				typeName:        param.typeName,
				variableName:    param.parameterName,
				stackIndex:      uint32(ii),
				assignedTypes:   map[string]bool{},
				referencedTypes: map[string]bool{},
				id:              VARIABLE_ID_COUNTER,
			}
			VARIABLE_ID_COUNTER++
			definition.scope.variables = append(definition.scope.variables, v)
			param.id = v.id
		}

		definition.scope.localVariableIndexOffset = uint32(len(*declaration.parameters))
	}

	// Check to see if there are local variables
	if localVariableCount > 0 {
		var ii uint32
		for ii = 0; ii < localVariableCount; ii++ {
			lv := &Variable{
				typeName:        UNKNOWN_TYPE,
				variableName:    fmt.Sprintf("local_%d", ii),
				stackIndex:      uint32(ii + definition.scope.localVariableIndexOffset),
				assignedTypes:   map[string]bool{},
				referencedTypes: map[string]bool{},
				id:              VARIABLE_ID_COUNTER,
			}
			VARIABLE_ID_COUNTER++
			definition.scope.variables = append(definition.scope.variables, lv)
		}
		// Skip over the local variable opcode
		startingIndex++
	}

	// Find the end of the function
	var functionEnd *Operation = nil
	endIdx := 0

	for idx := startingIndex; idx < len(OPERATIONS); idx++ {
		if OPERATIONS[idx].opcode == OP_FUNCTION_END {
			idx--

			if OPERATIONS[idx].opcode == OP_UNKNOWN_3C && OPERATIONS[idx-1].opcode == OP_LITERAL_ZERO {
				idx--
			}

			if OPERATIONS[idx].opcode == OP_UNKNOWN_3C && OPERATIONS[idx-1].opcode == OP_UNKNOWN_40 {
				idx--
			}

			functionEnd = &OPERATIONS[idx]
			endIdx = idx

			// Check to see if we have a bunch of those weird string operations for string local variables at the end of the function
			for idx >= startingIndex+4 {
				op1 := &OPERATIONS[idx-3]
				op2 := &OPERATIONS[idx-2]
				op3 := &OPERATIONS[idx-1]

				if op1.opcode != OP_VARIABLE_READ || op2.opcode != OP_UNKNOWN_3B || op3.opcode != OP_POP_STACK {
					break
				}
				idx -= 3
				functionEnd = &OPERATIONS[idx]
				//endIdx = idx
			}
			break
		}
	}

	// Idiot check
	if functionEnd == nil {
		fmt.Printf("ERROR: Failed to find end of function: %s", declaration.name)
		os.Exit(1)
	}

	// Save off the ending index
	definition.endingIndex = endIdx

	variableCount := uint32(len(definition.scope.variables))

	// Check for out of bounds variable access
	if declaration.parameters != nil {
		for idx := startingIndex; idx < endIdx; idx++ {
			op := &OPERATIONS[idx]
			switch op.opcode {
			case OP_VARIABLE_READ:
				varData := op.data.(VariableReadData)
				if varData.index >= variableCount {
					fmt.Printf("ERROR: Function %s tries to reference variable at index %d while only %d were declared, skipping.\n", declaration.GetScopedName(), varData.index, variableCount)
					return endIdx, definition
				}

			case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
				varData := op.data.(VariableWriteData)
				if varData.index >= variableCount {
					fmt.Printf("ERROR: Function %s tries to write to variable at index %d while only %d were declared, skipping.\n", declaration.GetScopedName(), varData.index, variableCount)
					return endIdx, definition
				}
			}
		}
	}

	// Save off the end offset so we can detect return statements
	definition.scope.functionEndOffset = functionEnd.offset

	blockOps := OPERATIONS[startingIndex : endIdx+1]
	definition.body = ParseOperations(definition.scope, &BlockContext{}, blockOps, 0, len(blockOps)-1)

	return endIdx, definition
}

func PrintFunctionAssembly(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) {
	writer.Appendf("// ==================== START_FUNCTION %s\n", renderFunctionDefinitionHeader(declaration))
	for idx := startingIndex; idx < len(OPERATIONS); idx++ {
		operation := OPERATIONS[idx]
		writer.Appendf("// 0x%08X ", operation.offset)
		operation.WriteAssembly(writer)
		writer.Append("\n")

		if operation.opcode == OP_FUNCTION_END {
			return
		}
	}
}

func ResolveUndefinedFunctionElements() {
	// for _, fnc := range DECOMPILED_FUNCS {
	// 	if fnc.declaration.parameters == nil {
	// 		if len(fnc.scope.variables) == 0 {
	// 			fnc.declaration.parameters = &[]FunctionParameter{}
	// 		}
	// 	}
	// }
}
