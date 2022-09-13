package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/juliangruber/go-intersect"
)

const UNKNOWN_TYPE = "UNKNOWN"

type BlockContext struct {
	breakOffset    *uint32
	continueOffset *uint32
	currentBlock   BlockElement
}

func (bc *BlockContext) IsCurrentBlockIfBlock() bool {
	if bc.currentBlock == nil {
		return false
	}

	switch bc.currentBlock.(type) {
	case *IfBlock:
		return true
	}
	return false
}

type BlockElement interface {
	Render(writer CodeWriter)
	IsBlock() bool
	RendersAsBlock() bool
	ResolveTypes(scope *Scope)
	CheckCode(scope *Scope)
}

type OpGraph struct {
	code      *string
	operation *Operation
	children  []*OpGraph
	typeName  string
}

func (og *OpGraph) GetAllReferencedVariableIndices() map[uint32]bool {
	result := map[uint32]bool{}
	switch og.operation.opcode {
	case OP_VARIABLE_READ:
		varData := og.operation.data.(VariableReadData)
		result[varData.index] = true

	case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
		varData := og.operation.data.(VariableWriteData)
		result[varData.index] = true
	}

	for _, child := range og.children {
		childResult := child.GetAllReferencedVariableIndices()
		for k, v := range childResult {
			result[k] = v
		}
	}

	return result
}

func (og *OpGraph) String() string {
	opInfo := OP_MAP[og.operation.opcode]
	if og.operation.data != nil && len(og.operation.data.String()) > 0 {
		return fmt.Sprintf(" %s[%s] ", opInfo.name, og.operation.data.String())
	} else {
		return fmt.Sprintf(" %s ", opInfo.name)
	}
}

func (og *OpGraph) SetPossibleType(scope *Scope, typeName string) {
	switch og.operation.opcode {
	case OP_VARIABLE_READ:
		varData := og.operation.data.(VariableReadData)
		scope.variables[varData.index].AddReferencedType(typeName)

	case OP_LITERAL_ZERO, OP_LITERAL_ONE, OP_LITERAL_BYTE, OP_LITERAL_SHORT, OP_LITERAL_INT:
		if IsEnumType(typeName) {
			numberData := og.operation.data.(LiteralInteger)
			value := numberData.GetValue()
			if value >= 0 {
				memberName := ENUM_MAP[typeName].valueToName[uint32(numberData.GetValue())]
				if len(memberName) > 0 {
					og.code = &memberName
				}
			}
		}

	case OP_FUNCTION_CALL_IMPORTED, OP_FUNCTION_CALL_LOCAL:
		fncData := og.operation.data.(FunctionCallData)
		if IsEnumType(typeName) {
			fncData.declaration.possibleReturnTypes[typeName] = true
		}
	}
}

func (og *OpGraph) ShouldRender() bool {
	// Don't render weird string statements that show up at the end of functions
	if og.operation.opcode == OP_POP_STACK && og.children[0].operation.opcode == OP_UNKNOWN_3B && og.children[0].children[0].operation.opcode == OP_VARIABLE_READ {
		return false
	}

	// Don't render string init statements
	if og.operation.opcode == OP_POP_STACK && og.children[0].operation.opcode == OP_VARIABLE_WRITE && og.children[0].children[0].operation.opcode == OP_HANDLE_INIT {
		return false
	}
	return true
}

func (og *OpGraph) FlagAsElseJump() {
	elseJumpCode := "else_jump"
	og.code = &elseJumpCode
}

func (og *OpGraph) IsElseJump() bool {
	return og.code != nil && *og.code == "else_jump"
}

func (og *OpGraph) GetVariableIndices() []uint32 {
	result := []uint32{}

	// Check if we have a variable
	switch og.operation.opcode {
	case OP_VARIABLE_READ:
		data := og.operation.data.(VariableReadData)
		result = append(result, data.index)

	case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
		data := og.operation.data.(VariableWriteData)
		result = append(result, data.index)
	}

	// Check our children
	for _, child := range og.children {
		childResult := child.GetVariableIndices()
		result = append(result, childResult...)
	}

	return result
}

func (og *OpGraph) GetOffsetRange() (uint32, uint32) {
	var min uint32 = og.operation.offset
	var max uint32 = og.operation.offset

	// Check our children
	for _, child := range og.children {
		childMin, childMax := child.GetOffsetRange()

		if childMin < min {
			min = childMin
		}
		if childMax > max {
			max = childMax
		}
	}

	return min, max
}

