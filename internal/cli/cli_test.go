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

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
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

func TestInitCreatesConfigUnderDotConfigLid(t *testing.T) {
	dir := t.TempDir()

	got, err := run(t, dir, "init")
	if err != nil {
		t.Fatalf("init: %v (stderr: %s)", err, got.err)
	}

	path := filepath.Join(dir, ".config", "lid", "lid.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(got.out, path) {
		t.Errorf("output does not name the written path:\n%s", got.out)
	}
}

func TestInitWritesAConfigThatLoads(t *testing.T) {
	dir := t.TempDir()

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("the config init wrote does not load: %v (stderr: %s)", err, got.err)
	}
}

func TestInitDerivesNameFromDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "My_Cool_Project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	body := read(t, filepath.Join(dir, ".config", "lid", "lid.yaml"))
	if !strings.Contains(body, "name: my-cool-project") {
		t.Errorf("config does not carry the sanitized directory name:\n%s", body)
	}
}

func TestInitNameFlagOverridesDirectory(t *testing.T) {
	dir := t.TempDir()

	if _, err := run(t, dir, "init", "--name", "chosen"); err != nil {
		t.Fatalf("init: %v", err)
	}

	body := read(t, filepath.Join(dir, ".config", "lid", "lid.yaml"))
	if !strings.Contains(body, "name: chosen") {
		t.Errorf("config does not use the flag name:\n%s", body)
	}
}

func TestInitRejectsInvalidNameFlag(t *testing.T) {
	dir := t.TempDir()

	if _, err := run(t, dir, "init", "--name", "Not Valid"); err == nil {
		t.Fatal("init: want error for invalid name, got nil")
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "lid", "lid.yaml")); err == nil {
		t.Error("init wrote a config despite the invalid name")
	}
}

func TestInitDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".config/lid/lid.yaml", "name: original\n")

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	if body := read(t, filepath.Join(dir, ".config", "lid", "lid.yaml")); body != "name: original\n" {
		t.Errorf("init overwrote the existing config:\n%s", body)
	}
}

func TestInitDoesNotOverwriteRootConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: original\n")

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".config", "lid", "lid.yaml")); err == nil {
		t.Error("init wrote a second config while a root lid.yaml already exists")
	}
}

func TestInitCreatesKitDirectory(t *testing.T) {
	dir := t.TempDir()

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, ".config", "lid", ".kit"))
	if err != nil {
		t.Fatalf("kit directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("kit path is not a directory")
	}
}

func TestInitIgnoresKitDirectoryInGitRepos(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	body := read(t, filepath.Join(dir, ".gitignore"))
	if !strings.Contains(body, ".config/lid/.kit/") {
		t.Errorf(".gitignore missing the kit entry:\n%s", body)
	}
	if strings.HasPrefix(body, "\n") {
		t.Errorf(".gitignore starts with a blank line:\n%q", body)
	}
}

func TestInitAppendsToExistingGitignoreWithoutDuplicating(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".gitignore", "node_modules\n")

	for i := 0; i < 2; i++ {
		if err := os.RemoveAll(filepath.Join(dir, ".config")); err != nil {
			t.Fatal(err)
		}
		if _, err := run(t, dir, "init"); err != nil {
			t.Fatalf("init: %v", err)
		}
	}

	body := read(t, filepath.Join(dir, ".gitignore"))
	if !strings.Contains(body, "node_modules") {
		t.Errorf(".gitignore lost existing entries:\n%s", body)
	}
	if n := strings.Count(body, ".config/lid/.kit/"); n != 1 {
		t.Errorf("kit entry appears %d times, want 1:\n%s", n, body)
	}
}

func TestInitLeavesNonRepositoriesWithoutAGitignore(t *testing.T) {
	dir := t.TempDir()

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Error("init created a .gitignore outside a git repository")
	}
}

func TestInitErrorsWhenNameCannotBeDerived(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "...")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := run(t, dir, "init")
	if err == nil {
		t.Fatal("init: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("error = %q, want it to suggest --name (stderr: %s)", err.Error(), got.err)
	}
}

func TestConfigCommandShowsBackend(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.Contains(got.out, "backend") || !strings.Contains(got.out, "sbx") {
		t.Errorf("output missing the backend row:\n%s", got.out)
	}
}

func TestConfigCommandOmitsUnsetOptionalRows(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, absent := range []string{"workspace", "template"} {
		if strings.Contains(got.out, absent) {
			t.Errorf("output shows %q even though it is unset:\n%s", absent, got.out)
		}
	}
}

func TestConfigCommandShowsSetOptionalRows(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nworkspace: /workspace\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.Contains(got.out, "/workspace") {
		t.Errorf("output missing the workspace row:\n%s", got.out)
	}
}

func TestConfigCommandSummarizesLists(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\npackages:\n  - jq\n  - ripgrep\nruntimes:\n  node: \"24\"\nnetwork:\n  - api.github.com\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, want := range []string{"jq", "ripgrep", "node 24", "api.github.com"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output missing %q:\n%s", want, got.out)
		}
	}
}
