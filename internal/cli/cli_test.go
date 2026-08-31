package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/app"
)

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

type result struct {
	out string
	err string
}

func run(t *testing.T, cwd string, args ...string) (result, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	a := &app.App{
		Version: "v1.2.3",
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.Execute()
	return result{out: out.String(), err: errBuf.String()}, err
}

func TestRootCommandIsNamedLid(t *testing.T) {
	root := NewRootCommand(&app.App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if root.Use != "lid" {
		t.Errorf("Use = %q, want %q", root.Use, "lid")
	}
}

func TestRootWithNoArgsPrintsHelp(t *testing.T) {
	got, err := run(t, t.TempDir())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got.out, "Available Commands") {
		t.Errorf("output missing command list:\n%s", got.out)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	got, err := run(t, t.TempDir(), "version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "lid version v1.2.3"; !strings.Contains(got.out, want) {
		t.Errorf("output = %q, want it to contain %q", got.out, want)
	}
}

func TestVersionFlagMatchesVersionCommand(t *testing.T) {
	fromFlag, err := run(t, t.TempDir(), "--version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	fromCmd, err := run(t, t.TempDir(), "version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fromFlag.out != fromCmd.out {
		t.Errorf("--version = %q, version = %q; want identical", fromFlag.out, fromCmd.out)
	}
}

func TestVersionIncludesCommitAndDateWhenKnown(t *testing.T) {
	var out bytes.Buffer
	a := &app.App{Version: "v1.2.3", Commit: "abc1234", Date: "2026-08-31", Out: &out, Err: &bytes.Buffer{}, Cwd: t.TempDir()}
	root := NewRootCommand(a)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"v1.2.3", "abc1234", "2026-08-31"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
}

func TestConfigCommandPrintsResolvedConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{filepath.Join(dir, "lid.yaml"), "lid", "codex"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output missing %q:\n%s", want, got.out)
		}
	}
}

func TestConfigCommandWritesNothingToStderr(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.err != "" {
		t.Errorf("stderr = %q, want empty", got.err)
	}
}

func TestConfigCommandErrorsWithoutConfig(t *testing.T) {
	if _, err := run(t, t.TempDir(), "config"); err == nil {
		t.Fatal("Execute: want error, got nil")
	}
}

func TestConfigFlagOverridesDiscovery(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: ignored\n")
	write(t, dir, "custom.yaml", "name: chosen\n")

	got, err := run(t, dir, "--config", filepath.Join(dir, "custom.yaml"), "config")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got.out, "chosen") {
		t.Errorf("output = %q, want it to contain %q", got.out, "chosen")
	}
}

func TestUnknownCommandErrors(t *testing.T) {
	if _, err := run(t, t.TempDir(), "nope"); err == nil {
		t.Fatal("Execute: want error for unknown command, got nil")
	}
}
