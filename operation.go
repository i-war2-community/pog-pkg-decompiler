package main

type Operation struct {
	offset uint32
	opcode byte
	data   OperationData
}

func (op *Operation) WriteAssembly(writer CodeWriter) {
	opInfo := OP_MAP[op.opcode]
	writer.Append(opInfo.name)
	if op.data != nil {
		writer.Appendf(" %s", op.data.String())
	}
}
