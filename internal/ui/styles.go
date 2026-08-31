package ui

import "github.com/charmbracelet/lipgloss"

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

var (
	Bold    = lipgloss.NewStyle().Bold(true)
	Success = lipgloss.NewStyle().Foreground(Green)
	Error   = lipgloss.NewStyle().Foreground(Red)
	Warn    = lipgloss.NewStyle().Foreground(Yellow)
	Info    = lipgloss.NewStyle().Foreground(Gray)
	Accent  = lipgloss.NewStyle().Foreground(Primary)
	Label   = lipgloss.NewStyle().Foreground(Violet)
)
