package main

import (
	"fmt"

	"github.com/juliangruber/go-intersect"
)

type BlockElement interface {
	Render(writer CodeWriter)
	IsBlock() bool
}

type OpGraph struct {
	code      *string
	operation *Operation
	children  []*OpGraph
}

func (og *OpGraph) GetVariableIndices() []uint32 {
	result := []uint32{}

	// Check if we have a variable
	switch og.operation.opcode {
	case OP_VARIABLE_READ:
		data := og.operation.data.(VariableReadData)
		result = append(result, data.index)

	case OP_VARIABLE_WRITE, OP_STRING_VARIABLE_WRITE:
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
		for ii := len(node.children) - 1; ii >= 0; ii-- {
			printGraphNode(node.children[ii], writer, true)
			if ii > 0 {
				writer.Append(", ")
			}
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
			writer.Append(";\n")
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

type DebugBlock struct {
	body []BlockElement
}

func (db *DebugBlock) Render(writer CodeWriter) {

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

func (db *DebugBlock) IsBlock() bool {
	return true
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
					if jumpData.offset < op.offset {
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

			// Make sure we aren't dealing with a for loop
			if endIdx > 0 {
				lastOp := ops[endIdx-1]
				if lastOp.opcode == OP_JUMP {
					jumpData := lastOp.data.(JumpData)
					if jumpData.offset < op.offset {
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

func isDebugBlock(idx int, ops []Operation) int {
	op := &ops[idx]

	if op.opcode == OP_JUMP_IF_NOT_DEBUG {
		jumpData := op.data.(JumpData)
		return offsetToOpIndex(jumpData.offset, ops)
	}

	return -1
}

func ParseOperations(scope Scope, ops []Operation) []BlockElement {
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
		node.operation = op
		node.code = RenderOperationCode(op, scope)

		if op.data.PopCount() > 0 {
			if op.data.PopCount() > len(stack) {
				fmt.Printf("WARN: Stack underflow!")
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
				if jumpData.offset > endOp.offset {
					elseEndIdx := offsetToOpIndex(jumpData.offset, ops)
					elseChild := &ElseBlock{
						body: ParseOperations(scope, ops[blockEnd:elseEndIdx+1]),
					}
					elements = append(elements, elseChild)
					idx = elseEndIdx - 1
					continue
				}
			}

			// Back off by one since it will be incremented above
			idx = blockEnd - 1
			continue
		}

		// Check for for/while loop
		blockEnd = isForOrWhileLoop(idx, ops)
		if blockEnd != -1 {
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

		// TODO: Check for different child block types here

		if statement != nil {
			elements = append(elements, statement)
		}
	}

	return elements
}
