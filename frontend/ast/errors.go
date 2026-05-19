package ast

import "fmt"

type CompileError struct {
	Stage string   // "Lexer", "Parser", "Sema", or "Codegen"
	Pos   Position // Standardized position from Issue #8
	Msg   string
}

func (e CompileError) Error() string {
	return fmt.Sprintf("[%s Error] at %d:%d: %s", e.Stage, e.Pos.Line, e.Pos.Col, e.Msg)
}
