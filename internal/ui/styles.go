package ui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	Purple  = lipgloss.Color("#A855F7")
	Violet  = lipgloss.Color("#C084FC")
	Green   = lipgloss.Color("#A8DB8F")
	Red     = lipgloss.Color("#FF6B6B")
	Yellow  = lipgloss.Color("#FBBF24")
	Gray    = lipgloss.Color("#6B7280")
	Muted   = lipgloss.Color("#9CA3AF")
	White   = lipgloss.Color("#F9FAFB")
	Dim     = lipgloss.Color("#4B5563")
	Primary = lipgloss.Color("#E8734A")
)

// Styles is a palette bound to one stream. Colour support is detected per
// stream, so a redirected stdout cannot silence the diagnostics on stderr.
type Styles struct {
	stream io.Writer

	Bold    lipgloss.Style
	Success lipgloss.Style
	Error   lipgloss.Style
	Warn    lipgloss.Style
	Info    lipgloss.Style
	Accent  lipgloss.Style
	Label   lipgloss.Style
	Faint   lipgloss.Style
	Strong  lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) Styles {
	return Styles{
		Bold:    r.NewStyle().Bold(true),
		Success: r.NewStyle().Foreground(Green),
		Error:   r.NewStyle().Foreground(Red).Bold(true),
		Warn:    r.NewStyle().Foreground(Yellow),
		Info:    r.NewStyle().Foreground(Gray),
		Accent:  r.NewStyle().Foreground(Primary),
		Label:   r.NewStyle().Foreground(Violet),
		Faint:   r.NewStyle().Foreground(Dim),
		Strong:  r.NewStyle().Foreground(White).Bold(true),
	}
}

func streamStyles(w *os.File) Styles {
	styles := newStyles(lipgloss.NewRenderer(w))
	styles.stream = w
	return styles
}

// Out styles command results; Err styles diagnostics.
var (
	Out = streamStyles(os.Stdout)
	Err = streamStyles(os.Stderr)
)

var (
	Bold    = Out.Bold
	Success = Out.Success
	Info    = Out.Info
	Accent  = Out.Accent
	Label   = Out.Label
)
