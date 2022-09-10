package main

import (
	"fmt"
	"os"
	"strings"
)

type FunctionParameter struct {
	typeName      string
	parameterName string
}

type FunctionDeclaration struct {
	pkg            string
	name           string
	returnTypeName string
	parameters     *[]FunctionParameter
}

const PROTOTYPE_PREFIX = "prototype"

var FUNC_DECLARATIONS map[string]*FunctionDeclaration = map[string]*FunctionDeclaration{}

func AddFunctionDeclaration(pkg string, name string) *FunctionDeclaration {
	result := new(FunctionDeclaration)
	result.pkg = pkg
	result.name = name

	// Check to see if we have this one already
	if existing, ok := FUNC_DECLARATIONS[result.GetScopedName()]; ok {
		return existing
	}

	result.returnTypeName = "UNKNOWN"

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
	result.pkg = parts[0]
	result.name = parts[1]

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

	if result.returnTypeName == "task" {
		result.returnTypeName = "htask"
	}

	FUNC_DECLARATIONS[result.GetScopedName()] = result
	return result
}

func (f *FunctionDeclaration) GetScopedName() string {
	if len(f.pkg) > 0 {
		return fmt.Sprintf("%s.%s", f.pkg, f.name)
	} else {
		return f.name
	}
}

func writeLocalVariableDeclarations(variables []Variable, writer CodeWriter) {
	written := 0
	for ii := 0; ii < len(variables); ii++ {
		lv := &variables[ii]
		if lv.setCount > 0 {
			writer.Appendf("%s %s;\n", lv.typeName, lv.variableName)
			written++
		}
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
	if declaration.parameters != nil {
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

func shouldRenderStatement(s *OpGraph) bool {
	// Don't render weird string statements that show up at the end of functions
	if s.operation.opcode == OP_POP_STACK && s.children[0].operation.opcode == OP_UNKNOWN_3B && s.children[0].children[0].operation.opcode == OP_VARIABLE_READ {
		return false
	}

	// Don't render string init statements
	if s.operation.opcode == OP_POP_STACK && s.children[0].operation.opcode == OP_VARIABLE_WRITE && s.children[0].children[0].operation.opcode == OP_HANDLE_INIT {
		return false
	}
	return true
}

func DecompileFunction(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) int {
	if OUTPUT_ASSEMBLY {
		PrintFunctionAssembly(declaration, startingIndex, initialOffset, writer)
	}
	scope := Scope{
		function:  declaration,
		variables: []Variable{},
	}
	//writer.Appendf(renderFunctionDefinitionHeader(declaration))
	//writer.Appendf("\n{\n")
	//writer.PushIndent()

	// Add the parameters from our function declaration to the scope variables
	if declaration.parameters != nil {
		for ii := 0; ii < len(*declaration.parameters); ii++ {
			param := &(*declaration.parameters)[ii]
			v := Variable{
				typeName:      param.typeName,
				variableName:  param.parameterName,
				stackIndex:    uint32(ii),
				possibleTypes: map[string]bool{},
			}
			scope.variables = append(scope.variables, v)
		}

		scope.localVariableIndexOffset = uint32(len(*declaration.parameters))
	}

	// Check to see if there are local variables
	firstOp := OPERATIONS[startingIndex]
	if firstOp.opcode == OP_PUSH_STACK_N {
		count := firstOp.data.(CountDataUInt32).count
		var ii uint32
		for ii = 0; ii < count; ii++ {
			lv := Variable{
				typeName:      "UNKNOWN",
				variableName:  fmt.Sprintf("local_%d", ii),
				stackIndex:    uint32(ii + scope.localVariableIndexOffset),
				possibleTypes: map[string]bool{},
			}
			scope.variables = append(scope.variables, lv)
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

			if len(declaration.returnTypeName) == 0 && OPERATIONS[idx].opcode == OP_UNKNOWN_3C && OPERATIONS[idx-1].opcode == OP_LITERAL_FALSE {
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
	scope.functionEndOffset = functionEnd.offset

	blockOps := OPERATIONS[startingIndex:endIdx]
	body := ParseOperations(scope, blockOps)

	// Resolve variable types
	ResolveTypes(scope, body)

	for idx := range scope.variables {
		v := &scope.variables[idx]
		if v.typeName == "UNKNOWN" {
			if len(v.possibleTypes) == 1 {
				for key := range v.possibleTypes {
					v.typeName = key
					break
				}
			} else if len(v.possibleTypes) > 1 {
				// If we have handle types, find the most concrete one
				baseType := "hobject"
				derivedType := ""

				for typeName := range v.possibleTypes {
					// If this
					if HandleIsDerivedFrom(typeName, baseType) {
						derivedType = typeName
						baseType = typeName
					} else if HandleIsDerivedFrom(baseType, typeName) {
						derivedType = baseType
					} else {
						v.typeName = "UNKNOWN"
						break
					}
				}

				v.typeName = derivedType
			}
		}

		// If we have a type for this variable, copy it over to the function declaration
		if idx < int(scope.localVariableIndexOffset) && v.typeName != "UNKNOWN" {
			param := &(*declaration.parameters)[idx]
			if param.typeName == "UNKNOWN" {
				param.typeName = v.typeName
			}
		}
	}

	// Resolve variable types
	ResolveTypes(scope, body)

	// Write the function header
	writer.Append(renderFunctionDefinitionHeader(declaration))
	writer.Append("\n{\n")
	writer.PushIndent()

	writeLocalVariableDeclarations(scope.variables[scope.localVariableIndexOffset:], writer)

	RenderBlockElements(body, writer)

	writer.PopIndent()
	writer.Append("}\n\n")

	return endIdx
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
