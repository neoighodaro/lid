package failure

import (
	"errors"
	"fmt"
	"strings"
)

type Error struct {
	What string
	In   string
	Fix  string

	cause error
}

// Because records what this failure stands for, so a caller that knows how to
// handle one particular condition can still recognise it behind the wording.
func (e *Error) Because(cause error) *Error {
	e.cause = cause
	return e
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Error() string {
	if e.Fix == "" {
		return e.What
	}
	return e.What + ": " + e.Fix
}

func New(format string, args ...any) *Error {
	return &Error{What: fmt.Sprintf(format, args...)}
}

func (e *Error) At(path string) *Error {
	e.In = path
	return e
}

func (e *Error) WithFix(format string, args ...any) *Error {
	e.Fix = strings.TrimSpace(fmt.Sprintf(format, args...))
	return e
}

// At names the file err came from, unless it already names one. A validator can
// then stay ignorant of which file it was handed.
func At(err error, path string) error {
	var f *Error
	if errors.As(err, &f) && f.In == "" {
		f.In = path
	}
	return err
}