func (og *OpGraph) ResolveTypes(scope *Scope) {

	noneCode := "none"
	trueCode := "true"
	falseCode := "false"

	for idx := range og.children {
		og.children[idx].ResolveTypes(scope)
	}

	switch og.operation.opcode {
	case OP_CAST_FLT_TO_INT, OP_BITWISE_AND, OP_BITWISE_OR, OP_INT_NEG, OP_LITERAL_BYTE, OP_LITERAL_SHORT, OP_LITERAL_INT:
		og.typeName = "int"

	case OP_INT_ADD, OP_INT_SUB, OP_INT_MUL, OP_INT_DIV, OP_INT_MOD:
		og.typeName = "int"
		og.children[0].SetPossibleType(scope, "int")
		og.children[1].SetPossibleType(scope, "int")

	case OP_CAST_INT_TO_FLT, OP_FLT_NEG, OP_LITERAL_FLT:
		og.typeName = "float"

	case OP_FLT_ADD, OP_FLT_SUB, OP_FLT_MUL, OP_FLT_DIV:
		og.typeName = "float"
		og.children[0].SetPossibleType(scope, "float")
		og.children[1].SetPossibleType(scope, "float")

	case OP_LITERAL_STRING:
		og.typeName = "string"

	case OP_FUNCTION_CALL_IMPORTED, OP_TASK_CALL_IMPORTED, OP_FUNCTION_CALL_LOCAL, OP_TASK_CALL_LOCAL:
		funcData := og.operation.data.(FunctionCallData)
		if funcData.declaration.returnTypeName != UNKNOWN_TYPE {
			if funcData.declaration.returnTypeName == "task" {
				og.typeName = "htask"
			} else {
				og.typeName = funcData.declaration.returnTypeName
			}
		}

		if funcData.declaration.parameters != nil && len(*funcData.declaration.parameters) == len(og.children) {
			for ii := range *funcData.declaration.parameters {
				param := &(*funcData.declaration.parameters)[ii]
				child := og.children[len(og.children)-1-ii]

				if child.typeName != UNKNOWN_TYPE {
					param.potentialTypes[child.typeName] = true
				} else {
					switch child.operation.opcode {
					case OP_LITERAL_ZERO:
						param.potentialTypes["bool"] = true
						param.potentialTypes["int"] = true

					case OP_LITERAL_ONE:
						param.potentialTypes["bool"] = true
						param.potentialTypes["int"] = true
					}
				}

				if param.typeName != UNKNOWN_TYPE {

					child.SetPossibleType(scope, param.typeName)

					if IsEnumType(param.typeName) {
						var literal LiteralInteger = nil
						switch child.operation.opcode {
						case OP_LITERAL_ONE, OP_LITERAL_ZERO:
							literal = child.operation.data.(LiteralBitData)

						case OP_LITERAL_BYTE:
							literal = child.operation.data.(LiteralByteData)

						case OP_LITERAL_SHORT:
							literal = child.operation.data.(LiteralShortData)

						case OP_LITERAL_INT:
							literal = child.operation.data.(LiteralIntData)
						}
						if literal != nil {
							// Replace the enum value with the name if it matches any known values
							if name, ok := ENUM_MAP[param.typeName].valueToName[uint32(literal.GetValue())]; ok {
								child.code = &name
							}
						}
					}

					switch child.operation.opcode {
					case OP_LITERAL_ZERO:
						if param.typeName == "bool" {
							child.code = &falseCode
						} else if IsHandleType(param.typeName) {
							child.code = &noneCode
						}

					case OP_LITERAL_ONE:
						if param.typeName == "bool" {
							child.code = &trueCode
						}
					}
				}
			}
		}

	case OP_LOGICAL_AND, OP_LOGICAL_OR, OP_LOGICAL_NOT:
		og.typeName = "bool"

	case OP_INT_GT, OP_INT_LT, OP_INT_GT_EQUALS, OP_INT_LT_EQUALS:
		og.typeName = "bool"
		child1 := og.children[0]
		child2 := og.children[1]

		child1.SetPossibleType(scope, "int")
		child2.SetPossibleType(scope, "int")

	case OP_INT_EQUALS, OP_INT_NOT_EQUALS:
		og.typeName = "bool"

		child1 := og.children[0]
		child2 := og.children[1]

		child1.SetPossibleType(scope, "int")
		child2.SetPossibleType(scope, "int")

		child1IsHandle := IsHandleType(child1.typeName)
		child2IsHandle := IsHandleType(child2.typeName)

		child1IsCast := child1.operation.opcode == OP_CAST_HANDLE_TO_BOOL
		child2IsCast := child2.operation.opcode == OP_CAST_HANDLE_TO_BOOL

		// We need to do a special check here to see if we are comparing a handle to "none", which gets compiled down to a zero
		if child1IsCast || child1IsHandle {
			if child2.operation.opcode == OP_LITERAL_ZERO {
				child2.code = &noneCode
			}
		}

		if child2IsCast || child2IsHandle {
			if child1.operation.opcode == OP_LITERAL_ZERO {
				child1.code = &noneCode
			}
		}

		if child1IsHandle {
			if child2IsCast {
				child2.children[0].SetPossibleType(scope, "hobject")
			} else {
				child2.SetPossibleType(scope, child1.typeName)
			}
		}

		if child2IsHandle {
			if child2IsCast {
				child1.children[0].SetPossibleType(scope, "hobject")
			} else {
				child1.SetPossibleType(scope, child2.typeName)
			}
		}

	case OP_FLT_GT, OP_FLT_LT, OP_FLT_GT_EQUALS, OP_FLT_LT_EQUALS:
		og.typeName = "bool"
		child1 := og.children[0]
		child2 := og.children[1]

		child1.SetPossibleType(scope, "float")
		child2.SetPossibleType(scope, "float")

	case OP_STRING_EQUALS:
		og.typeName = "bool"

	case OP_VARIABLE_READ:
		varData := og.operation.data.(VariableReadData)
		scope.variables[varData.index].refCount++
		if scope.variables[varData.index].typeName != UNKNOWN_TYPE {
			og.typeName = scope.variables[varData.index].typeName
		}

	case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
		varData := og.operation.data.(VariableWriteData)
		v := scope.variables[varData.index]
		// Add to the variable's ref count if this isn't just from a handle init
		if og.children[0].operation.opcode != OP_HANDLE_INIT {
			v.refCount++
		}

		// Check for assigning an enum from a literal integer
		if IsEnumType(v.typeName) {
			og.children[0].SetPossibleType(scope, v.typeName)
		}

		// Copy over the type of our first child
		childType := og.children[0].typeName

		switch og.children[0].operation.opcode {
		case OP_LITERAL_ZERO:
			if IsHandleType(v.typeName) {
				og.children[0].code = &noneCode
				break
			}
			fallthrough
		case OP_LITERAL_ONE:
			// It could be either of these really
			v.AddAssignedType("bool")
			v.AddAssignedType("int")

			// If we are assigning literal true or literal false
			if v.typeName == "bool" {
				childType = "bool"
				boolStr := falseCode
				if og.children[0].operation.opcode == OP_LITERAL_ONE {
					boolStr = trueCode
				}

				og.children[0].code = &boolStr
			}
		}

		if childType != UNKNOWN_TYPE {
			og.typeName = childType
			if og.typeName != UNKNOWN_TYPE {
				v.AddAssignedType(og.typeName)
			}
		} else if v.typeName != UNKNOWN_TYPE {
			og.children[0].SetPossibleType(scope, v.typeName)
		}

	case OP_JUMP:
		if og.code != nil && strings.HasPrefix(*og.code, "return") {
			//if scope.function.returnTypeName == UNKNOWN_TYPE {
			returnType := ""
			if len(og.children) > 0 {
				switch og.children[0].operation.opcode {
				case OP_LITERAL_ZERO, OP_LITERAL_ONE:
					scope.function.possibleReturnTypes["bool"] = true
					scope.function.possibleReturnTypes["int"] = true
				default:
					returnType = og.children[0].typeName
					if returnType != UNKNOWN_TYPE {
						scope.function.possibleReturnTypes[returnType] = true
					}
				}
			}
			//} else {
			if scope.function.returnTypeName != UNKNOWN_TYPE {
				if len(og.children) > 0 {
					returnOp := og.children[0]

					if IsEnumType(scope.function.returnTypeName) {

						// Make sure any local variable knows it should be an enum type
						returnOp.SetPossibleType(scope, scope.function.returnTypeName)

						if IsLiteralInteger(returnOp.operation) {
							value := GetLiteralIntegerValue(returnOp.operation)
							enumData := ENUM_MAP[scope.function.returnTypeName]
							if value >= 0 {
								name := enumData.valueToName[uint32(value)]
								if len(name) > 0 {
									returnOp.code = &name
								}
							}
						}
					} else {
						switch returnOp.operation.opcode {
						case OP_LITERAL_ZERO:
							if IsHandleType(scope.function.returnTypeName) {
								returnOp.code = &noneCode
							} else if scope.function.returnTypeName == "bool" {
								returnOp.code = &falseCode
							}

						case OP_LITERAL_ONE:
							if scope.function.returnTypeName == "bool" {
								returnOp.code = &trueCode
							}
						}
					}
				}
			}
			//}
		}

	case OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
		// If we have a variable read inside an if statement, we might have a bool
		if len(og.children) == 1 {
			og.children[0].SetPossibleType(scope, "bool")
		}

	default:
		if len(og.children) > 0 {
			og.typeName = og.children[0].typeName
		}
	}
}

