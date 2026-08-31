package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/neoighodaro/lid/internal/failure"
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
		"Error":   Err.Error.Render,
		"Warn":    Err.Warn.Render,
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

func TestRenderErrorPutsTheProblemLocationAndFixOnSeparateLines(t *testing.T) {
	out := RenderError(&failure.Error{
		What: "the nono backend cannot use packages",
		In:   ".config/lid/lid.yaml",
		Fix:  "install them on your host, or set backend: sbx",
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("want problem, a blank line, then the two labelled lines, got:\n%s", out)
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("no blank line separating the problem from its details:\n%s", out)
	}
	if strings.TrimSpace(lines[3]) == "" {
		t.Errorf("the details are not adjacent lines:\n%s", out)
	}
	if !strings.Contains(lines[0], "cannot use packages") {
		t.Errorf("first line is not the problem:\n%s", out)
	}
	if strings.Contains(lines[0], ".config/lid/lid.yaml") {
		t.Errorf("the path crowds the problem line:\n%s", out)
	}
	for _, want := range []string{".config/lid/lid.yaml", "install them on your host"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderErrorLabelsTheLocationAndTheFix(t *testing.T) {
	out := RenderError(&failure.Error{What: "boom", In: "lid.yaml", Fix: "duck"})

	for _, want := range []string{"in", "fix"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q label:\n%s", want, out)
		}
	}
}

func TestRenderErrorOmitsPartsThatAreEmpty(t *testing.T) {
	out := RenderError(&failure.Error{What: "no lid.yaml found"})

	if strings.Contains(out, "fix") || strings.Contains(out, "in ") {
		t.Errorf("output invents labels for parts that are missing:\n%s", out)
	}
	if !strings.Contains(out, "no lid.yaml found") {
		t.Errorf("output lost the problem:\n%s", out)
	}
}

func TestRenderErrorHandlesAPlainError(t *testing.T) {
	out := RenderError(errors.New("something broke"))

	if !strings.Contains(out, "something broke") {
		t.Errorf("plain error lost its message:\n%s", out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("a plain error should stay on one line:\n%s", out)
	}
}

func TestRenderErrorFindsAFailureThroughAWrap(t *testing.T) {
	out := RenderError(fmt.Errorf("start: %w", &failure.Error{What: "boom", Fix: "duck"}))

	if !strings.Contains(out, "duck") {
		t.Errorf("wrapped failure rendered without its fix:\n%s", out)
	}
}

func forcedStyles() Styles {
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.TrueColor)
	return newStyles(r)
}

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// paints returns the distinct colour sequences in s, in order of appearance.
func paints(s string) []string {
	var seen []string
	for _, code := range sgrRe.FindAllString(s, -1) {
		if code == "\x1b[0m" || code == "\x1b[m" {
			continue
		}
		if !slices.Contains(seen, code) {
			seen = append(seen, code)
		}
	}
	return seen
}

func TestRenderErrorColoursTheProblemLocationAndFixDifferently(t *testing.T) {
	out := renderError(forcedStyles(), &failure.Error{What: "boom", In: "lid.yaml", Fix: "duck"})

	if len(paints(out)) < 4 {
		t.Fatalf("want the tag, problem, labels and values painted apart, got %d colours:\n%q", len(paints(out)), out)
	}

	lines := strings.Split(out, "\n")
	for _, line := range lines[2:] {
		colours := paints(line)
		if len(colours) < 2 {
			t.Errorf("label and value share a colour on %q", line)
		}
	}
	if in, fix := paints(lines[2]), paints(lines[3]); in[len(in)-1] == fix[len(fix)-1] {
		t.Errorf("the location and the fix are painted the same:\n%q", out)
	}
}

func TestRenderErrorKeepsTheProblemReadableRatherThanAllRed(t *testing.T) {
	out := renderError(forcedStyles(), &failure.Error{What: "boom", In: "lid.yaml"})

	first := strings.Split(out, "\n")[0]
	if len(paints(first)) < 2 {
		t.Errorf("the whole problem line is one colour, so the tag does not stand out:\n%q", first)
	}
}

func TestRenderErrorPaintsAPlainError(t *testing.T) {
	out := renderError(forcedStyles(), errors.New("something broke"))

	if len(paints(out)) == 0 {
		t.Errorf("a plain error rendered without colour:\n%q", out)
	}
}

func TestStderrStylesDetectColourFromStderr(t *testing.T) {
	if Err.stream != os.Stderr {
		t.Error("diagnostic styles do not detect colour from stderr, so redirecting stdout would silence them")
	}
	if Out.stream != os.Stdout {
		t.Error("result styles do not detect colour from stdout")
	}
}
