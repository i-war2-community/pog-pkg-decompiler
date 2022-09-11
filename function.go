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
}

type FunctionDeclaration struct {
	pkg                 string
	name                string
	returnTypeName      string
	parameters          *[]FunctionParameter
	possibleReturnTypes map[string]bool
}

var FUNC_DECLARATIONS map[string]*FunctionDeclaration = map[string]*FunctionDeclaration{}

type FunctionDefinition struct {
	declaration *FunctionDeclaration
	scope       *Scope
	body        []BlockElement

	startingIndex int
	initialOffset int64
}

func (fd *FunctionDefinition) ResolveTypes() int {
	resolvedCount := 0

	// Resolve variable types
	ResolveTypes(fd.scope, fd.body)

	for idx := range fd.scope.variables {
		v := &fd.scope.variables[idx]
		possibleTypes := v.GetPossibleTypes()

		if idx < int(fd.scope.localVariableIndexOffset) {
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

			// Check for int and bool being the possible types
			if len(possibleTypes) == 2 {
				_, okInt := possibleTypes["int"]
				_, okBool := possibleTypes["bool"]
				if okInt && okBool {
					if v.typeName != "int" {
						v.typeName = "int"
						resolvedCount++
					}
					continue
				}
			}

			// Find the most basic type of all those assigned
			assignedTypes := v.GetAssignedTypes()

			// If this is a function parameter, add the types that were assigned to it
			if idx < int(fd.scope.localVariableIndexOffset) {
				for key := range (*fd.declaration.parameters)[idx].potentialTypes {
					assignedTypes = append(assignedTypes, key)
				}
			}

			if len(assignedTypes) == 0 {
				continue
			}

			baseType := assignedTypes[0]

			for idx := 1; idx < len(assignedTypes); idx++ {
				assigned := assignedTypes[idx]

				if HandleIsDerivedFrom(assigned, baseType) {
					continue
				}
				if HandleIsDerivedFrom(baseType, assigned) {
					baseType = assigned
					continue
				}
				baseType = UNKNOWN_TYPE
				break
			}

			if baseType != UNKNOWN_TYPE {
				if v.typeName != baseType {
					v.typeName = baseType
					resolvedCount++
				}
			}
		}
	}

	// Copy over parameter types from their respective scope variable to the function declaration
	for idx := 0; idx < int(fd.scope.localVariableIndexOffset); idx++ {
		v := &fd.scope.variables[idx]
		param := &(*fd.declaration.parameters)[idx]
		if v.typeName != UNKNOWN_TYPE {
			param.typeName = v.typeName
		}
	}

	// See if we can resolve the return type
	if fd.declaration.returnTypeName == UNKNOWN_TYPE {
		if len(fd.declaration.possibleReturnTypes) == 1 {
			for key := range fd.declaration.possibleReturnTypes {
				fd.declaration.returnTypeName = key
				resolvedCount++
				break
			}
		} else if len(fd.declaration.possibleReturnTypes) > 1 {
			fmt.Print("Too many possible return types")
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

func (fd *FunctionDefinition) isLocalVariableAssignment(statement *Statement) *Variable {
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
		return &fd.scope.variables[varData.index]
	}
	return nil
}

func (fd *FunctionDefinition) Render(writer CodeWriter) {

	if OUTPUT_ASSEMBLY {
		//PrintFunctionAssembly(fd.declaration, fd.startingIndex, fd.initialOffset, writer)
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
			variable := fd.isLocalVariableAssignment(statement)
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

	writeLocalVariableDeclarations(fd.scope.variables[fd.scope.localVariableIndexOffset:], assignments, writer)

	RenderBlockElements(fd.body, writer)

	writer.PopIndent()
	writer.Append("}\n\n")
}

func AddFunctionDeclaration(pkg string, name string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.pkg = pkg
	result.name = name
	result.possibleReturnTypes = map[string]bool{}

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
		result.returnTypeName = "htask"
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

func writeLocalVariableDeclarations(variables []Variable, assignments map[uint32]*Statement, writer CodeWriter) {
	written := 0
	for ii := 0; ii < len(variables); ii++ {
		lv := &variables[ii]
		if lv.typeName == UNKNOWN_TYPE && lv.refCount == 0 {
			lv.typeName = "int"
		}

		if assignment, ok := assignments[lv.stackIndex]; ok {
			writer.Appendf("%s ", lv.typeName)
			assignment.Render(writer)
			writer.Append(";\n")
		} else {
			writer.Appendf("%s %s;\n", lv.typeName, lv.variableName)
		}
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
		if returnType == "htask" {
			returnType = "task"
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
	if OUTPUT_ASSEMBLY {
		PrintFunctionAssembly(declaration, startingIndex, initialOffset, writer)
	}
	definition := &FunctionDefinition{
		startingIndex: startingIndex,
		initialOffset: initialOffset,
		declaration:   declaration,
		scope: &Scope{
			function:  declaration,
			variables: []Variable{},
		},
	}

	// Add the parameters from our function declaration to the scope variables
	if declaration.parameters != nil {
		for ii := 0; ii < len(*declaration.parameters); ii++ {
			param := &(*declaration.parameters)[ii]
			v := Variable{
				typeName:        param.typeName,
				variableName:    param.parameterName,
				stackIndex:      uint32(ii),
				assignedTypes:   map[string]bool{},
				referencedTypes: map[string]bool{},
			}
			definition.scope.variables = append(definition.scope.variables, v)
		}

		definition.scope.localVariableIndexOffset = uint32(len(*declaration.parameters))
	}

	// Check to see if there are local variables
	firstOp := OPERATIONS[startingIndex]
	if firstOp.opcode == OP_PUSH_STACK_N {
		count := firstOp.data.(CountDataUInt32).count
		var ii uint32
		for ii = 0; ii < count; ii++ {
			lv := Variable{
				typeName:        UNKNOWN_TYPE,
				variableName:    fmt.Sprintf("local_%d", ii),
				stackIndex:      uint32(ii + definition.scope.localVariableIndexOffset),
				assignedTypes:   map[string]bool{},
				referencedTypes: map[string]bool{},
			}
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

			if (len(declaration.returnTypeName) == 0) && OPERATIONS[idx].opcode == OP_UNKNOWN_3C && OPERATIONS[idx-1].opcode == OP_LITERAL_ZERO {
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

	// Save off the end offset so we can detect return statements
	definition.scope.functionEndOffset = functionEnd.offset

	blockOps := OPERATIONS[startingIndex : endIdx+1]
	definition.body = ParseOperations(definition.scope, &BlockContext{}, blockOps, 0, len(blockOps)-1)

	return endIdx, definition
}

func PrintFunctionAssembly(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) {
	writer.Appendf("// ==================== START_FUNCTION %s\n", declaration.GetScopedName())
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
