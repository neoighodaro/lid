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
}

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
