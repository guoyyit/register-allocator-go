package ir

import "fmt"

// Error is a locatable input error.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
}

func errorf(line int, format string, args ...any) error {
	return &Error{Line: line, Msg: fmt.Sprintf(format, args...)}
}
