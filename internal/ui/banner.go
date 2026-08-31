package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const defaultBannerWidth = 80

var bannerLabel = lipgloss.NewStyle().Foreground(White).Italic(true)
var bannerSubtitle = lipgloss.NewStyle().Foreground(Muted).Italic(true)
var logoLines = normalizedLogoLines()

type Banner struct {
	Version  string
	Subtitle string
	Width    int
}

func RenderBanner(b Banner) string {
	width := b.Width
	if width <= 0 {
		width = defaultBannerWidth
	}

	var out strings.Builder
	out.WriteString(topBorder(b.Version, width) + "\n")
	out.WriteString(blankRow(width))
	for _, line := range logoLines {
		out.WriteString(row(line, width, Accent))
	}
	out.WriteString(blankRow(width))

	if b.Subtitle != "" {
		out.WriteString(divider(width))
		out.WriteString(blankRow(width))
		out.WriteString(row(truncate(b.Subtitle, width-2), width, bannerSubtitle))
		out.WriteString(blankRow(width))
	}

	out.WriteString(frame("╰" + strings.Repeat("─", width) + "╯"))
	return out.String()
}

func topBorder(version string, width int) string {
	labelWidth := lipgloss.Width(version) + 2
	if version == "" || labelWidth > width {
		return frame("╭" + strings.Repeat("─", width) + "╮")
	}
	left := (width - labelWidth) / 2
	right := width - labelWidth - left
	return frame("╭"+strings.Repeat("─", left)) +
		" " + bannerLabel.Render(version) + " " +
		frame(strings.Repeat("─", right)+"╮")
}

func divider(width int) string {
	return frame("├"+strings.Repeat("─", width)+"┤") + "\n"
}

func blankRow(width int) string {
	return frame("│") + strings.Repeat(" ", width) + frame("│") + "\n"
}

func row(content string, width int, style lipgloss.Style) string {
	content = truncate(content, width)
	pad := width - lipgloss.Width(content)
	left := pad / 2
	return frame("│") + strings.Repeat(" ", left) + style.Render(content) +
		strings.Repeat(" ", pad-left) + frame("│") + "\n"
}

func frame(s string) string {
	return Accent.Render(s)
}

func truncate(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	var out []rune
	for _, r := range s {
		if lipgloss.Width(string(append(out, r)))+1 > max {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}

func normalizedLogoLines() []string {
	lines := strings.Split(strings.Trim(logo, "\n"), "\n")
	out := make([]string, 0, len(lines))
	widest := 0
	for _, line := range lines {
		line = strings.TrimPrefix(line, "  ")
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
		out = append(out, line)
	}
	for i, line := range out {
		out[i] = line + strings.Repeat(" ", widest-lipgloss.Width(line))
	}
	return out
}