func (og *OpGraph) CheckCode(scope *Scope) {
	for idx := range og.children {
		og.children[idx].CheckCode(scope)
	}

	switch og.operation.opcode {
	case OP_INT_EQUALS, OP_INT_NOT_EQUALS:
		child1 := og.children[0]
		child2 := og.children[1]

		child1IsHandle := IsHandleType(child1.typeName)
		child2IsHandle := IsHandleType(child2.typeName)

		child1IsCast := child1.operation.opcode == OP_CAST_HANDLE_TO_BOOL
		child2IsCast := child2.operation.opcode == OP_CAST_HANDLE_TO_BOOL

		if !child1IsCast && !child2IsCast && child1IsHandle && child2IsHandle && child1.typeName != child2.typeName {
			// cw := NewCodeWriter(os.Stdout)
			// cw.Appendf("ERROR: Mismatched handle types in equivalence check. This will cause both handles to be cast to bools and then compared:\n")
			// cw.PushIndent()
			// printGraphNode(og, cw, false)
			// cw.Append("\n")
			// cw.Appendf("L Type: %s\n", child1.typeName)
			// cw.Appendf("R Type: %s\n", child2.typeName)
			// cw.PopIndent()
			// cw.Append("\n")

			childIdx := -1
			targetType := ""

			if HandleIsDerivedFrom(child1.typeName, child2.typeName) {
				childIdx = 0
				targetType = child2.typeName

			} else if HandleIsDerivedFrom(child2.typeName, child1.typeName) {
				childIdx = 1
				targetType = child1.typeName
			} else {
				fmt.Printf("ERROR: Failed to insert cast to fix mismatched handle type comparison.\n")
				return
			}

			// Get the appropriate function to make the cast
			castFunction := GetCastFunctionForHandleType(targetType)

			// HACK: Insert an operation to perform the cast once we render the code
			castOp := &OpGraph{
				operation: &Operation{
					opcode: OP_CAST_HANDLE_TO_HANDLE,
				},
				children: []*OpGraph{
					og.children[childIdx],
				},
				code: &castFunction,
			}
			og.children[childIdx] = castOp
		}
	}
}

func printGraphNode(node *OpGraph, writer CodeWriter, onlyChild bool) {

	if len(node.children) == 2 && !IsFunctionCall(node.operation) {
		if !onlyChild || node.operation.opcode == OP_STRING_EQUALS {
			writer.Append("(")
		}
		printGraphNode(node.children[0], writer, false)
		writer.Append(" ")
	}

	// Write ourselves
	if node.code != nil {
		writer.Append(*node.code)
	} else {
		opInfo := OP_MAP[node.operation.opcode]
		writer.Appendf("%s", opInfo.name)
		if node.operation.data != nil && len(node.operation.data.String()) > 0 {
			writer.Appendf("[%s]", node.operation.data.String())
		}
	}

	if len(node.children) == 1 && !IsFunctionCall(node.operation) {
		printGraphNode(node.children[0], writer, true)
	}

	if len(node.children) == 2 && !IsFunctionCall(node.operation) {
		writer.Append(" ")
		printGraphNode(node.children[1], writer, false)
		if !onlyChild || node.operation.opcode == OP_STRING_EQUALS {
			writer.Append(")")
		}
	}

	if IsFunctionCall(node.operation) {
		writer.Append("(")
		if len(node.children) > 0 {
			writer.Append(" ")
		}
		for ii := len(node.children) - 1; ii >= 0; ii-- {
			printGraphNode(node.children[ii], writer, true)
			if ii > 0 {
				writer.Append(", ")
			}
		}
		if len(node.children) > 0 {
			writer.Append(" ")
		}
		writer.Append(")")
	}
}

type Statement struct {
	graph *OpGraph
}

func (s *Statement) Render(writer CodeWriter) {
	printGraphNode(s.graph, writer, true)
}

func (s *Statement) RenderAssemblyOffsets(writer CodeWriter) {
	min, max := s.graph.GetOffsetRange()
	if min != max {
		writer.Appendf("0x%08X - 0x%08X", min, max)
	} else {
		writer.Appendf("0x%08X", min)
	}
}

func (s *Statement) IsBlock() bool {
	return false
}

func (s *Statement) RendersAsBlock() bool {
	return false
}

func (s *Statement) ResolveTypes(scope *Scope) {
	s.graph.ResolveTypes(scope)
}

func (s *Statement) CheckCode(scope *Scope) {
	s.graph.CheckCode(scope)
}

func shouldHaveNewlineBetween(element1 BlockElement, element2 BlockElement) bool {
	if element1.RendersAsBlock() && element2.RendersAsBlock() {
		_, isIf := element1.(*IfBlock)
		_, isElse := element2.(*ElseBlock)
		return !(isIf && isElse)
	}
	if !element1.RendersAsBlock() && !element2.RendersAsBlock() {
		return false
	}

	return true
}

