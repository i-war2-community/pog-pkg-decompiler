package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type FunctionParameter struct {
	typeName      string
	parameterName string
}

type Variable struct {
	typeName     string
	variableName string
	stackIndex   uint32
}

type FunctionDeclaration struct {
	pkg            string
	name           string
	returnTypeName string
	parameters     *[]FunctionParameter
}

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

func (f *FunctionDeclaration) GetScopedName() string {
	if len(f.pkg) > 0 {
		return fmt.Sprintf("%s.%s", f.pkg, f.name)
	} else {
		return f.name
	}
}

func writeLocalVariableDeclarations(variables []Variable, writer CodeWriter) {
	for ii := 0; ii < len(variables); ii++ {
		lv := &variables[ii]
		writer.Appendf("%s %s;\n", lv.typeName, lv.variableName)
	}
}

type OpGraph struct {
	code      string
	operation *Operation
	children  []*OpGraph
}

func (og *OpGraph) String() string {
	opInfo := OP_MAP[og.operation.opcode]
	if og.operation.data != nil && len(og.operation.data.String()) > 0 {
		return fmt.Sprintf(" %s[%s] ", opInfo.name, og.operation.data.String())
	} else {
		return fmt.Sprintf(" %s ", opInfo.name)
	}
}

func printGraphNode(node *OpGraph, writer CodeWriter, onlyChild bool) {

	if len(node.children) == 2 {
		if !onlyChild {
			writer.Append("(")
		}
		printGraphNode(node.children[0], writer, false)
		writer.Append(" ")
	}

	// Write ourselves
	if len(node.code) > 0 {
		writer.Append(node.code)
	} else {
		opInfo := OP_MAP[node.operation.opcode]
		writer.Appendf("%s", opInfo.name)
		if node.operation.data != nil && len(node.operation.data.String()) > 0 {
			writer.Appendf("[%s]", node.operation.data.String())
		}
	}

	if len(node.children) == 1 {
		printGraphNode(node.children[0], writer, true)
	}

	if len(node.children) == 2 {
		writer.Append(" ")
		printGraphNode(node.children[1], writer, false)
		if !onlyChild {
			writer.Append(")")
		}
	}

	if len(node.children) > 2 {
		writer.Append("(")
		for ii := len(node.children) - 1; ii >= 0; ii-- {
			printGraphNode(node.children[ii], writer, true)
			if ii > 0 {
				writer.Append(", ")
			}
		}
		writer.Append(")")
	}
}

func getVariableByStackIndex(variables []Variable, stackIndex uint32) *Variable {
	for ii := 0; ii < len(variables); ii++ {
		lv := &variables[ii]
		if lv.stackIndex == stackIndex {
			return lv
		}
	}
	return nil
}

func renderFunctionDefinitionHeader(declaration *FunctionDeclaration) string {
	var sb strings.Builder

	if len(declaration.returnTypeName) > 0 {
		sb.WriteString(fmt.Sprintf("%s ", declaration.returnTypeName))
	}

	sb.WriteString(declaration.name)
	sb.WriteString("(")
	if declaration.parameters != nil {
		count := len(*declaration.parameters)
		for ii := 0; ii < count; ii++ {
			p := (*declaration.parameters)[ii]
			sb.WriteString(fmt.Sprintf("%s %s", p.typeName, p.parameterName))
			if ii < count-1 {
				sb.WriteString(", ")
			}
		}
	}
	sb.WriteString(")")

	return sb.String()
}

