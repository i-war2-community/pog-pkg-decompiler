package main

import (
	"fmt"
	"os"
	"strings"
)

const PROTOTYPE_PREFIX = "prototype"

type FunctionParameter struct {
	typeName      string
	parameterName string
	variable      *Variable
}

type FunctionDeclaration struct {
	pkg             string
	name            string
	parameters      *[]FunctionParameter
	autoDetectTypes bool
	returnInfo      *Variable
}

func (fd *FunctionDeclaration) ResetPossibleTypes() {
	fd.returnInfo.ResetPossibleTypes()
}

func (fd *FunctionDeclaration) GetReturnType() string {
	if fd.returnInfo.typeName == "task" {
		return "htask"
	}
	return fd.returnInfo.typeName
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

func (fd *FunctionDefinition) ResetPossibleTypes() {
	fd.declaration.ResetPossibleTypes()
	for _, v := range fd.scope.variables {
		v.ResetPossibleTypes()
	}
}

func (fd *FunctionDefinition) CheckCode() {
	CheckCode(fd.scope, fd.body)
}

func (fd *FunctionDefinition) ResolveBodyTypes() int {
	resolvedCount := 0

	// Resolve potential types
	ResolveTypes(fd.scope, fd.body)

	// Resolve only the local variables, not the parameters
	for idx := fd.scope.localVariableIndexOffset; idx < uint32(len(fd.scope.variables)); idx++ {
		v := fd.scope.variables[idx]
		if v.ResolveType() {
			resolvedCount++
		}
	}

	return resolvedCount
}

func (fd *FunctionDefinition) ResolveDeclarationTypes() int {
	resolvedCount := 0

	// If the function declaration came from an include, we don't want to resolve types
	if fd.declaration.autoDetectTypes {

		// Resolve our parameter types
		for idx := 0; idx < int(fd.scope.localVariableIndexOffset); idx++ {
			v := fd.scope.variables[idx]

			if v.ResolveType() {
				resolvedCount++
			}

			// Copy the type over to the parameter
			param := &(*fd.declaration.parameters)[idx]
			if v.typeName != UNKNOWN_TYPE {
				param.typeName = v.typeName
			}
		}

		// See if we can resolve the return type
		if fd.declaration.returnInfo.ResolveType() {
			resolvedCount++
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

	if DEBUG_LOGGING {
		writer.Appendf(`debug atomic Debug.PrintString("Inside function: %s %s\n");`, EXPORTING_PACKAGE, renderFunctionDefinitionHeader(fd.declaration))
		writer.Append("\n")
	}

	RenderBlockElements(fd.body, writer)

	writer.PopIndent()
	writer.Append("}\n\n")
}

func AddFunctionDeclaration(pkg string, name string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.pkg = pkg
	result.name = name
	result.returnInfo = &Variable{
		stackIndex:      0xFFFFFFFF, // Make sure this isn't used somewhere on accident as a regular variable
		assignedTypes:   map[string]bool{},
		referencedTypes: map[string]bool{},
		typeName:        UNKNOWN_TYPE,
	}

	result.autoDetectTypes = true

	// Check to see if we have this one already
	if existing, ok := FUNC_DECLARATIONS[result.GetScopedName()]; ok {
		return existing
	}

	result.returnInfo.id = VARIABLE_ID_COUNTER
	VARIABLE_ID_COUNTER++

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

func NewLocalFunctionAtOffset(offset uint32) *FunctionDeclaration {
	declaration := AddFunctionDeclaration("", fmt.Sprintf("local_function_%08X", offset))
	FUNC_DEFINITION_MAP[offset] = declaration
	return declaration
}

func AddFunctionDeclarationFromPrototype(prototype string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.returnInfo = &Variable{
		stackIndex:      0xFFFFFFFF, // Make sure this isn't used somewhere on accident as a regular variable
		assignedTypes:   map[string]bool{},
		referencedTypes: map[string]bool{},
		typeName:        UNKNOWN_TYPE,
	}
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
		result.returnInfo.typeName = ""
	case 2:
		result.returnInfo.typeName = parts[0]
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
				typeName:      parts[0],
				parameterName: parts[1],
			}
			parameters = append(parameters, p)
		}

		result.parameters = &parameters
	} else {
		result.parameters = &[]FunctionParameter{}
	}

	if result.pkg == EXPORTING_PACKAGE {
		result.returnInfo.id = VARIABLE_ID_COUNTER
		VARIABLE_ID_COUNTER++
	}

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

func ResolveAllUnknownFunctionReturnTypes() {
	for _, fnc := range DECOMPILED_FUNCS {
		if fnc.declaration.returnInfo.typeName == UNKNOWN_TYPE {
			for idx := fnc.endingIndex; idx < len(OPERATIONS); idx++ {
				// Go until we find the actual function end opcode
				if OPERATIONS[idx].opcode == OP_FUNCTION_END {
					// Detect if the return type should be void or a task
					if OPERATIONS[idx-1].opcode == OP_LITERAL_ZERO ||
						(OPERATIONS[idx-1].opcode == OP_UNKNOWN_3C && OPERATIONS[idx-2].opcode == OP_LITERAL_ZERO) {
						fnc.declaration.returnInfo.typeName = ""
					} else {
						fnc.declaration.returnInfo.typeName = "task"
					}
					break
				}
			}
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
			// If it has an init, it is probably a string
			if lv.hasInit {
				lv.typeName = "string"
			} else {
				lv.typeName = "int"
			}
		}

		if lv.typeName == UNKNOWN_TYPE {
			fmt.Printf("ERROR: Failed to determine type for local variable %s id %d in function %s\n", lv.variableName, lv.id, declaration.GetScopedName())
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
	// Use the actual set return type here to deal with the task/htask thing
	returnType := declaration.returnInfo.typeName
	if len(returnType) > 0 {
		if returnType == UNKNOWN_TYPE {
			fmt.Printf("ERROR: Failed to determine return type id %d for function %s\n", declaration.returnInfo.id, declaration.GetScopedName())
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
				fmt.Printf("ERROR: Failed to determine type for function parameter %s(%s) id %d\n", declaration.GetScopedName(), p.parameterName, p.variable.id)
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

func DecompileFunction(declaration *FunctionDeclaration, startingIndex int, initialOffset int64) (int, *FunctionDefinition) {
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
	secondOp := OPERATIONS[startingIndex+1]
	if firstOp.opcode == OP_PUSH_STACK_N && secondOp.opcode != OP_SCHEDULE_START {
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
		}
		declaration.parameters = &params

		// Anything with schedules inside it MUST be a task
		if declaration.returnInfo.typeName == "UNKNOWN" && hasSchedules {
			declaration.returnInfo.typeName = "task"
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
			// Save off a reference to this variable for use elsewhere
			param.variable = v
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
		// Check for handle inits
		if OPERATIONS[idx].opcode == OP_VARIABLE_INIT && OPERATIONS[idx+1].opcode == OP_VARIABLE_WRITE {
			varData := OPERATIONS[idx+1].data.(VariableWriteData)
			definition.scope.variables[varData.index].hasInit = true
		}
		// Keep going until we find the function end operation
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

	if ASSEMBLY_ONLY {
		return endIdx, definition
	}

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
	if ASSEMBLY_ONLY {
		writer.Appendf("// ==================== START_FUNCTION %s\n", declaration.GetScopedName())
	} else {
		writer.Appendf("// ==================== START_FUNCTION %s\n", renderFunctionDefinitionHeader(declaration))
	}
	for idx := startingIndex; idx < len(OPERATIONS); idx++ {
		operation := OPERATIONS[idx]
		if ASSEMBLY_OFFSET_PREFIX {
			writer.Appendf("// 0x%08X ", operation.offset)
		} else {
			writer.Append("// ")
		}
		operation.WriteAssembly(writer)
		writer.Append("\n")

		if operation.opcode == OP_FUNCTION_END {
			return
		}
	}
}
