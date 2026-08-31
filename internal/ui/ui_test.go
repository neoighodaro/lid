package ui

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"
)

func TestRenderLogoIsSixLines(t *testing.T) {
	got := strings.Split(strings.TrimRight(RenderLogo(), "\n"), "\n")
	if len(got) != 6 {
		t.Fatalf("RenderLogo() has %d lines, want 6", len(got))
	}
	for i, line := range got {
		if strings.TrimSpace(line) == "" {
			t.Errorf("line %d is blank", i)
		}
	}
}

func TestStylesPreserveContent(t *testing.T) {
	for name, render := range map[string]func(...string) string{
		"Success": Success.Render,
		"Error":   Error.Render,
		"Warn":    Warn.Render,
		"Info":    Info.Render,
		"Accent":  Accent.Render,
		"Bold":    Bold.Render,
	} {
		if got := render("hello"); !strings.Contains(got, "hello") {
			t.Errorf("%s.Render(%q) = %q, content lost", name, "hello", got)
		}
	}
}

func bannerLines(t *testing.T, b Banner) []string {
	t.Helper()
	return strings.Split(strings.TrimRight(RenderBanner(b), "\n"), "\n")
}

func TestRenderBannerLinesAreAllEqualWidth(t *testing.T) {
	for _, b := range []Banner{
		{Version: "v0.1.0", Subtitle: "Declarative sandbox manager"},
		{},
		{Version: "v1.2.3-rc1+build.99"},
		{Subtitle: "x"},
		{Version: "v1", Subtitle: "Declarative sandbox manager", Width: 40},
		{Version: "v1", Subtitle: "Declarative sandbox manager", Width: 90},
	} {
		lines := bannerLines(t, b)
		want := lipgloss.Width(lines[0])
		for i, line := range lines {
			if got := lipgloss.Width(line); got != want {
				t.Errorf("Banner%+v line %d width = %d, want %d\n%s", b, i, got, want, line)
			}
		}
	}
}

func TestRenderBannerWidthIncludesBorders(t *testing.T) {
	lines := bannerLines(t, Banner{Width: 50, Version: "v1", Subtitle: "hi"})
	if got := lipgloss.Width(lines[0]); got != 52 {
		t.Errorf("width = %d, want 52 (50 inner + 2 borders)", got)
	}
}

func TestRenderBannerShowsVersionInTopBorder(t *testing.T) {
	lines := bannerLines(t, Banner{Version: "v9.9.9"})
	if !strings.Contains(lines[0], "v9.9.9") {
		t.Errorf("top border = %q, want it to contain the version", lines[0])
	}
}

func TestRenderBannerTopBorderIsUnbrokenWithoutVersion(t *testing.T) {
	lines := bannerLines(t, Banner{Width: 20})
	if want := "╭" + strings.Repeat("─", 20) + "╮"; lines[0] != want {
		t.Errorf("top border = %q, want %q", lines[0], want)
	}
}

func TestRenderBannerHasRoundedCorners(t *testing.T) {
	lines := bannerLines(t, Banner{Version: "v1"})
	last := lines[len(lines)-1]
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") {
		t.Errorf("top = %q, want ╭…╮", lines[0])
	}
	if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Errorf("bottom = %q, want ╰…╯", last)
	}
}

func TestRenderBannerCentersSubtitle(t *testing.T) {
	const subtitle = "Declarative sandbox manager"
	var found bool
	for _, line := range bannerLines(t, Banner{Width: 52, Subtitle: subtitle}) {
		if !strings.Contains(line, subtitle) {
			continue
		}
		found = true
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│")
		left := len(inner) - len(strings.TrimLeft(inner, " "))
		right := len(inner) - len(strings.TrimRight(inner, " "))
		if diff := left - right; diff > 1 || diff < -1 {
			t.Errorf("subtitle padding left=%d right=%d, want centered", left, right)
		}
	}
	if !found {
		t.Fatalf("subtitle %q missing from banner", subtitle)
	}
}

func TestRenderBannerTruncatesSubtitleThatWouldOverflow(t *testing.T) {
	long := strings.Repeat("verbose", 20)
	lines := bannerLines(t, Banner{Width: 30, Subtitle: long})
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 32 {
			t.Errorf("line %d width = %d, want 32; long subtitle broke the box\n%s", i, got, line)
		}
	}
	if !strings.Contains(RenderBanner(Banner{Width: 30, Subtitle: long}), "…") {
		t.Error("want an ellipsis marking the truncation")
	}
}

func TestRenderBannerOmitsSubtitleSectionWhenEmpty(t *testing.T) {
	withSubtitle := bannerLines(t, Banner{Subtitle: "hi"})
	without := bannerLines(t, Banner{})
	if len(without) >= len(withSubtitle) {
		t.Errorf("banner without subtitle has %d lines, want fewer than %d", len(without), len(withSubtitle))
	}
	for _, line := range without {
		if strings.Contains(line, "├") || strings.Contains(line, "┤") {
			t.Errorf("unexpected divider with no subtitle: %q", line)
		}
	}
}

func TestRenderBannerContainsLogo(t *testing.T) {
	got := RenderBanner(Banner{})
	for _, line := range strings.Split(strings.TrimRight(RenderLogo(), "\n"), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("banner missing logo line %q", line)
		}
	}
}
