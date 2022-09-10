package main

import (
	"fmt"
	"strings"
)

type CodeWriter interface {
	PushIndent()
	PopIndent()
	Append(s string)
	Appendf(format string, a ...interface{})
	String() string
	Bytes() []byte
}

type codeWriter struct {
	sb     strings.Builder
	indent int
	last   string
}

func NewCodeWriter() CodeWriter {
	return &codeWriter{}
}

func (cw *codeWriter) PushIndent() {
	cw.indent++
}

func (cw *codeWriter) PopIndent() {
	cw.indent--
	if cw.indent < 0 {
		cw.indent = 0
	}
}

func (cw *codeWriter) internalAppend(s string) {
	cw.sb.WriteString(s)
	//fmt.Print(s)
	cw.last = s
}

func (cw *codeWriter) handleIndent() {
	if strings.HasSuffix(cw.last, "\n") {
		for ii := 0; ii < cw.indent; ii++ {
			cw.internalAppend("\t")
		}
	}
}

func (cw *codeWriter) Append(s string) {
	cw.handleIndent()
	cw.internalAppend(s)
}

func (cw *codeWriter) Appendf(format string, a ...interface{}) {
	cw.handleIndent()
	cw.internalAppend(fmt.Sprintf(format, a...))
}

func (cw *codeWriter) String() string {
	return cw.sb.String()
}

func (cw *codeWriter) Bytes() []byte {
	return []byte(cw.sb.String())
}
