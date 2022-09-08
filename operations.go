package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

type OperationData interface {
	String() string
}

type OperationParser func(data []byte, codeOffset uint32) OperationData

type OperationInfo struct {
	name     string
	dataSize int
	parser   OperationParser
	omit     bool
}

const (
	OP_POP_STACK   byte = 0x01
	OP_POP_STACK_N byte = 0x02
	OP_CLONE_STACK byte = 0x03

	OP_LITERAL_FALSE byte = 0x04
	OP_LITERAL_TRUE  byte = 0x05
	OP_LITERAL_BYTE  byte = 0x06
	OP_LITERAL_SHORT byte = 0x07
	OP_LITERAL_INT   byte = 0x08
	OP_LITERAL_FLT   byte = 0x0B

	OP_VARIABLE_READ  byte = 0x0C
	OP_VARIABLE_WRITE byte = 0x0D
	OP_PUSH_STACK_N   byte = 0x0E

	OP_JUMP          byte = 0x0F
	OP_JUMP_IF_FALSE byte = 0x10
	OP_JUMP_IF_TRUE  byte = 0x11

	OP_FUNCTION_END           byte = 0x13
	OP_FUNCTION_CALL_LOCAL    byte = 0x14
	OP_FUNCTION_CALL_IMPORTED byte = 0x15
	OP_TASK_CALL_LOCAL        byte = 0x17
	OP_TASK_CALL_IMPORTED     byte = 0x18

	OP_INT_ADD byte = 0x1A
	OP_INT_SUB byte = 0x1B
	OP_INT_MUL byte = 0x1C
	OP_INT_DIV byte = 0x1D
	OP_INT_MOD byte = 0x1E
	OP_INT_NEG byte = 0x1F

	OP_INT_EQUALS     byte = 0x20
	OP_INT_NOT_EQUALS byte = 0x21
	OP_INT_GT         byte = 0x22
	OP_INT_LT         byte = 0x23
	OP_INT_GT_EQUALS  byte = 0x24
	OP_INT_LT_EQUALS  byte = 0x25

	OP_FLT_ADD byte = 0x26
	OP_FLT_SUB byte = 0x27
	OP_FLT_MUL byte = 0x28
	OP_FLT_DIV byte = 0x29
	OP_FLT_NEG byte = 0x2B

	OP_FLT_GT        byte = 0x2C
	OP_FLT_LT        byte = 0x2D
	OP_FLT_GT_EQUALS byte = 0x2E
	OP_FLT_LT_EQUALS byte = 0x2F

	OP_LOGICAL_AND byte = 0x30
	OP_LOGICAL_OR  byte = 0x31
	OP_LOGICAL_NOT byte = 0x32

	OP_BITWISE_AND byte = 0x33
	OP_BITWISE_OR  byte = 0x34

	OP_CAST_INT_TO_FLT    byte = 0x37
	OP_CAST_FLT_TO_INT    byte = 0x38
	OP_CAST_HANDLE_TO_INT byte = 0x39

	OP_STRING_INIT byte = 0x3A

	OP_UNKNOWN_3B            byte = 0x3B
	OP_UNKNOWN_3C            byte = 0x3C // Maybe this is some operation to store the top of the stack as a return value? Even void functions return 0 under the hood.
	OP_STRING_VARIABLE_WRITE byte = 0x3D

	OP_LITERAL_STRING byte = 0x3E
	OP_STRING_EQUALS  byte = 0x3F

	OP_UNKNOWN_40 byte = 0x40 // Something to do with lists?

	OP_SCHEDULE_START byte = 0x41
	OP_SCHEDULE_EVERY byte = 0x42

	OP_ATOMIC_START byte = 0x43
	OP_ATOMIC_STOP  byte = 0x44

	OP_JUMP_IF_NOT_DEBUG byte = 0x45
)

