package main

import (
	"fmt"
	"io"
	"strings"
)

type CodeWriter interface {
	PushIndent()
	PopIndent()
	Append(s string)
	Appendf(format string, a ...interface{})
}

type codeWriter struct {
	ioWrite io.Writer
	indent  int
	last    string
}

func NewCodeWriter(w io.Writer) CodeWriter {
	return &codeWriter{
		ioWrite: w,
	}
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
	fmt.Fprint(cw.ioWrite, s)
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