func RenderBlockElements(elements []BlockElement, writer CodeWriter) {
	for idx := 0; idx < len(elements); idx++ {
		e := elements[idx]
		e.Render(writer)
		if !e.IsBlock() {
			if OUTPUT_ASSEMBLY {
				writer.Append("; // ")
				s := e.(*Statement)
				s.RenderAssemblyOffsets(writer)
				writer.Append("\n")
			} else {
				writer.Append(";\n")
			}
		}
		if idx < len(elements)-1 && shouldHaveNewlineBetween(e, elements[idx+1]) {
			writer.Append("\n")
		}
	}
}

type IfBlock struct {
	conditional *Statement
	body        []BlockElement
}

func (ib *IfBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Append("if ( ")
	ib.conditional.Render(writer)
	if OUTPUT_ASSEMBLY {
		writer.Append(" ) // ")
		ib.conditional.RenderAssemblyOffsets(writer)
		writer.Append("\n")
	} else {
		writer.Append(" )\n")
	}
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(ib.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (ib *IfBlock) IsBlock() bool {
	return true
}

func (ib *IfBlock) RendersAsBlock() bool {
	return true
}

func (ib *IfBlock) ResolveTypes(scope *Scope) {
	ib.conditional.ResolveTypes(scope)
	ResolveTypes(scope, ib.body)
}

func (ib *IfBlock) CheckCode(scope *Scope) {
	ib.conditional.CheckCode(scope)
	CheckCode(scope, ib.body)
}

type ElseBlock struct {
	body []BlockElement
}

func (eb *ElseBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Append("else")

	inline := false

	// If we only have one element and it is any kind of block, just do an else without braces
	if len(eb.body) == 1 && eb.body[0].IsBlock() {
		inline = true
	}

	// If we only have two elements, an if and another else, just do an else without braces
	if len(eb.body) == 2 {
		_, okIf := eb.body[0].(*IfBlock)
		_, okElse := eb.body[1].(*ElseBlock)
		if okIf && okElse {
			inline = true
		}
	}

	if inline {
		writer.Append(" ")
		RenderBlockElements(eb.body, writer)
	} else {

		writer.Append("\n")
		writer.Append("{\n")
		writer.PushIndent()

		// Write out the body
		RenderBlockElements(eb.body, writer)

		writer.PopIndent()
		writer.Append("}\n")
	}
}

func (eb *ElseBlock) IsBlock() bool {
	return true
}

func (eb *ElseBlock) RendersAsBlock() bool {
	return true
}

func (eb *ElseBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, eb.body)
}

func (eb *ElseBlock) CheckCode(scope *Scope) {
	CheckCode(scope, eb.body)
}

type DebugBlock struct {
	body []BlockElement
}

func (db *DebugBlock) Render(writer CodeWriter) {

	if len(db.body) == 1 {
		writer.Append("debug ")
		RenderBlockElements(db.body, writer)
	} else {
		// Write out the top of the block
		writer.Append("debug\n")
		writer.Append("{\n")
		writer.PushIndent()

		// Write out the body
		RenderBlockElements(db.body, writer)

		// Write out the bottom of the block
		writer.PopIndent()
		writer.Append("}\n")
	}
}

func (db *DebugBlock) IsBlock() bool {
	return true
}

func (db *DebugBlock) RendersAsBlock() bool {
	if len(db.body) == 1 {
		return db.body[0].RendersAsBlock()
	}
	return true
}

func (db *DebugBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
}

func (db *DebugBlock) CheckCode(scope *Scope) {
	CheckCode(scope, db.body)
}

type AtomicBlock struct {
	body []BlockElement
}

func (db *AtomicBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Append("atomic\n")
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(db.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (db *AtomicBlock) IsBlock() bool {
	return true
}

func (db *AtomicBlock) RendersAsBlock() bool {
	return true
}

func (db *AtomicBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
}

func (db *AtomicBlock) CheckCode(scope *Scope) {
	CheckCode(scope, db.body)
}

type ScheduleBlock struct {
	body []BlockElement
}

func (db *ScheduleBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Append("schedule\n")
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(db.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (db *ScheduleBlock) IsBlock() bool {
	return true
}

func (db *ScheduleBlock) RendersAsBlock() bool {
	return true
}

func (db *ScheduleBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
}

func (db *ScheduleBlock) CheckCode(scope *Scope) {
	CheckCode(scope, db.body)
}

type ScheduleEveryBlock struct {
	interval float32
	body     []BlockElement
}

func (eb *ScheduleEveryBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Appendf("every %f:\n", eb.interval)
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(eb.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (db *ScheduleEveryBlock) IsBlock() bool {
	return true
}

func (db *ScheduleEveryBlock) RendersAsBlock() bool {
	return true
}

func (db *ScheduleEveryBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
}

func (db *ScheduleEveryBlock) CheckCode(scope *Scope) {
	CheckCode(scope, db.body)
}

type WhileLoop struct {
	conditional *Statement
	body        []BlockElement
}

func (wl *WhileLoop) Render(writer CodeWriter) {
	// Write out the top of the block
	writer.Append("while ( ")
	wl.conditional.Render(writer)
	if OUTPUT_ASSEMBLY {
		writer.Append(" ) // ")
		wl.conditional.RenderAssemblyOffsets(writer)
		writer.Append("\n")
	} else {
		writer.Append(" )\n")
	}
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(wl.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (wl *WhileLoop) IsBlock() bool {
	return true
}

func (wl *WhileLoop) RendersAsBlock() bool {
	return true
}

func (wl *WhileLoop) ResolveTypes(scope *Scope) {
	wl.conditional.ResolveTypes(scope)
	ResolveTypes(scope, wl.body)
}

func (wl *WhileLoop) CheckCode(scope *Scope) {
	wl.conditional.CheckCode(scope)
	CheckCode(scope, wl.body)
}

type DoWhileLoop struct {
	conditional *Statement
	body        []BlockElement
}

func (wl *DoWhileLoop) Render(writer CodeWriter) {
	// Write out the top of the block

	writer.Append("do\n")
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(wl.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
	writer.Append("while ( ")
	wl.conditional.Render(writer)
	if OUTPUT_ASSEMBLY {
		writer.Append(" ); // ")
		wl.conditional.RenderAssemblyOffsets(writer)
		writer.Append("\n")
	} else {
		writer.Append(" );\n")
	}
}

func (wl *DoWhileLoop) IsBlock() bool {
	return true
}

func (wl *DoWhileLoop) RendersAsBlock() bool {
	return true
}

func (wl *DoWhileLoop) ResolveTypes(scope *Scope) {
	wl.conditional.ResolveTypes(scope)
	ResolveTypes(scope, wl.body)
}

func (wl *DoWhileLoop) CheckCode(scope *Scope) {
	wl.conditional.CheckCode(scope)
	CheckCode(scope, wl.body)
}

type ForLoop struct {
	init        *Statement
	conditional *Statement
	increment   *Statement
	body        []BlockElement
}

func (fl *ForLoop) Render(writer CodeWriter) {
	// Write out the top of the block
	writer.Append("for ( ")
	fl.init.Render(writer)
	writer.Append("; ")
	fl.conditional.Render(writer)
	writer.Append("; ")
	fl.increment.Render(writer)
	if OUTPUT_ASSEMBLY {
		writer.Append(" ) // ")
		fl.init.RenderAssemblyOffsets(writer)
		writer.Append("; ")
		fl.conditional.RenderAssemblyOffsets(writer)
		writer.Append("; ")
		fl.increment.RenderAssemblyOffsets(writer)
		writer.Append("\n")
	} else {
		writer.Append(" )\n")
	}
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(fl.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (fl *ForLoop) IsBlock() bool {
	return true
}

func (fl *ForLoop) RendersAsBlock() bool {
	return true
}

func (fl *ForLoop) ResolveTypes(scope *Scope) {
	fl.init.ResolveTypes(scope)
	fl.conditional.ResolveTypes(scope)
	fl.increment.ResolveTypes(scope)
	ResolveTypes(scope, fl.body)
}

func (fl *ForLoop) CheckCode(scope *Scope) {
	fl.init.CheckCode(scope)
	fl.conditional.CheckCode(scope)
	fl.increment.CheckCode(scope)
	CheckCode(scope, fl.body)
}

type SwitchBlock struct {
	conditional *Statement
	body        []BlockElement
}

func (sb *SwitchBlock) Render(writer CodeWriter) {
	// Write out the top of the block
	writer.Append("switch ( ")
	if sb.conditional != nil {
		sb.conditional.Render(writer)
	}
	if OUTPUT_ASSEMBLY {
		writer.Append(" ) // ")
		if sb.conditional != nil {
			sb.conditional.RenderAssemblyOffsets(writer)
		}
		writer.Append("\n")
	} else {
		writer.Append(" )\n")
	}
	writer.Append("{\n")
	writer.PushIndent()

	// Write out the body
	RenderBlockElements(sb.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
	writer.Append("}\n")
}

func (sb *SwitchBlock) IsBlock() bool {
	return true
}

func (sb *SwitchBlock) RendersAsBlock() bool {
	return true
}

func (sb *SwitchBlock) ResolveTypes(scope *Scope) {
	if sb.conditional != nil {
		sb.conditional.ResolveTypes(scope)

		if IsEnumType(sb.conditional.graph.typeName) {
			// Get the enum data
			enumData := ENUM_MAP[sb.conditional.graph.typeName]

			for _, child := range sb.body {
				childCase := child.(*CaseBlock)
				if childCase.value != nil {
					code := enumData.valueToName[uint32(*childCase.value)]
					if len(code) > 0 {
						childCase.valueCode = &code
					}
				}
			}
		}
	}
	ResolveTypes(scope, sb.body)
}

func (sb *SwitchBlock) CheckCode(scope *Scope) {
	if sb.conditional != nil {
		sb.conditional.CheckCode(scope)
	}
	CheckCode(scope, sb.body)
}

type CaseBlock struct {
	startingOffset uint32
	jumpLocation   uint32
	value          *int32
	valueCode      *string
	body           []BlockElement
}

func (cb *CaseBlock) Render(writer CodeWriter) {
	// Write out the top of the block
	if cb.value != nil {
		if cb.valueCode != nil {
			writer.Appendf("case %s:", *cb.valueCode)
		} else {
			writer.Appendf("case %d:", *cb.value)
		}
	} else {
		writer.Append("default:")
	}

	if OUTPUT_ASSEMBLY {
		writer.Appendf(" // 0x%08X\n", cb.jumpLocation)
	} else {
		writer.Append("\n")
	}

	writer.PushIndent()

	// Write out the body
	RenderBlockElements(cb.body, writer)

	// Write out the bottom of the block
	writer.PopIndent()
}

func (cb *CaseBlock) IsBlock() bool {
	return true
}

func (cb *CaseBlock) RendersAsBlock() bool {
	return true
}

func (cb *CaseBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, cb.body)
}

func (cb *CaseBlock) CheckCode(scope *Scope) {
	CheckCode(scope, cb.body)
}

func offsetToOpIndex(offset uint32, ops []Operation) int {
	for idx := range ops {
		if ops[idx].offset == offset {
			return idx
		}
	}

	return -1
}

func isIfBlock(idx int, conditionalOffset uint32, ops []Operation) int {
	op := &ops[idx]

	if op.opcode == OP_JUMP_IF_FALSE {
		jumpData := op.data.(ConditionalJumpData)
		if jumpData.offset > op.offset {
			endIdx := offsetToOpIndex(jumpData.offset, ops)

			// Make sure we aren't dealing with a for loop
			if endIdx > 0 {
				lastOp := ops[endIdx-1]
				if lastOp.opcode == OP_JUMP {
					jumpData := lastOp.data.(JumpData)
					if jumpData.offset == conditionalOffset {
						return -1
					}
				}
			}

			return endIdx
		}
	}

	return -1
}

func isForOrWhileLoop(idx int, conditionalOffset uint32, ops []Operation) int {
	op := &ops[idx]

	if op.opcode == OP_JUMP_IF_FALSE {
		jumpData := op.data.(ConditionalJumpData)
		if jumpData.offset > op.offset {
			endIdx := offsetToOpIndex(jumpData.offset, ops)

			// Make sure we are dealing with a loop
			if endIdx > 0 {
				lastOp := ops[endIdx-1]
				if lastOp.opcode == OP_JUMP {
					jumpData := lastOp.data.(JumpData)
					if jumpData.offset == conditionalOffset {
						return endIdx
					}
				}
			}

			return -1
		}
	}

	return -1
}

func shouldUseForLoop(init BlockElement, condition *Statement, increment BlockElement) bool {
	if init == nil || increment == nil {
		return false
	}

	if init.IsBlock() || increment.IsBlock() {
		return false
	}

	initStatement := init.(*Statement)
	incrementStatement := increment.(*Statement)

	// Check to see if one or more variable indices show up in all three
	initVariables := initStatement.graph.GetVariableIndices()
	conditionVariables := condition.graph.GetVariableIndices()
	incrementVariables := incrementStatement.graph.GetVariableIndices()

	intersection := intersect.Simple(intersect.Simple(initVariables, conditionVariables), incrementVariables)

	return len(intersection) > 0
}

func isDoWhileLoop(idx int, maxIdx int, ops []Operation) int {
	for ii := maxIdx; ii > idx; ii-- {
		op := &ops[ii]
		if op.opcode == OP_JUMP_IF_TRUE {
			jumpData := op.data.(ConditionalJumpData)
			if jumpData.offset == ops[idx].offset {
				return ii
			}
		}
	}

	return -1
}

func isDebugBlock(idx int, ops []Operation) int {
	op := &ops[idx]

	if op.opcode == OP_JUMP_IF_NOT_DEBUG {
		jumpData := op.data.(JumpData)
		result := offsetToOpIndex(jumpData.offset, ops)
		if result == -1 {
			fmt.Printf("ERROR: Failed to deal with debug block at offset 0x%08X\n", op.offset)
			os.Exit(1)
		}
		return result
	}

	return -1
}

func isScheduleBlock(idx int, ops []Operation) int {
	op := &ops[idx]
	if op.opcode == OP_SCHEDULE_START {
		targetIdx := idx + 1
		target := &ops[idx+1]

		for target.opcode == OP_SCHEDULE_EVERY {
			everyData := target.data.(ScheduleEveryData)

			targetIdx = offsetToOpIndex(everyData.skipOffset, ops)
			if targetIdx == -1 {
				return -1
			}
			target = &ops[targetIdx]
		}

		return targetIdx
	}

	return -1
}

func isAtomicBlock(idx int, ops []Operation) int {
	lastAtomicStop := -1
	op := &ops[idx]
	if op.opcode == OP_ATOMIC_START {
		atomicCounter := 1

		for ii := idx + 1; ii < len(ops); ii++ {
			op = &ops[ii]
			switch op.opcode {
			case OP_ATOMIC_START:
				if lastAtomicStop != -1 {
					return lastAtomicStop
				}
				atomicCounter++

			case OP_ATOMIC_STOP:
				if atomicCounter > 0 {
					atomicCounter--
				}
				if atomicCounter == 0 {
					lastAtomicStop = ii
				}
			}
		}

		if lastAtomicStop == -1 {
			fmt.Printf("ERROR: Failed to deal with atomic block at offset 0x%08X\n", op.offset)
			os.Exit(2)
		}
	}
	return lastAtomicStop
}

func parseSwitchBlock(scope *Scope, context *BlockContext, condStart int, condEnd int, switchEnd int, cases []*CaseBlock, ops []Operation) *SwitchBlock {
	switchBlock := &SwitchBlock{}

	// Take the conditional ops and append a pop to them so we end up parsing a statement
	conditionalOps := ops[condStart : condEnd+1]
	popOp := Operation{
		opcode: OP_POP_STACK,
		data:   PopStackData{},
	}
	conditionalOps = append(conditionalOps, popOp)

	conditionalStatement := ParseOperations(scope, context, conditionalOps, 0, len(conditionalOps)-1)

	if len(conditionalStatement) == 0 || conditionalStatement[0].IsBlock() {
		fmt.Printf("ERROR: Failed to parse conditional statement for switch at 0x%08X\n", ops[condStart].offset)
		os.Exit(3)
	}

	switchBlock.conditional = conditionalStatement[0].(*Statement)

	for ii := range cases {
		startIdx := offsetToOpIndex(cases[ii].startingOffset, ops)
		if startIdx == -1 {
			fmt.Printf("ERROR: Failed to parse conditional statement for switch at 0x%08X\n", ops[condStart].offset)
			os.Exit(4)
		}

		endIdx := -1
		if ii < len(cases)-1 {
			endIdx = offsetToOpIndex(cases[ii+1].startingOffset, ops)
		} else {
			endIdx = condStart
		}

		if endIdx == -1 {
			fmt.Printf("ERROR: Failed to parse conditional statement for switch at 0x%08X\n", ops[condStart].offset)
			os.Exit(5)
		}

		caseContext := &BlockContext{
			breakOffset:    &ops[switchEnd].offset,
			continueOffset: context.continueOffset,
			currentBlock:   cases[ii],
		}

		body := ParseOperations(scope, caseContext, ops, startIdx, endIdx-1)

		// There is an implicit break jump at the end of the switch we need to remove
		if ii == len(cases)-1 {
			bodyLen := len(body)
			// Do a sanity check
			if !body[bodyLen-1].IsBlock() && body[bodyLen-1].(*Statement).graph.operation.opcode == OP_JUMP {
				// Remove the implicit break
				body = body[:bodyLen-1]
			}
		}
		cases[ii].body = body
		switchBlock.body = append(switchBlock.body, cases[ii])
	}

	return switchBlock
}

func isSwitchBlock(scope *Scope, context *BlockContext, idx int, ops []Operation) (*SwitchBlock, int) {
	op := &ops[idx]
	if op.opcode == OP_JUMP {
		jumpData := op.data.(JumpData)
		// Make sure we are jumping forward
		if jumpData.offset > op.offset {
			condStart := offsetToOpIndex(jumpData.offset, ops)
			if condStart == -1 {
				return nil, -1
			}
			condEnd := -1
			cases := []*CaseBlock{}

			// Loop through and find the first OP_CLONE_STACK without finding any jumps
			for idx := condStart; idx < len(ops); {
				oper := ops[idx]
				if condEnd == -1 {
					switch oper.opcode {
					// If we find some jump before the clone stack, we have stumbled onto a different switch statement
					case OP_JUMP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_JUMP_IF_NOT_DEBUG:
						return nil, -1

					case OP_CLONE_STACK:
						condEnd = idx - 1
					}
					idx++
				} else {
					if oper.opcode == OP_CLONE_STACK {
						idx++
						continue
					}
					if oper.opcode == OP_JUMP {
						jumpData := oper.data.(JumpData)
						// See if we have a jump that takes us between the start of the switch and the end, which would be the default: section
						if jumpData.offset < oper.offset && jumpData.offset > op.offset {
							// Add the default block
							caseBlock := &CaseBlock{
								startingOffset: jumpData.offset,
								jumpLocation:   oper.offset,
							}
							cases = append(cases, caseBlock)

							// We want to skip over this jump later
							return parseSwitchBlock(scope, context, condStart, condEnd, idx+1, cases, ops), idx + 1
						} else {
							// This might be the start of a second switch statement directly below this one
							return parseSwitchBlock(scope, context, condStart, condEnd, idx, cases, ops), idx
						}
					}

					// Break out if there aren't enough operations left
					if len(ops)-idx < 3 {
						if len(cases) > 0 {
							return parseSwitchBlock(scope, context, condStart, condEnd, idx, cases, ops), idx
						}
						return nil, -1
					}

					oper2 := ops[idx+1]
					oper3 := ops[idx+2]

					if !IsLiteralInteger(&oper) || oper2.opcode != OP_INT_EQUALS || oper3.opcode != OP_JUMP_IF_TRUE {
						if len(cases) > 0 {
							return parseSwitchBlock(scope, context, condStart, condEnd, idx, cases, ops), idx
						}
						return nil, -1
					}

					// Add a case block for this
					jumpData := oper3.data.(ConditionalJumpData)
					caseValue := GetLiteralIntegerValue(&oper)
					caseBlock := &CaseBlock{
						startingOffset: jumpData.offset,
						jumpLocation:   oper.offset,
						value:          &caseValue,
					}
					cases = append(cases, caseBlock)

					idx += 3
				}
			}
		}
	}

	return nil, -1
}

func ParseOperations(scope *Scope, context *BlockContext, ops []Operation, minOpIdx int, maxOpIdx int) []BlockElement {
	elements := []BlockElement{}

	// Create the stack
	stack := []*OpGraph{}

	for idx := minOpIdx; idx <= maxOpIdx; idx++ {
		blockEnd := -1
		op := &ops[idx]

		opInfo := OP_MAP[op.opcode]

		// Check for do-while loop
		blockEnd = isDoWhileLoop(idx, maxOpIdx, ops)
		if blockEnd != -1 {
			// Reset the stack
			stack = []*OpGraph{}

			child := &DoWhileLoop{}

			// Change the jump at the end to a pop so that we will get the do-while conditional as the last statement of the loop body below
			ops[blockEnd].opcode = OP_POP_STACK
			ops[blockEnd].data = PopStackData{}

			loopContext := &BlockContext{
				currentBlock:   child,
				continueOffset: &op.offset,
				breakOffset:    &ops[blockEnd+1].offset,
			}

			loopBody := ParseOperations(scope, loopContext, ops, idx, blockEnd)

			// Remove the last statement from the body, that should be our conditional
			child.conditional = loopBody[len(loopBody)-1].(*Statement)
			child.body = loopBody[:len(loopBody)-1]

			elements = append(elements, child)

			// Set our idx to what used to be the jump statement so we can move on to the next statement
			idx = blockEnd
			continue
		}

		// Skip over useless operations
		if opInfo.omit || op.data == nil {
			continue
		}

		// Create a node for this operation
		node := new(OpGraph)
		node.typeName = UNKNOWN_TYPE
		node.operation = op
		node.code = RenderOperationCode(op, scope)

		if op.data.PopCount() > 0 {
			if op.data.PopCount() > len(stack) {
				//fmt.Printf("WARN: Stack underflow at 0x%08X\n", op.offset)
				//os.Exit(2)
				continue
			}
			for ii := 0; ii < op.data.PopCount(); ii++ {
				last := len(stack) - 1
				child := stack[last]
				stack = stack[:last]
				node.children = append(node.children, child)
			}
		}
		if op.data.PushCount() == 1 {
			stack = append(stack, node)
		}

		var statement *Statement = nil

		if len(stack) == 0 && node.ShouldRender() {
			statement = &Statement{
				graph: node,
			}
		}

		// For it to be an if, while, or for block we need to have just finished parsing a conditional statement
		if statement != nil {
			min, _ := statement.graph.GetOffsetRange()

			// Check for if block
			blockEnd = isIfBlock(idx, min, ops)
			if blockEnd != -1 {
				child := &IfBlock{
					conditional: statement,
				}

				blockContext := &BlockContext{
					breakOffset:    context.breakOffset,
					continueOffset: context.continueOffset,
					currentBlock:   child,
				}

				child.body = ParseOperations(scope, blockContext, ops, idx+1, blockEnd-1)

				elements = append(elements, child)

				var lastElement BlockElement = nil

				if len(child.body) != 0 {
					lastElement = child.body[len(child.body)-1]
				}

				if lastElement != nil && !lastElement.IsBlock() && lastElement.(*Statement).graph.IsElseJump() { //if endOp.opcode == OP_JUMP {
					endOp := lastElement.(*Statement).graph.operation
					jumpData := endOp.data.(JumpData)
					// Make sure this isn't a continue/break/return
					//if jumpData.offset > endOp.offset && jumpData.offset != scope.functionEndOffset {
					elseEndIdx := offsetToOpIndex(jumpData.offset, ops)
					if elseEndIdx == -1 {
						fmt.Printf("ERROR: Failed to parse else block at 0x%08X\n", ops[blockEnd].offset)
						os.Exit(3)
					}

					// Remove the implicit jump at the end of the if block
					child.body = child.body[:len(child.body)-1]

					elseChild := &ElseBlock{}

					elseBlockContext := &BlockContext{
						breakOffset:    context.breakOffset,
						continueOffset: context.continueOffset,
						currentBlock:   elseChild,
					}

					elseChild.body = ParseOperations(scope, elseBlockContext, ops, blockEnd, elseEndIdx-1)
					elements = append(elements, elseChild)
					idx = elseEndIdx - 1
					continue
				}

				// Back off by one since it will be incremented above
				idx = blockEnd - 1
				continue
			}

			// Check for for/while loop
			blockEnd = isForOrWhileLoop(idx, min, ops)
			if blockEnd != -1 {
				// Clear out the jump at the end since it has served it's purpose
				ops[blockEnd-1].Remove()

				// See if there is a last element in our current block
				var lastElement BlockElement = nil
				var lastBodyElement BlockElement = nil

				// Get the last element we already processed because it is our init statement
				if len(elements) > 0 {
					lastElement = elements[len(elements)-1]
				}

				min, _ := statement.graph.GetOffsetRange()

				loopContext := &BlockContext{
					breakOffset:    &ops[blockEnd].offset,
					continueOffset: &min,
					currentBlock:   nil, // We can't set the current block because we don't know if we are a for or a while yet
				}

				loopBody := ParseOperations(scope, loopContext, ops, idx+1, blockEnd-1)

				// See if there is a last element in our loop body, it might be an increment statement in a for loop
				if len(loopBody) > 0 {
					lastBodyElement = loopBody[len(loopBody)-1]
				}

				var child BlockElement = nil

				// If they were both statements, check to see if we should use a for loop instead
				if shouldUseForLoop(lastElement, statement, lastBodyElement) {
					// Remove the last element since it is our init statement
					initStatement := elements[len(elements)-1].(*Statement)
					elements = elements[:len(elements)-1]

					// Remove the last body element since it is our increment statement
					incrementStatement := loopBody[len(loopBody)-1].(*Statement)
					loopBody = loopBody[:len(loopBody)-1]

					child = &ForLoop{
						init:        initStatement,
						conditional: statement,
						increment:   incrementStatement,
						body:        loopBody,
					}
				} else {
					child = &WhileLoop{
						conditional: statement,
						body:        loopBody,
					}
				}

				elements = append(elements, child)

				// Back off by one since it will be incremented above
				idx = blockEnd - 1
				continue
			}
		}

		// Check for debug block
		blockEnd = isDebugBlock(idx, ops)
		if blockEnd != -1 {

			child := &DebugBlock{}

			blockContext := &BlockContext{
				breakOffset:    context.breakOffset,
				continueOffset: context.continueOffset,
				currentBlock:   child,
			}

			child.body = ParseOperations(scope, blockContext, ops, idx+1, blockEnd-1)

			elements = append(elements, child)

			// Back off by one since it will be incremented above
			idx = blockEnd - 1
			continue
		}

		// Check for atomic block
		blockEnd = isAtomicBlock(idx, ops)
		if blockEnd != -1 {

			child := &AtomicBlock{}

			blockContext := &BlockContext{
				breakOffset:    context.breakOffset,
				continueOffset: context.continueOffset,
				currentBlock:   child,
			}

			child.body = ParseOperations(scope, blockContext, ops, idx+1, blockEnd-1)

			elements = append(elements, child)

			idx = blockEnd
			continue
		}

		// Check for schedule block
		blockEnd = isScheduleBlock(idx, ops)
		if blockEnd != -1 {
			// Remove the looping jump at the end
			ops[blockEnd].Remove()

			schedule := &ScheduleBlock{
				body: []BlockElement{},
			}

			// Iterate over the "every" blocks
			targetIdx := idx + 1
			target := ops[targetIdx]
			for target.opcode == OP_SCHEDULE_EVERY {
				everyData := target.data.(ScheduleEveryData)

				nextIdx := offsetToOpIndex(everyData.skipOffset, ops)

				everyBlock := &ScheduleEveryBlock{
					interval: everyData.interval,
				}

				everyContext := &BlockContext{
					continueOffset: context.continueOffset,
					breakOffset:    &ops[blockEnd+1].offset,
					currentBlock:   everyBlock,
				}
				everyBlock.body = ParseOperations(scope, everyContext, ops, targetIdx+1, nextIdx-1)

				schedule.body = append(schedule.body, everyBlock)

				targetIdx = nextIdx
				target = ops[targetIdx]
			}

			elements = append(elements, schedule)

			idx = blockEnd
			continue
		}

		// Check for switch block
		switchBlock, blockEnd := isSwitchBlock(scope, context, idx, ops)
		if switchBlock != nil {
			elements = append(elements, switchBlock)

			idx = blockEnd - 1
			continue
		}

		// Check for potential return/break/continue statement
		if op.opcode == OP_JUMP {
			jumpData := op.data.(JumpData)
			if jumpData.offset == scope.functionEndOffset {
				if len(stack) > 0 {
					retString := "return "
					last := len(stack) - 1
					child := stack[last]
					stack = stack[:last]
					node.children = append(node.children, child)
					node.code = &retString
				} else {
					retString := "return"
					node.code = &retString
				}
				statement = &Statement{
					graph: node,
				}
			} else if context.breakOffset != nil && jumpData.offset == *context.breakOffset { //else if jumpData.offset > ops[len(ops)-1].offset {
				breakString := "break"
				node.code = &breakString

				statement = &Statement{
					graph: node,
				}
			} else if context.continueOffset != nil && jumpData.offset == *context.continueOffset { // else if jumpData.offset < ops[0].offset {
				continueString := "continue"
				node.code = &continueString

				statement = &Statement{
					graph: node,
				}
			} else if context.IsCurrentBlockIfBlock() && idx == maxOpIdx && jumpData.offset > op.offset {
				// Flag this as being the jump past the else block
				statement.graph.FlagAsElseJump()
			} else {
				fmt.Printf("ERROR: Unhandled jump at offset 0x%08X\n", op.offset)
				//os.Exit(1)
			}
		}

		if statement != nil {
			elements = append(elements, statement)
		}
	}

	return elements
}

func ResolveTypes(scope *Scope, elements []BlockElement) {
	for idx := range elements {
		elements[idx].ResolveTypes(scope)
	}
}

func CheckCode(scope *Scope, elements []BlockElement) {
	for idx := range elements {
		elements[idx].CheckCode(scope)
	}
}
