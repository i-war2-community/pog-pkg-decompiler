package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/juliangruber/go-intersect"
)

const UNKNOWN_TYPE = "UNKNOWN"

type BlockElement interface {
	Render(writer CodeWriter)
	IsBlock() bool
	ResolveTypes(scope *Scope)
}

type OpGraph struct {
	code      *string
	operation *Operation
	children  []*OpGraph
	typeName  string
}

func (og *OpGraph) String() string {
	opInfo := OP_MAP[og.operation.opcode]
	if og.operation.data != nil && len(og.operation.data.String()) > 0 {
		return fmt.Sprintf(" %s[%s] ", opInfo.name, og.operation.data.String())
	} else {
		return fmt.Sprintf(" %s ", opInfo.name)
	}
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
	for idx := range og.children {
		og.children[idx].ResolveTypes(scope)
	}

	switch og.operation.opcode {
	case OP_INT_ADD, OP_INT_SUB, OP_INT_MUL, OP_INT_DIV, OP_INT_MOD, OP_CAST_FLT_TO_INT, OP_BITWISE_AND, OP_BITWISE_OR, OP_INT_NEG, OP_LITERAL_BYTE, OP_LITERAL_SHORT, OP_LITERAL_INT:
		og.typeName = "int"

	case OP_FLT_ADD, OP_FLT_SUB, OP_FLT_MUL, OP_FLT_DIV, OP_CAST_INT_TO_FLT, OP_FLT_NEG, OP_LITERAL_FLT:
		og.typeName = "float"

	case OP_LITERAL_STRING:
		og.typeName = "string"

	case OP_FUNCTION_CALL_IMPORTED, OP_TASK_CALL_IMPORTED, OP_FUNCTION_CALL_LOCAL, OP_TASK_CALL_LOCAL:
		funcData := og.operation.data.(FunctionCallData)
		if funcData.declaration.returnTypeName != UNKNOWN_TYPE {
			og.typeName = funcData.declaration.returnTypeName
		}

		if funcData.declaration.parameters != nil && len(*funcData.declaration.parameters) == len(og.children) {
			for ii := range *funcData.declaration.parameters {
				param := &(*funcData.declaration.parameters)[ii]
				child := og.children[len(og.children)-1-ii]

				if param.typeName == UNKNOWN_TYPE {
					continue
				}

				switch child.operation.opcode {
				case OP_LITERAL_ZERO:
					if param.typeName == "bool" {
						boolCode := "false"
						child.code = &boolCode
					}

				case OP_LITERAL_ONE:
					if param.typeName == "bool" {
						boolCode := "true"
						child.code = &boolCode
					}

				case OP_VARIABLE_READ:
					child.typeName = param.typeName
					varData := child.operation.data.(VariableReadData)
					scope.variables[varData.index].possibleTypes[param.typeName] = true
				}
			}
		}

	case OP_LOGICAL_AND, OP_LOGICAL_OR, OP_LOGICAL_NOT:
		og.typeName = "bool"

	case OP_INT_EQUALS, OP_INT_NOT_EQUALS, OP_INT_GT, OP_INT_LT, OP_INT_GT_EQUALS, OP_INT_LT_EQUALS:
		og.typeName = "bool"

	case OP_FLT_GT, OP_FLT_LT, OP_FLT_GT_EQUALS, OP_FLT_LT_EQUALS:
		og.typeName = "bool"

	case OP_STRING_EQUALS:
		og.typeName = "bool"

	case OP_VARIABLE_READ:
		varData := og.operation.data.(VariableReadData)
		if scope.variables[varData.index].typeName != UNKNOWN_TYPE {
			og.typeName = scope.variables[varData.index].typeName
		}

	case OP_VARIABLE_WRITE, OP_HANDLE_VARIABLE_WRITE:
		varData := og.operation.data.(VariableWriteData)
		// Add to the variable's set count if this isn't just from a handle init
		if og.children[0].operation.opcode != OP_HANDLE_INIT {
			scope.variables[varData.index].setCount++
		}

		// Copy over the type of our first child
		childType := og.children[0].typeName

		switch og.children[0].operation.opcode {
		case OP_LITERAL_ZERO, OP_LITERAL_ONE:
			childType = "bool"
			// If we are assigning literal true or literal false
			if scope.variables[varData.index].typeName == "bool" {
				boolStr := "false"
				if og.children[0].operation.opcode == OP_LITERAL_ONE {
					boolStr = "true"
				}

				og.children[0].code = &boolStr
			}
		}

		og.typeName = childType
		if og.typeName != UNKNOWN_TYPE {
			scope.variables[varData.index].possibleTypes[og.typeName] = true
		}

	case OP_JUMP:
		if og.code != nil && strings.HasPrefix(*og.code, "return") {
			if scope.function.returnTypeName == UNKNOWN_TYPE {
				returnType := ""
				if len(og.children) > 0 {
					returnType = og.children[0].typeName
				}
				if returnType != UNKNOWN_TYPE {
					scope.function.possibleReturnTypes[returnType] = true
				}
			}
		}

	case OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
		// If we have a variable read inside an if statement, we might have a bool
		if len(og.children) == 1 && og.children[0].operation.opcode == OP_VARIABLE_READ {
			child := og.children[0]
			varData := child.operation.data.(VariableReadData)
			scope.variables[varData.index].possibleTypes["bool"] = true
		}

	default:
		if len(og.children) > 0 {
			og.typeName = og.children[0].typeName
		}
	}
}

