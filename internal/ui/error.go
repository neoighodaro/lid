package ui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/neoighodaro/lid/internal/failure"
)

// RenderError lays out an error for the terminal: problem, then where lid found
// it, then what to do. One long colon-chained sentence is the hardest form to
// read at the end of a command, so the parts get their own lines and colours.
func RenderError(err error) string {
	return renderError(Err, err)
}

func renderError(s Styles, err error) string {
	var f *failure.Error
	if !errors.As(err, &f) {
		return s.Error.Render("Error:") + " " + s.Strong.Render(err.Error())
	}

	var details []string
	for _, part := range []struct {
		label string
		value string
		style lipgloss.Style
	}{
		{"in ", f.In, s.Accent},
		{"fix", f.Fix, s.Success},
	} {
		if part.value == "" {
			continue
		}
		details = append(details, "  "+s.Faint.Render(part.label)+"  "+part.style.Render(part.value))
	}

	out := s.Error.Render("Error:") + " " + s.Strong.Render(f.What)
	if len(details) > 0 {
		out += "\n\n" + strings.Join(details, "\n")
	}
	return out
}
