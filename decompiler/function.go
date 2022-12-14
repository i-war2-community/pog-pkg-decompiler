package decompiler

import (
	"fmt"
	"os"
	"regexp"
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

func (fd *FunctionDeclaration) ReturnsNonVoid() bool {
	return len(fd.returnInfo.typeName) > 0
}

func (fd *FunctionDeclaration) HasParameters() bool {
	if fd.parameters == nil {
		return false
	}
	return len(*fd.parameters) > 0
}

func (fd *FunctionDeclaration) FindParameter(regex *regexp.Regexp) (int, *FunctionParameter) {
	for idx := range *fd.parameters {
		p := &(*fd.parameters)[idx]
		if regex.MatchString(p.parameterName) {
			return idx, p
		}
	}

	return -1, nil
}

func (fd *FunctionDeclaration) IsParameterVariable(v *Variable) bool {
	for idx := range *fd.parameters {
		p := &(*fd.parameters)[idx]
		if p.variable == v {
			return true
		}
	}

	return false
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
	for _, v := range fd.scope.variables {
		if !IsHandleType(v.typeName) {
			continue
		}
		assignedTypes := v.GetAssignedTypes()
		for _, atype := range assignedTypes {
			if !IsHandleType(atype) {
				continue
			}
			if !HandleIsDerivedFrom(atype, v.typeName) {
				fmt.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!>>> Variable %d using type %s, from which assigned type %s is not derived!\n", v.id, v.typeName, atype)
			}
		}
	}
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

func (fd *FunctionDefinition) ResolveAllNames() int {
	totalResolved := 0
	for idx := range fd.scope.variables {
		v := fd.scope.variables[idx]

		// Add generic name providers here
		if IsHandleType(v.typeName) {
			v.AddNameProvider(&HandleTypeNameProvider{handleType: v.typeName})
		}

		if IsEnumType(v.typeName) {
			v.AddNameProvider(&EnumTypeNameProvider{})
		}

		if IsCollectionType(v.typeName) {
			v.AddNameProvider(&CollectionTypeNameProvider{})
		}

		if v.ResolveName() {
			totalResolved++
		}
	}

	nameCollisions := map[string][]*Variable{}

	// Find name collisions
	for _, v := range fd.scope.variables {
		nameCollisions[v.variableName] = append(nameCollisions[v.variableName], v)
	}

	// Resolve name collisions
	for _, vars := range nameCollisions {
		if len(vars) > 1 {
			for idx, v := range vars {
				v.ResolveNamingConflict(idx)
			}
		}
	}

	// Copy over the variable names to our parameters if we are auto detecting the types for this function
	if fd.declaration.autoDetectTypes {
		for idx := range *fd.declaration.parameters {
			p := &(*fd.declaration.parameters)[idx]
			p.parameterName = p.variable.variableName
		}
	}

	// Put underscores on the end of all function parameter names
	for idx := range *fd.declaration.parameters {
		p := &(*fd.declaration.parameters)[idx]
		p.parameterName = fmt.Sprintf("%s_", p.parameterName)
		p.variable.variableName = p.parameterName
	}

	return totalResolved
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

	if op1.operation.opcode == OP_POP_STACK && (op2.operation.opcode == OP_VARIABLE_WRITE || op2.operation.opcode == OP_STRING_VARIABLE_WRITE) {
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

func (fd *FunctionDefinition) getLatestVariableWriteIndexBeforeOffset(offset uint32) int {
	endIdx := offsetToOpIndex(offset, OPERATIONS[fd.startingIndex:])
	if endIdx == -1 {
		return -1
	}
	latestVariableIndex := -1
	for idx := fd.startingIndex; idx < endIdx+fd.startingIndex; idx++ {
		switch OPERATIONS[idx].opcode {
		case OP_VARIABLE_WRITE, OP_STRING_VARIABLE_WRITE:
			varData := OPERATIONS[idx].data.(VariableWriteData)
			latestVariableIndex = int(varData.index)
		}
	}
	return latestVariableIndex
}

func (fd *FunctionDefinition) Render(writer CodeWriter) {

	if OUTPUT_ASSEMBLY {
		PrintFunctionAssembly(fd.declaration, fd.startingIndex, fd.initialOffset, writer)
	}

	// Write the function header
	writer.Append(renderFunctionDefinitionHeader(fd.declaration))
	if OUTPUT_ASSEMBLY {
		if fd.declaration.ReturnsNonVoid() || fd.declaration.HasParameters() {
			writer.Append(" // ")
			if fd.declaration.ReturnsNonVoid() {
				writer.Appendf("Return ID: %d ", fd.declaration.returnInfo.id)
			}
			for _, p := range *fd.declaration.parameters {
				writer.Appendf("%s ID: %d ", p.parameterName, p.variable.id)
			}
		}
	}

	writer.Append("\n{\n")
	writer.PushIndent()

	assignments := map[uint32]*Statement{}

	endIdx := -1

	// Try to detect initial assignments
	for _, be := range fd.body {
		if !be.IsBlock() {
			statement := be.(*Statement)
			variable := fd.isLocalVariableInitialAssignment(statement)
			statementOffset, _ := statement.graph.GetOffsetRange()
			lastVariableWritten := fd.getLatestVariableWriteIndexBeforeOffset(statementOffset)
			if variable != nil && int(variable.stackIndex) > endIdx && int(variable.stackIndex) >= lastVariableWritten {
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

	writeLocalVariableDeclarations(fd.scope.variables[fd.scope.localVariableIndexOffset:], assignments, fd, writer)

	if DEBUG_LOGGING {
		writer.Appendf(`debug atomic Debug.PrintString("Inside function: %s %s\n");`, EXPORTING_PACKAGE, renderFunctionDefinitionHeader(fd.declaration))
		writer.Append("\n")
	}

	RenderBlockElements(fd.body, fd.scope, writer)

	writer.PopIndent()
	writer.Append("}\n\n")
}

func AddFunctionDeclaration(pkg string, name string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.pkg = pkg
	result.name = name

	result.autoDetectTypes = true

	// Check to see if we have this one already
	if existing, ok := FUNC_DECLARATIONS[result.GetScopedName()]; ok {
		return existing
	}

	result.returnInfo = NewVariable("", UNKNOWN_TYPE, true)

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

var LOCAL_FUNCTION_ID_COUNTER int = 0

func NewLocalFunctionAtOffset(offset uint32) *FunctionDeclaration {
	declaration := AddFunctionDeclaration("", fmt.Sprintf("local_function_%d", LOCAL_FUNCTION_ID_COUNTER))
	LOCAL_FUNCTION_ID_COUNTER++
	FUNC_DEFINITION_MAP[offset] = declaration
	return declaration
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
	returnType := UNKNOWN_TYPE

	switch len(parts) {
	case 1:
		returnType = ""
	case 2:
		returnType = parts[0]
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

	result.returnInfo = NewVariable("", returnType, result.pkg == EXPORTING_PACKAGE)

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

func writeLocalVariableDeclarations(variables []*Variable, assignments map[uint32]*Statement, definition *FunctionDefinition, writer CodeWriter) {
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
			fmt.Printf("ERROR: Failed to determine type for local variable %s id %d in function %s\n", lv.variableName, lv.id, definition.declaration.GetScopedName())
		}

		if assignment, ok := assignments[lv.stackIndex]; ok {
			writer.Appendf("%s ", lv.typeName)
			assignment.Render(definition.scope, writer)
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

			case OP_VARIABLE_WRITE, OP_STRING_VARIABLE_WRITE:
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
			v := NewVariable(param.parameterName, param.typeName, true)
			v.stackIndex = uint32(ii)
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
			lv := NewVariable(fmt.Sprintf("local_%d", ii), UNKNOWN_TYPE, true)
			lv.stackIndex = uint32(ii + definition.scope.localVariableIndexOffset)
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

			case OP_VARIABLE_WRITE, OP_STRING_VARIABLE_WRITE:
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
