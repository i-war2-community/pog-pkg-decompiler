package main

import (
	"fmt"
	"strings"
)

type Operation struct {
	offset uint32
	opcode byte
	data   OperationData
}

func (op *Operation) Remove() {
	op.opcode = OP_REMOVED
}

func (op *Operation) WriteAssembly(writer CodeWriter) {
	opInfo := OP_MAP[op.opcode]
	writer.Append(opInfo.name)
	if op.data != nil {
		writer.Appendf(" %s", op.data.String())
	}
}

func IsFunctionCall(operation *Operation) bool {
	switch operation.opcode {
	case OP_FUNCTION_CALL_IMPORTED, OP_FUNCTION_CALL_LOCAL, OP_TASK_CALL_IMPORTED, OP_TASK_CALL_LOCAL:
		return true
	}
	return false
}

func IsLiteralInteger(operation *Operation) bool {
	switch operation.opcode {
	case OP_LITERAL_ZERO, OP_LITERAL_ONE, OP_LITERAL_BYTE, OP_LITERAL_SHORT, OP_LITERAL_INT:
		return true
	}

	return false
}

func GetLiteralIntegerValue(operation *Operation) int32 {
	switch operation.opcode {
	case OP_LITERAL_ZERO, OP_LITERAL_ONE, OP_LITERAL_BYTE, OP_LITERAL_SHORT, OP_LITERAL_INT:
		return int32(operation.data.(LiteralInteger).GetValue())
	}
	fmt.Printf("ERROR: Trying to get integer value for non-integer type")
	return -1
}

func RenderFloat(flt float32) string {
	result := fmt.Sprintf("%f", flt)
	// Truncate all the extra zeros off the end
	if len(result) > 3 {
		trailingZeroCount := 0
		for idx := len(result) - 1; idx > 0; idx-- {
			if result[idx] != '0' {
				break
			}
			trailingZeroCount++
		}
		if trailingZeroCount > 1 {
			result = result[:len(result)-(trailingZeroCount-1)]
		}
	}
	return result
}

func RenderOperationCode(operation *Operation, scope *Scope) *string {
	var result string
	switch operation.opcode {
	case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
		writeData := operation.data.(VariableWriteData)
		v := scope.GetVariableByStackIndex(writeData.index)
		if v != nil {
			result = fmt.Sprintf("%s = ", v.variableName)
		}

	case OP_VARIABLE_READ:
		readData := operation.data.(VariableReadData)
		v := scope.GetVariableByStackIndex(readData.index)
		if v != nil {
			result = fmt.Sprint(v.variableName)
		}

	case OP_LITERAL_ONE:
		result = "1"

	case OP_LITERAL_ZERO:
		result = "0"

	case OP_LITERAL_BYTE:
		data := operation.data.(LiteralByteData)
		result = fmt.Sprintf("%d", data.value)

	case OP_LITERAL_SHORT:
		data := operation.data.(LiteralShortData)
		result = fmt.Sprintf("%d", data.value)

	case OP_LITERAL_INT:
		data := operation.data.(LiteralIntData)
		result = fmt.Sprintf("%d", data.value)

	case OP_LITERAL_FLT:
		data := operation.data.(LiteralFloatData)
		result = RenderFloat(data.value)

	case OP_LITERAL_STRING:
		data := operation.data.(LiteralStringData)
		// TODO: This seems like an area that could cause a lot of trouble
		s := strings.ReplaceAll(STRING_TABLE[data.index], "\n", `\n`)
		s = strings.ReplaceAll(s, "\t", `\t`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		result = fmt.Sprintf(`"%s"`, s)

	case OP_FUNCTION_CALL_LOCAL:
		data := operation.data.(FunctionCallData)
		result = data.declaration.name

	case OP_FUNCTION_CALL_IMPORTED:
		data := operation.data.(FunctionCallData)
		result = data.declaration.GetScopedName()

	case OP_TASK_CALL_LOCAL:
		data := operation.data.(FunctionCallData)
		result = fmt.Sprintf("start %s", data.declaration.name)

	case OP_TASK_CALL_IMPORTED:
		data := operation.data.(FunctionCallData)
		result = fmt.Sprintf("start %s", data.declaration.GetScopedName())

	case OP_INT_EQUALS, OP_STRING_EQUALS:
		result = "=="

	case OP_INT_NOT_EQUALS:
		result = "!="

	case OP_INT_GT, OP_FLT_GT:
		result = ">"

	case OP_INT_LT, OP_FLT_LT:
		result = "<"

	case OP_INT_GT_EQUALS, OP_FLT_GT_EQUALS:
		result = ">="

	case OP_INT_LT_EQUALS, OP_FLT_LT_EQUALS:
		result = "<="

	case OP_INT_NEG, OP_FLT_NEG:
		result = "-"

	case OP_INT_ADD, OP_FLT_ADD:
		result = "+"

	case OP_INT_SUB, OP_FLT_SUB:
		result = "-"

	case OP_INT_MUL, OP_FLT_MUL:
		result = "*"

	case OP_INT_DIV, OP_FLT_DIV:
		result = "/"

	case OP_INT_MOD:
		result = "%"

	case OP_LOGICAL_AND:
		result = "&&"

	case OP_LOGICAL_OR:
		result = "||"

	case OP_LOGICAL_NOT:
		result = "!"

	case OP_BITWISE_AND:
		result = "&"

	case OP_BITWISE_OR:
		result = "|"

	case OP_UNKNOWN_3B:
		result = ""

	case OP_UNKNOWN_3C:
		result = ""

	case OP_POP_STACK:
		result = ""

	case OP_CAST_INT_TO_FLT, OP_CAST_TO_BOOL, OP_CAST_FLT_TO_INT:
		result = ""

	case OP_JUMP_IF_FALSE:
		result = ""

	default:
		return nil
	}

	return &result
}
