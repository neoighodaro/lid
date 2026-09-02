package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/neoighodaro/lid/internal/failure"
)

func newTestApp(cwd string) *App {
	return &App{
		Version: "test",
		Out:     &bytes.Buffer{},
		Err:     &bytes.Buffer{},
		Cwd:     cwd,
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigLoadsFromCwd(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	cfg, err := newTestApp(dir).Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Name != "lid" {
		t.Errorf("Name = %q, want %q", cfg.Name, "lid")
	}
}

func TestConfigIsMemoized(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	a := newTestApp(dir)

	first, err := a.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "lid.yaml")); err != nil {
		t.Fatal(err)
	}

	second, err := a.Config()
	if err != nil {
		t.Fatalf("second Config: %v", err)
	}
	if first != second {
		t.Error("Config returned a different value on the second call")
	}
}

func TestConfigMemoizesError(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(dir)

	if _, err := a.Config(); err == nil {
		t.Fatal("Config: want error, got nil")
	}
	write(t, dir, "lid.yaml", "name: lid\n")
	if _, err := a.Config(); err == nil {
		t.Fatal("second Config: want memoized error, got nil")
	}
}

func TestConfigUsesConfigPathOverride(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: ignored\n")
	write(t, dir, "custom.yaml", "name: chosen\n")

	a := newTestApp(dir)
	a.ConfigPath = filepath.Join(dir, "custom.yaml")

	cfg, err := a.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Name != "chosen" {
		t.Errorf("Name = %q, want %q", cfg.Name, "chosen")
	}
}

func TestNewSetsCwdAndStreams(t *testing.T) {
	a, err := New("v1.2.3", "abc123", "2026-08-31")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Cwd == "" {
		t.Error("Cwd is empty")
	}
	if a.Out == nil || a.Err == nil || a.In == nil {
		t.Error("streams not initialised")
	}
	if a.Version != "v1.2.3" || a.Commit != "abc123" || a.Date != "2026-08-31" {
		t.Errorf("build info = %q/%q/%q", a.Version, a.Commit, a.Date)
	}
}

func TestLaunchReportsTheCommandsOwnExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell to exit from")
	}
	a := &App{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}

	err := a.Launch([]string{"sh", "-c", "exit 7"})

	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Launch error is %v, want an *ExitError so lid can exit the same way", err)
	}
	if exit.Code != 7 {
		t.Errorf("Code = %d, want 7", exit.Code)
	}
}

func TestLaunchGivesTheCommandTheAppsStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell to echo from")
	}
	var out bytes.Buffer
	a := &App{In: strings.NewReader("ping\n"), Out: &out, Err: io.Discard}

	if err := a.Launch([]string{"sh", "-c", "cat"}); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if got := out.String(); got != "ping\n" {
		t.Errorf("stdout = %q, want the piped stdin echoed back", got)
	}
}

// TestADirectoryThatStoppedAnsweringSaysSoRatherThanComplainingAboutPermissions
// covers the shell a sandbox has stranded: it can still read files by name, so
// the only sign is that it can no longer say where it is. The bare error names
// no path and blames permissions, which sends whoever reads it after the wrong
// thing entirely.
func TestADirectoryThatStoppedAnsweringSaysSoRatherThanComplainingAboutPermissions(t *testing.T) {
	_, err := workingDir(func() (string, error) { return "", syscall.EPERM })
	if err == nil {
		t.Fatal("a working directory that cannot be read is reported as fine")
	}

	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("the error is not one lid can render a fix for: %v", err)
	}
	if !strings.Contains(f.Fix, "cd into the project again") {
		t.Errorf("the fix does not say what to do about it: %q", f.Fix)
	}
	if strings.Contains(f.Error(), "permission") {
		t.Errorf("the message still blames permissions: %q", f.Error())
	}
	if !errors.Is(err, syscall.EPERM) {
		t.Error("the underlying cause is lost, so nothing can tell this apart from any other failure")
	}
}

func TestNewReportsTheDirectoryItIsRunningIn(t *testing.T) {
	cwd, err := workingDir(func() (string, error) { return "/somewhere", nil })
	if err != nil {
		t.Fatalf("workingDir: %v", err)
	}
	if cwd != "/somewhere" {
		t.Errorf("workingDir = %q, want /somewhere", cwd)
	}
}