func printGraphNode(node *OpGraph, writer CodeWriter, onlyChild bool) {

	if len(node.children) == 2 && !IsFunctionCall(node.operation) {
		if !onlyChild {
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
		if !onlyChild {
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

func (s *Statement) IsBlock() bool {
	return false
}

func (s *Statement) ResolveTypes(scope *Scope) {
	s.graph.ResolveTypes(scope)
}

func shouldHaveNewlineBetween(element1 BlockElement, element2 BlockElement) bool {
	if element1.IsBlock() && element2.IsBlock() {
		_, isIf := element1.(*IfBlock)
		_, isElse := element2.(*ElseBlock)
		return !(isIf && isElse)
	}
	if !element1.IsBlock() && !element2.IsBlock() {
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
				writer.Append("; ")
				s := e.(*Statement)
				min, max := s.graph.GetOffsetRange()
				if min != max {
					writer.Appendf("// 0x%08X - 0x%08X\n", min, max)
				} else {
					writer.Appendf("// 0x%08X\n", min)
				}
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
	writer.Append(" )\n")
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

func (ib *IfBlock) ResolveTypes(scope *Scope) {
	ib.conditional.ResolveTypes(scope)
	ResolveTypes(scope, ib.body)
}

type ElseBlock struct {
	body []BlockElement
}

func (eb *ElseBlock) Render(writer CodeWriter) {

	// Write out the top of the block
	writer.Append("else")

	inline := false

	if len(eb.body) == 1 && eb.body[0].IsBlock() {
		inline = true
	}
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

func (eb *ElseBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, eb.body)
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

func (db *DebugBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
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

func (db *AtomicBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
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

func (db *ScheduleBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
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

func (db *ScheduleEveryBlock) ResolveTypes(scope *Scope) {
	ResolveTypes(scope, db.body)
}

type WhileLoop struct {
	conditional *Statement
	body        []BlockElement
}

func (wl *WhileLoop) Render(writer CodeWriter) {
	// Write out the top of the block
	writer.Append("while ( ")
	wl.conditional.Render(writer)
	writer.Append(" )\n")
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

func (wl *WhileLoop) ResolveTypes(scope *Scope) {
	wl.conditional.ResolveTypes(scope)
	ResolveTypes(scope, wl.body)
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
	writer.Append("} while ( ")
	wl.conditional.Render(writer)
	writer.Append(" );\n")
}

func (wl *DoWhileLoop) IsBlock() bool {
	return true
}

func (wl *DoWhileLoop) ResolveTypes(scope *Scope) {
	wl.conditional.ResolveTypes(scope)
	ResolveTypes(scope, wl.body)
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
	writer.Append(" )\n")
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

func (fl *ForLoop) ResolveTypes(scope *Scope) {
	fl.init.ResolveTypes(scope)
	fl.conditional.ResolveTypes(scope)
	fl.increment.ResolveTypes(scope)
	ResolveTypes(scope, fl.body)
}

func offsetToOpIndex(offset uint32, ops []Operation) int {
	for idx := range ops {
		if ops[idx].offset == offset {
			return idx
		}
	}

	return -1
}

func isIfBlock(idx int, ops []Operation) int {
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
					if jumpData.offset < op.offset && jumpData.offset >= ops[0].offset {
						return -1
					}
				}
			}

			return endIdx
		}
	}

	return -1
}

func isForOrWhileLoop(idx int, ops []Operation) int {
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
					if jumpData.offset < op.offset && jumpData.offset >= ops[0].offset {
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

func isDoWhileLoop(idx int, ops []Operation) int {
	for ii := len(ops) - 1; ii > idx; ii-- {
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
			fmt.Printf("Failed to deal with debug block")
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
	}
	return lastAtomicStop
}

func ParseOperations(scope *Scope, ops []Operation) []BlockElement {
	elements := []BlockElement{}

	// Create the stack
	stack := []*OpGraph{}

	for idx := 0; idx < len(ops); idx++ {
		op := &ops[idx]

		opInfo := OP_MAP[op.opcode]

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

		// Check for potential return statement
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
			} else if jumpData.offset > ops[len(ops)-1].offset {
				// If we are jumping beyond our current block, this should be a break statement
				breakString := "break"
				node.code = &breakString

				statement = &Statement{
					graph: node,
				}
			} else if jumpData.offset < ops[0].offset {
				// If we are jumping before our current block, it must be a continue statement
				continueString := "continue"
				node.code = &continueString

				statement = &Statement{
					graph: node,
				}
			} else {
				fmt.Printf("ERROR: Unhandled jump at offset 0x%08X\n", op.offset)
				os.Exit(1)
			}
		} else if len(stack) == 0 && shouldRenderStatement(node) {
			statement = &Statement{
				graph: node,
			}
		}
		blockEnd := -1

		// Check for if block
		blockEnd = isIfBlock(idx, ops)
		if blockEnd != -1 {
			child := &IfBlock{
				conditional: statement,
				body:        ParseOperations(scope, ops[idx+1:blockEnd]),
			}

			elements = append(elements, child)

			endOp := &ops[blockEnd-1]

			if endOp.opcode == OP_JUMP {
				jumpData := endOp.data.(JumpData)
				// Make sure this isn't a continue/break/return
				if jumpData.offset > endOp.offset && jumpData.offset != scope.functionEndOffset {
					elseEndIdx := offsetToOpIndex(jumpData.offset, ops)
					if elseEndIdx != -1 {
						ops[blockEnd-1].opcode = OP_REMOVED
						// Re-parse the operations excluding the jump at the end since it is for the else block
						child.body = ParseOperations(scope, ops[idx+1:blockEnd])

						elseChild := &ElseBlock{
							body: ParseOperations(scope, ops[blockEnd:elseEndIdx+1]),
						}
						elements = append(elements, elseChild)
						idx = elseEndIdx - 1
						continue
					}
				}
			}

			// Back off by one since it will be incremented above
			idx = blockEnd - 1
			continue
		}

		// Check for for/while loop
		blockEnd = isForOrWhileLoop(idx, ops)
		if blockEnd != -1 {
			// Clear out the jump at the end since it has served it's purpose
			ops[blockEnd-1].opcode = OP_REMOVED

			// See if there is a last element in our current block
			var lastElement BlockElement = nil
			var lastBodyElement BlockElement = nil

			if len(elements) > 0 {
				lastElement = elements[len(elements)-1]
			}

			loopBody := ParseOperations(scope, ops[idx+1:blockEnd])

			// See if there is a last element in our loop body
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

		// Check for do-while loop
		blockEnd = isDoWhileLoop(idx, ops)
		if blockEnd != -1 {
			// Reset the stack
			stack = []*OpGraph{}

			// We need to modify the opcode so we don't infinitely find do-while loops
			ops[blockEnd].opcode = OP_POP_STACK
			ops[blockEnd].data = PopStackData{}

			loopBody := ParseOperations(scope, ops[idx:blockEnd+1])

			// Remove the last statement from the body, that should be our conditional
			conditional := loopBody[len(loopBody)-1].(*Statement)
			loopBody = loopBody[:len(loopBody)-1]

			child := &DoWhileLoop{
				conditional: conditional,
				body:        loopBody,
			}

			elements = append(elements, child)

			idx = blockEnd
			continue
		}

		// Check for debug block
		blockEnd = isDebugBlock(idx, ops)
		if blockEnd != -1 {
			child := &DebugBlock{
				body: ParseOperations(scope, ops[idx+1:blockEnd]),
			}

			elements = append(elements, child)

			// Back off by one since it will be incremented above
			idx = blockEnd - 1
			continue
		}

		// Check for atomic block
		blockEnd = isAtomicBlock(idx, ops)
		if blockEnd != -1 {
			ops[blockEnd].opcode = OP_REMOVED

			child := &AtomicBlock{
				body: ParseOperations(scope, ops[idx+1:blockEnd+1]),
			}

			elements = append(elements, child)

			idx = blockEnd
			continue
		}

		// Check for schedule block
		blockEnd = isScheduleBlock(idx, ops)
		if blockEnd != -1 {
			// Remove the looping jump at the end
			ops[blockEnd].opcode = OP_REMOVED

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
					body:     ParseOperations(scope, ops[targetIdx+1:nextIdx]),
				}

				schedule.body = append(schedule.body, everyBlock)

				targetIdx = nextIdx
				target = ops[targetIdx]
			}

			elements = append(elements, schedule)

			idx = blockEnd
			continue
		}

		// TODO: Check for different child block types here

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