func renderOperationCode(operation *Operation, variables []Variable) string {
	switch operation.opcode {
	case OP_VARIABLE_WRITE, OP_STRING_VARIABLE_WRITE:
		writeData := operation.data.(VariableWriteData)
		v := getVariableByStackIndex(variables, writeData.index)
		return fmt.Sprintf("%s = ", v.variableName)

	case OP_VARIABLE_READ:
		readData := operation.data.(VariableReadData)
		v := getVariableByStackIndex(variables, readData.index)
		return fmt.Sprint(v.variableName)

	case OP_LITERAL_TRUE:
		return "1"

	case OP_LITERAL_FALSE:
		return "0"

	case OP_LITERAL_BYTE:
		data := operation.data.(LiteralByteData)
		return fmt.Sprintf("%d", data.value)

	case OP_LITERAL_SHORT:
		data := operation.data.(LiteralShortData)
		return fmt.Sprintf("%d", data.value)

	case OP_LITERAL_INT:
		data := operation.data.(LiteralIntData)
		return fmt.Sprintf("%d", data.value)

	case OP_LITERAL_FLT:
		data := operation.data.(LiteralFloatData)
		return fmt.Sprintf("%f", data.value)

	case OP_LITERAL_STRING:
		data := operation.data.(LiteralStringData)
		// TODO: This seems like an area that could cause a lot of trouble
		s, _ := json.Marshal(STRING_TABLE[data.index])
		return string(s)

	case OP_FUNCTION_CALL_LOCAL:
		data := operation.data.(FunctionCallLocalData)
		return data.declaration.name

	case OP_INT_EQUALS, OP_STRING_EQUALS:
		return "=="

	case OP_INT_NOT_EQUALS:
		return "!="

	case OP_INT_GT, OP_FLT_GT:
		return ">"

	case OP_INT_LT, OP_FLT_LT:
		return "<"

	case OP_INT_GT_EQUALS, OP_FLT_GT_EQUALS:
		return ">="

	case OP_INT_LT_EQUALS, OP_FLT_LT_EQUALS:
		return "<="

	case OP_INT_ADD, OP_FLT_ADD:
		return "+"

	case OP_INT_SUB, OP_FLT_SUB:
		return "-"

	case OP_INT_MUL, OP_FLT_MUL:
		return "*"

	case OP_INT_DIV, OP_FLT_DIV:
		return "/"

	case OP_INT_MOD:
		return "%"

	case OP_LOGICAL_AND:
		return "&&"

	case OP_LOGICAL_OR:
		return "||"

	case OP_LOGICAL_NOT:
		return "!"

	}

	return ""
}

func DecompileFunction(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) int {
	PrintFunctionAssembly(declaration, startingIndex, initialOffset, writer)

	variables := []Variable{}
	writer.Appendf(renderFunctionDefinitionHeader(declaration))
	writer.Appendf("\n{\n")
	writer.PushIndent()

	var localVariableIndexOffset uint32 = 0

	if declaration.parameters != nil {
		for ii := 0; ii < len(*declaration.parameters); ii++ {
			param := &(*declaration.parameters)[ii]
			v := Variable{
				typeName:     param.typeName,
				variableName: param.parameterName,
				stackIndex:   uint32(ii),
			}
			variables = append(variables, v)
		}

		localVariableIndexOffset = uint32(len(*declaration.parameters))
	}

	// Check to see if there are local variables
	firstOp := OPERATIONS[startingIndex]
	if firstOp.opcode == OP_PUSH_STACK_N {
		count := firstOp.data.(CountDataUInt32).count
		var ii uint32
		for ii = 0; ii < count; ii++ {
			lv := Variable{
				typeName:     "UNKNOWN",
				variableName: fmt.Sprintf("local_%d", ii),
				stackIndex:   uint32(ii + localVariableIndexOffset),
			}
			variables = append(variables, lv)
		}
		// Skip over the local variable opcode
		startingIndex++
	}

	writeLocalVariableDeclarations(variables[localVariableIndexOffset:], writer)

	stack := []*OpGraph{}

	for idx := startingIndex; idx < len(OPERATIONS); idx++ {
		operation := &OPERATIONS[idx]

		if operation.opcode == OP_FUNCTION_END {
			writer.PopIndent()
			writer.Append("}\n")
			return idx
		}

		opInfo := OP_MAP[operation.opcode]

		if opInfo.omit || operation.data == nil {
			continue
		}

		// Create a node for this operation
		node := new(OpGraph)
		node.operation = operation
		node.code = renderOperationCode(operation, variables)

		if operation.data.PopCount() > 0 {
			if operation.data.PopCount() > len(stack) {
				fmt.Printf("Warn: Stack underflow!")
				//os.Exit(2)
				continue
			}
			for ii := 0; ii < operation.data.PopCount(); ii++ {
				last := len(stack) - 1
				child := stack[last]
				stack = stack[:last]
				node.children = append(node.children, child)
			}
		}
		if operation.data.PushCount() == 1 {
			stack = append(stack, node)
		}

		if operation.opcode == OP_POP_STACK {
			printGraphNode(node.children[0], writer, true)
			writer.Append(";\n")
		}
	}
	return len(OPERATIONS)
}

func PrintFunctionAssembly(declaration *FunctionDeclaration, startingIndex int, initialOffset int64, writer CodeWriter) {
	writer.Appendf("// ==================== START_FUNCTION %s\n", declaration.GetScopedName())
	for idx := startingIndex; idx < len(OPERATIONS); idx++ {
		operation := OPERATIONS[idx]
		writer.Appendf("// 0x%08X 0x%08X ", operation.offset+uint32(initialOffset), operation.offset)
		operation.WriteAssembly(writer)
		writer.Append("\n")

		if operation.opcode == OP_FUNCTION_END {
			return
		}
	}
}