var OP_MAP = map[byte]OperationInfo{
	OP_POP_STACK:   {name: "OP_POP_STACK", dataSize: 0},
	OP_POP_STACK_N: {name: "OP_POP_STACK_N", dataSize: 1, parser: ParseCountUInt8},
	OP_CLONE_STACK: {name: "OP_CLONE_STACK", dataSize: 0},

	OP_LITERAL_FALSE: {name: "OP_LITERAL_FALSE", dataSize: 0},
	OP_LITERAL_TRUE:  {name: "OP_LITERAL_TRUE", dataSize: 0},
	OP_LITERAL_BYTE:  {name: "OP_LITERAL_BYTE", dataSize: 1, parser: ParseLiteralByte},
	OP_LITERAL_SHORT: {name: "OP_LITERAL_SHORT", dataSize: 2, parser: ParseLiteralShort},
	OP_LITERAL_INT:   {name: "OP_LITERAL_INT", dataSize: 4, parser: ParseLiteralInt},
	OP_LITERAL_FLT:   {name: "OP_LITERAL_FLT", dataSize: 4, parser: ParseLiteralFloat},

	OP_VARIABLE_READ:  {name: "OP_VARIABLE_READ", dataSize: 4, parser: ParseVariable},
	OP_VARIABLE_WRITE: {name: "OP_VARIABLE_WRITE", dataSize: 4, parser: ParseVariable},
	OP_PUSH_STACK_N:   {name: "OP_PUSH_STACK_N", dataSize: 4, parser: ParseCountUInt32},

	OP_JUMP:          {name: "OP_JUMP", dataSize: 4, parser: ParseJump},
	OP_JUMP_IF_FALSE: {name: "OP_JUMP_IF_FALSE", dataSize: 4, parser: ParseJump},
	OP_JUMP_IF_TRUE:  {name: "OP_JUMP_IF_TRUE", dataSize: 4, parser: ParseJump},

	OP_FUNCTION_END:           {name: "OP_FUNCTION_END", dataSize: 0},
	OP_FUNCTION_CALL_LOCAL:    {name: "OP_FUNCTION_CALL_LOCAL", dataSize: 12},
	OP_FUNCTION_CALL_IMPORTED: {name: "OP_FUNCTION_CALL_IMPORTED", dataSize: 12, parser: ParseFunctionCallImported},

	OP_TASK_CALL_LOCAL:    {name: "OP_TASK_CALL_LOCAL", dataSize: 12},
	OP_TASK_CALL_IMPORTED: {name: "OP_TASK_CALL_IMPORTED", dataSize: 12, parser: ParseFunctionCallImported},

	OP_INT_ADD: {name: "OP_INT_ADD", dataSize: 0},
	OP_INT_SUB: {name: "OP_INT_SUB", dataSize: 0},
	OP_INT_MUL: {name: "OP_INT_MUL", dataSize: 0},
	OP_INT_DIV: {name: "OP_INT_DIV", dataSize: 0},
	OP_INT_MOD: {name: "OP_INT_MOD", dataSize: 0},
	OP_INT_NEG: {name: "OP_INT_NEG", dataSize: 0},

	OP_INT_EQUALS:     {name: "OP_INT_EQUALS", dataSize: 0},
	OP_INT_NOT_EQUALS: {name: "OP_INT_NOT_EQUALS", dataSize: 0},
	OP_INT_GT:         {name: "OP_INT_GT", dataSize: 0},
	OP_INT_LT:         {name: "OP_INT_LT", dataSize: 0},
	OP_INT_GT_EQUALS:  {name: "OP_INT_GT_EQUALS", dataSize: 0},
	OP_INT_LT_EQUALS:  {name: "OP_INT_LT_EQUALS", dataSize: 0},

	OP_FLT_ADD: {name: "OP_FLT_ADD", dataSize: 0},
	OP_FLT_SUB: {name: "OP_FLT_SUB", dataSize: 0},
	OP_FLT_MUL: {name: "OP_FLT_MUL", dataSize: 0},
	OP_FLT_DIV: {name: "OP_FLT_DIV", dataSize: 0},
	OP_FLT_NEG: {name: "OP_FLT_NEG", dataSize: 0},

	OP_FLT_GT:        {name: "OP_FLT_GT", dataSize: 0},
	OP_FLT_LT:        {name: "OP_FLT_LT", dataSize: 0},
	OP_FLT_GT_EQUALS: {name: "OP_FLT_GT_EQUALS", dataSize: 0},
	OP_FLT_LT_EQUALS: {name: "OP_FLT_LT_EQUALS", dataSize: 0},

	OP_LOGICAL_AND: {name: "OP_LOGICAL_AND", dataSize: 0},
	OP_LOGICAL_OR:  {name: "OP_LOGICAL_OR", dataSize: 0},
	OP_LOGICAL_NOT: {name: "OP_LOGICAL_NOT", dataSize: 0},

	OP_BITWISE_AND: {name: "OP_BITWISE_AND", dataSize: 0},
	OP_BITWISE_OR:  {name: "OP_BITWISE_OR", dataSize: 0},

	OP_CAST_INT_TO_FLT:    {name: "OP_CAST_INT_TO_FLT", dataSize: 0},
	OP_CAST_FLT_TO_INT:    {name: "OP_CAST_FLT_TO_INT", dataSize: 0},
	OP_CAST_HANDLE_TO_INT: {name: "OP_CAST_HANDLE_TO_INT", dataSize: 0},

	OP_STRING_INIT: {name: "OP_STRING_INIT", dataSize: 4},

	OP_UNKNOWN_3B:            {name: "OP_UNKNOWN_3B", dataSize: 0, omit: true},
	OP_UNKNOWN_3C:            {name: "OP_UNKNOWN_3C", dataSize: 0, omit: true},
	OP_STRING_VARIABLE_WRITE: {name: "OP_STRING_VARIABLE_WRITE", dataSize: 4, parser: ParseVariable},

	OP_LITERAL_STRING: {name: "OP_LITERAL_STRING", dataSize: 4, parser: ParseLiteralString},
	OP_STRING_EQUALS:  {name: "OP_STRING_EQUALS", dataSize: 0},

	OP_UNKNOWN_40: {name: "OP_UNKNOWN_40", dataSize: 0}, // Something list related?

	OP_SCHEDULE_START: {name: "OP_SCHEDULE_START", dataSize: 0},
	OP_SCHEDULE_EVERY: {name: "OP_SCHEDULE_EVERY", dataSize: 12, parser: ParseScheduleEvery},

	OP_ATOMIC_START: {name: "OP_ATOMIC_START", dataSize: 0},
	OP_ATOMIC_STOP:  {name: "OP_ATOMIC_STOP", dataSize: 0},

	OP_JUMP_IF_NOT_DEBUG: {name: "OP_JUMP_IF_NOT_DEBUG", dataSize: 4, parser: ParseJump},
}

type LiteralByteData struct {
	value int8
}

func (d LiteralByteData) String() string {
	return fmt.Sprintf("%d", d.value)
}

func ParseLiteralByte(data []byte, codeOffset uint32) OperationData {
	return LiteralByteData{
		value: int8(data[0]),
	}
}

type LiteralShortData struct {
	value int16
}

func (d LiteralShortData) String() string {
	return fmt.Sprintf("%d", d.value)
}

func ParseLiteralShort(data []byte, codeOffset uint32) OperationData {
	return LiteralShortData{
		value: int16(binary.LittleEndian.Uint16(data)),
	}
}

type LiteralIntData struct {
	value int32
}

func (d LiteralIntData) String() string {
	return fmt.Sprintf("%d", d.value)
}

func ParseLiteralInt(data []byte, codeOffset uint32) OperationData {
	return LiteralIntData{
		value: int32(binary.LittleEndian.Uint32(data)),
	}
}

type LiteralFloatData struct {
	value float32
}

func (d LiteralFloatData) String() string {
	return fmt.Sprintf("%f", d.value)
}

func ParseLiteralFloat(data []byte, codeOffset uint32) OperationData {
	return LiteralFloatData{
		value: math.Float32frombits(binary.LittleEndian.Uint32(data)),
	}
}

type VariableData struct {
	index uint32
}

func (d VariableData) String() string {
	return fmt.Sprintf("%d", d.index)
}

func ParseVariable(data []byte, codeOffset uint32) OperationData {
	return VariableData{
		index: binary.LittleEndian.Uint32(data),
	}
}

type CountDataUInt8 struct {
	count uint8
}

func (d CountDataUInt8) String() string {
	return fmt.Sprintf("%d", d.count)
}

func ParseCountUInt8(data []byte, codeOffset uint32) OperationData {
	return CountDataUInt8{
		count: uint8(data[0]),
	}
}

type CountDataUInt32 struct {
	count uint32
}

func (d CountDataUInt32) String() string {
	return fmt.Sprintf("%d", d.count)
}

func ParseCountUInt32(data []byte, codeOffset uint32) OperationData {
	return CountDataUInt32{
		count: binary.LittleEndian.Uint32(data),
	}
}

type JumpData struct {
	offset uint32
}

func (d JumpData) String() string {
	return fmt.Sprintf("0x%08X", d.offset)
}

func ParseJump(data []byte, codeOffset uint32) OperationData {
	return JumpData{
		offset: binary.LittleEndian.Uint32(data),
	}
}

type FunctionCallImportedData struct {
	name string
}

func (d FunctionCallImportedData) String() string {
	return fmt.Sprintf(d.name)
}

func ParseFunctionCallImported(data []byte, codeOffset uint32) OperationData {
	name := FUNC_IMPORT_MAP[codeOffset]
	return FunctionCallImportedData{
		name: name,
	}
}

type LiteralStringData struct {
	index uint32
}

func (d LiteralStringData) String() string {
	value := strings.ReplaceAll(STRING_TABLE[d.index], "\n", "\\n")
	return fmt.Sprintf("\"%s\"", value)
}

func ParseLiteralString(data []byte, codeOffset uint32) OperationData {
	return LiteralStringData{
		index: binary.LittleEndian.Uint32(data),
	}
}

type ScheduleEveryData struct {
	offset   uint32
	interval float32
}

func (d ScheduleEveryData) String() string {
	return fmt.Sprintf("0x%08X %f", d.offset, d.interval)
}

func ParseScheduleEvery(data []byte, codeOffset uint32) OperationData {
	return ScheduleEveryData{
		offset: binary.LittleEndian.Uint32(data[0:4]),
		// TODO: Figure out the middle value...
		interval: math.Float32frombits(binary.LittleEndian.Uint32(data[8:12])),
	}
}
