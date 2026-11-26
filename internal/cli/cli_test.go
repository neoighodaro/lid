package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/handoff"
	"github.com/neoighodaro/lid/internal/herdr"
	"github.com/neoighodaro/lid/internal/update"
	"github.com/neoighodaro/lid/internal/zellij"
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

// lid config is where you check what lid made of the file, so a key that is
// read but never shown sends you back to guessing.
func TestConfigCommandShowsEveryKeyItRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "DB_PASSWORD=hunter2\n")
	write(t, dir, "lid.yaml", strings.Join([]string{
		"name: demo",
		"workspace: /workspace",
		"clone: true",
		"runtimes:\n  php: \"8.5\"",
		"php:\n  extensions: [imagick]",
		"ports: [\"8080:80\"]",
		"env:\n  API_TOKEN: sk-live-secret",
		"env_file: [.env]",
		"network: [api.github.com]",
		"scripts:\n  on_create: [composer install]\n  on_start: [./warm.sh]",
		"handoff:\n  enabled: true",
		"bridges:\n  - name: postgres\n    port: 5432\n    host_port: 15432\n    env: DB_HOST",
		"shell:\n  directory: /workspace\n  aliases: {g: git}",
		"claude:\n  theme: dark\n  plugins:\n    marketplaces: [org/market]\n    install: [thing@market]\n  settings:\n    model: claude-opus-5",
		"zellij:\n  display_name: Demo",
		"memory: |\n  line one\n  line two",
		"",
	}, "\n"))

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, got.err)
	}
	for _, want := range []string{
		"workspace", "clone", "php.extensions", "ports", "env_file", "network",
		"scripts.on_create", "scripts.on_start", "handoff", "bridges",
		"shell.directory", "shell.aliases", "claude.theme",
		"claude.marketplaces", "claude.install", "claude.settings",
		"zellij", "memory",
	} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output never mentions %q:\n%s", want, got.out)
		}
	}
}

func TestConfigCommandShowsTheKeysOnlyNonoHas(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: demo\nbackend: nono\nnetwork_profile: developer\nallow_commands: [npm]\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, got.err)
	}
	for _, want := range []string{"network_profile", "developer", "allow_commands", "npm"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output never mentions %q:\n%s", want, got.out)
		}
	}
}

// Naming the variables is the useful part; printing what they hold would put
// whatever env_file resolves on the terminal.
func TestConfigCommandNamesEnvironmentVariablesWithoutTheirValues(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "DB_PASSWORD=hunter2\n")
	write(t, dir, "lid.yaml", "name: demo\nenv:\n  API_TOKEN: sk-live-secret\nenv_file: [.env]\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, got.err)
	}
	if !strings.Contains(got.out, "API_TOKEN") {
		t.Errorf("output never names API_TOKEN:\n%s", got.out)
	}
	for _, secret := range []string{"sk-live-secret", "hunter2"} {
		if strings.Contains(got.out, secret) {
			t.Errorf("output leaks %q:\n%s", secret, got.out)
		}
	}
}

func TestConfigCommandLeavesOutWhatTheFileDoesNotSet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: demo\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, absent := range []string{"handoff", "bridges", "memory", "shell.", "claude."} {
		if strings.Contains(got.out, absent) {
			t.Errorf("output mentions %q for a config that does not set it:\n%s", absent, got.out)
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
	for _, absent := range []string{"workspace", "template", "packages", "runtimes", "ports", "network"} {
		if hasRow(got.out, absent) {
			t.Errorf("output shows the %q row even though it is unset:\n%s", absent, got.out)
		}
	}
}

// hasRow looks for a label at the start of a line, so a value that happens to
// contain a label name (the default base image contains "templates") cannot
// pass for the row itself.
func hasRow(out, label string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), label) {
			return true
		}
	}
	return false
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

func TestConfigCommandWarnsAboutUnverifiedAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.Contains(got.err, "codex") {
		t.Errorf("stderr does not name the unverified agent:\n%s", got.err)
	}
	if !strings.Contains(got.err, "verified") {
		t.Errorf("stderr does not say the agent is unverified:\n%s", got.err)
	}
}

func TestAgentWarningStaysOffStdout(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if strings.Contains(got.out, "not verified") {
		t.Errorf("the warning leaked into stdout:\n%s", got.out)
	}
}

type execRecorder struct {
	calls [][]string
	err   error
}

func (r *execRecorder) exec(argv []string, _, _ io.Writer) error {
	r.calls = append(r.calls, argv)
	return r.err
}

func runBuild(t *testing.T, cwd string, rec *execRecorder, args ...string) (result, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	a := &app.App{
		Version: "v1.2.3",
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
		Exec:    rec.exec,
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.Execute()
	return result{out: out.String(), err: errBuf.String()}, err
}

func TestBuildPrintWritesTheDockerfileToStdout(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nruntimes:\n  node: 22\n")
	rec := &execRecorder{}

	res, err := runBuild(t, dir, rec, "build", "--print")
	if err != nil {
		t.Fatalf("build --print: %v\nstderr: %s", err, res.err)
	}

	if !strings.HasPrefix(res.out, "FROM ") {
		t.Errorf("stdout is not a Dockerfile:\n%s", res.out)
	}
	if len(rec.calls) != 0 {
		t.Errorf("--print ran %v, want nothing", rec.calls)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".config", "lid", ".kit", "Dockerfile")); statErr == nil {
		t.Error("--print wrote a Dockerfile into the kit dir")
	}
}

func TestBuildRunsDockerBuild(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	rec := &execRecorder{}

	res, err := runBuild(t, dir, rec, "build")
	if err != nil {
		t.Fatalf("build: %v\nstderr: %s", err, res.err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("ran %d commands, want 1: %v", len(rec.calls), rec.calls)
	}
	if got := strings.Join(rec.calls[0], " "); !strings.HasPrefix(got, "docker build --pull -t lid-sandbox ") {
		t.Errorf("argv = %q, want a docker build of lid-sandbox", got)
	}
	if read(t, filepath.Join(dir, ".config", "lid", ".kit", "Dockerfile")) == "" {
		t.Error("no Dockerfile was written into the kit dir")
	}
}

func TestBuildReportsSuccessOnStdout(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	res, err := runBuild(t, dir, &execRecorder{}, "build")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(res.out, "lid-sandbox") {
		t.Errorf("stdout does not name the built image:\n%s", res.out)
	}
}

func TestBuildFailsWhenDockerFails(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	rec := &execRecorder{err: errors.New("exit status 1")}

	if _, err := runBuild(t, dir, rec, "build"); err == nil {
		t.Fatal("build: want error, got nil")
	}
}

func TestBuildErrorsWithoutAConfig(t *testing.T) {
	rec := &execRecorder{}

	_, err := runBuild(t, t.TempDir(), rec, "build")
	if err == nil {
		t.Fatal("build: want error, got nil")
	}
	if !strings.Contains(err.Error(), "lid init") {
		t.Errorf("error = %v, want it to point at lid init", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("ran %v without a config", rec.calls)
	}
}

func TestBuildWarnsForAnUnverifiedAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")

	res, err := runBuild(t, dir, &execRecorder{}, "build")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(res.err, "not verified") {
		t.Errorf("stderr does not warn about the agent:\n%s", res.err)
	}
}

func TestBuildWarnsThatClaudeConfigIsIgnoredForOtherAgents(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\nclaude:\n  theme: dark\n")

	res, err := runBuild(t, dir, &execRecorder{}, "build")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(res.err, "claude") {
		t.Errorf("stderr does not warn that claude config is ignored:\n%s", res.err)
	}
}

func TestBuildStaysQuietAboutClaudeConfigForTheClaudeAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nclaude:\n  theme: dark\n")

	res, err := runBuild(t, dir, &execRecorder{}, "build")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(res.err, "ignoring") {
		t.Errorf("stderr warns for the verified agent:\n%s", res.err)
	}
}

func TestBuildRefusesABackendThatBuildsNoImage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nbackend: nono\n")
	rec := &execRecorder{}

	_, err := runBuild(t, dir, rec, "build")
	if err == nil {
		t.Fatal("build: want error, got nil")
	}
	if !strings.Contains(err.Error(), "nono") {
		t.Errorf("error = %v, want it to name the backend", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("ran %v for a backend with no image", rec.calls)
	}
}

func TestBuildRefusesACustomTemplate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\ntemplate: my-template\n")

	_, err := runBuild(t, dir, &execRecorder{}, "build")
	if err == nil {
		t.Fatal("build: want error, got nil")
	}
	if !strings.Contains(err.Error(), "my-template") {
		t.Errorf("error = %v, want it to name the template", err)
	}
}

func TestStartPrintRendersTheNonoProfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\n")

	got, err := run(t, dir, "start", "--print")
	if err != nil {
		t.Fatalf("start --print: %v\n%s", err, got.err)
	}
	for _, want := range []string{`"extends": "nolabs-ai/claude"`, `"name": "api"`, `"network_profile": "claude-code"`} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output missing %q:\n%s", want, got.out)
		}
	}
}

func TestStartPrintWarnsOnStderrNotStdout(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\nmemory: be nice\n")

	got, err := run(t, dir, "start", "--print")
	if err != nil {
		t.Fatalf("start --print: %v", err)
	}
	if !strings.Contains(got.err, "memory") {
		t.Errorf("stderr does not warn about the ignored key:\n%s", got.err)
	}
	if strings.Contains(got.out, "memory") {
		t.Errorf("a warning leaked into the profile on stdout:\n%s", got.out)
	}
}

func TestStartPrintShowsTheImageAndThePlanForSbx(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")

	got, err := run(t, dir, "start", "--print")
	if err != nil {
		t.Fatalf("start --print: %v", err)
	}
	for _, want := range []string{"FROM ", "sbx create", "sbx run --name api"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output missing %q:\n%s", want, got.out)
		}
	}
}

func TestStartRefusesAnAgentNonoCannotSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\nagent: gemini\n")

	_, err := run(t, dir, "start", "--print")
	if err == nil {
		t.Fatal("start --print: want an error for an unsupported agent")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Errorf("error = %v, want it to name the agent", err)
	}
}

// startHost stands in for a machine with nono installed, recording the probes
// lid runs and the command it finally hands the terminal to.
type startHost struct {
	missing bool
	probes  [][]string
	argv    []string
	exit    error
}

func (h *startHost) lookPath(name string) (string, error) {
	if h.missing {
		return "", errors.New("executable file not found in $PATH")
	}
	return "/opt/homebrew/bin/" + name, nil
}

func (h *startHost) output(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	if len(argv) > 1 && argv[1] == "--version" {
		return []byte("nono 0.74.0\n"), nil
	}
	return []byte("Result: valid\n"), nil
}

func (h *startHost) attach(argv []string, _ io.Reader, _, _ io.Writer) error {
	h.argv = argv
	return h.exit
}

func runStart(t *testing.T, cwd string, h *startHost, args ...string) (result, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	a := &app.App{
		Version: "v1.2.3",
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
		Host:    backend.Host{LookPath: h.lookPath, Output: h.output},
		Attach:  h.attach,
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.Execute()
	return result{out: out.String(), err: errBuf.String()}, err
}

func nonoProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nbackend: nono\n")
	return dir
}

func TestStartProvisionsTheProfileThenLaunchesTheAgent(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{}

	res, err := runStart(t, dir, h, "start")
	if err != nil {
		t.Fatalf("start: %v\nstderr: %s", err, res.err)
	}

	profile := filepath.Join(dir, ".config", "lid", ".kit", "nono.jsonc")
	if read(t, profile) == "" {
		t.Fatalf("no profile written to %s", profile)
	}
	got := strings.Join(h.argv, " ")
	if !strings.Contains(got, "nono run --profile "+profile) {
		t.Errorf("launched %q, want a nono run against the profile it wrote", got)
	}
	if !strings.Contains(got, "-- claude") {
		t.Errorf("launched %q, want it to run the agent", got)
	}
}

func TestStartRefusesToProvisionWithoutNonoInstalled(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{missing: true}

	res, err := runStart(t, dir, h, "start")
	if err == nil {
		t.Fatal("start succeeded with no nono installed")
	}

	if !strings.Contains(err.Error(), "nono") {
		t.Errorf("error does not mention nono: %v", err)
	}
	if h.argv != nil {
		t.Errorf("launched %v anyway", h.argv)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".config", "lid", ".kit", "nono.jsonc")); statErr == nil {
		t.Error("wrote a profile for a backend that cannot run")
	}
	_ = res
}

func TestStartPrintNeverTouchesTheHost(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{}

	res, err := runStart(t, dir, h, "start", "--print")
	if err != nil {
		t.Fatalf("start --print: %v\nstderr: %s", err, res.err)
	}

	if len(h.probes) != 0 {
		t.Errorf("--print probed the host: %v", h.probes)
	}
	if h.argv != nil {
		t.Errorf("--print launched %v", h.argv)
	}
	if !strings.Contains(res.out, "\"extends\"") {
		t.Errorf("stdout is not a profile:\n%s", res.out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".config", "lid", ".kit", "nono.jsonc")); statErr == nil {
		t.Error("--print wrote a profile to disk")
	}
}

func TestStartSaysWhereItWroteTheProfileWithoutPollutingStdout(t *testing.T) {
	dir := nonoProject(t)

	res, err := runStart(t, dir, &startHost{}, "start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if !strings.Contains(res.err, filepath.Join(".config", "lid", ".kit", "nono.jsonc")) {
		t.Errorf("stderr does not name the profile it wrote:\n%s", res.err)
	}
	if strings.Contains(res.out, "nono.jsonc") {
		t.Errorf("the note leaked into stdout:\n%s", res.out)
	}
}

func TestStartStaysQuietWhenTheProfileHasNotChanged(t *testing.T) {
	dir := nonoProject(t)
	if _, err := runStart(t, dir, &startHost{}, "start"); err != nil {
		t.Fatalf("first start: %v", err)
	}

	h := &startHost{}
	res, err := runStart(t, dir, h, "start")
	if err != nil {
		t.Fatalf("second start: %v", err)
	}

	if strings.Contains(res.err, "nono.jsonc") {
		t.Errorf("announced a profile it did not rewrite:\n%s", res.err)
	}
	for _, probe := range h.probes {
		if strings.Join(probe, " ") == "nono profile validate" {
			t.Errorf("re-validated an unchanged profile: %v", h.probes)
		}
	}
	if h.argv == nil {
		t.Error("did not launch the agent on an unchanged profile")
	}
}

func TestStartPassesTheAgentsExitStatusThrough(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{exit: &app.ExitError{Code: 130}}

	_, err := runStart(t, dir, h, "start")

	var exit *app.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error is %v, want the agent's exit status", err)
	}
	if exit.Code != 130 {
		t.Errorf("Code = %d, want 130", exit.Code)
	}
}

func TestExitCodePassesTheAgentsStatusThroughSilently(t *testing.T) {
	var errBuf bytes.Buffer
	a := &app.App{Out: io.Discard, Err: &errBuf}

	if got := exitCode(a, &app.ExitError{Code: 3}); got != 3 {
		t.Errorf("exitCode = %d, want 3", got)
	}
	if errBuf.String() != "" {
		t.Errorf("reported the agent's own exit as a lid error:\n%s", errBuf.String())
	}
}

func TestExitCodeReportsARealFailure(t *testing.T) {
	var errBuf bytes.Buffer
	a := &app.App{Out: io.Discard, Err: &errBuf}

	if got := exitCode(a, errors.New("boom")); got != 1 {
		t.Errorf("exitCode = %d, want 1", got)
	}
	if !strings.Contains(errBuf.String(), "boom") {
		t.Errorf("the failure was swallowed:\n%s", errBuf.String())
	}
}

func TestExitCodeIsZeroWhenNothingWentWrong(t *testing.T) {
	if got := exitCode(&app.App{Out: io.Discard, Err: io.Discard}, nil); got != 0 {
		t.Errorf("exitCode = %d, want 0", got)
	}
}

func TestInitWritesTheBackendItWasAskedFor(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init", "nono"); err != nil {
		t.Fatalf("init nono: %v", err)
	}

	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("the config init wrote does not load: %v", err)
	}
	if cfg.Backend != config.BackendNono {
		t.Errorf("Backend = %q, want %q: the file must not lean on lid's default", cfg.Backend, config.BackendNono)
	}
}

func TestInitDefaultsToTheDefaultBackend(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Backend != config.DefaultBackend {
		t.Errorf("Backend = %q, want %q", cfg.Backend, config.DefaultBackend)
	}
}

func TestInitRejectsABackendItDoesNotHave(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init", "podman"); err == nil {
		t.Fatal("init accepted a backend lid cannot run")
	}
	if _, err := config.Discover(dir); err == nil {
		t.Error("init wrote a config despite the unknown backend")
	}
}

func TestInitKeepsEachBackendsKeysOutOfTheOthersTemplate(t *testing.T) {
	for backend, absent := range map[string][]string{
		config.BackendNono: {"base_image:", "template:", "packages:", "workspace:"},
		config.BackendSbx:  {"allow_commands:", "network_profile:"},
	} {
		dir := t.TempDir()
		if _, err := run(t, dir, "init", backend); err != nil {
			t.Fatalf("init %s: %v", backend, err)
		}
		body := read(t, filepath.Join(dir, ".config", "lid", "lid.yaml"))
		for _, key := range absent {
			if strings.Contains(body, key) {
				t.Errorf("the %s template still offers %s, which that backend cannot honour", backend, key)
			}
		}
	}
}

func TestInitPinsTheRuntimesItWasGiven(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init", "sbx", "--runtimes", "node@24,go@1.27"); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Runtimes["node"] != "24" || cfg.Runtimes["go"] != "1.27" {
		t.Errorf("Runtimes = %v, want node 24 and go 1.27", cfg.Runtimes)
	}
}

func TestInitRejectsRuntimesForABackendThatInstallsNone(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(t, dir, "init", "nono", "--runtimes", "node@24"); err == nil {
		t.Fatal("init pinned runtimes for nono, which uses the host's toolchains rather than installing any")
	}
	if _, err := config.Discover(dir); err == nil {
		t.Error("init wrote a config despite the rejected flag")
	}
}

func TestInitRejectsARuntimePinItCannotUse(t *testing.T) {
	for _, pin := range []string{"node", "deno@2", "node@"} {
		dir := t.TempDir()
		if _, err := run(t, dir, "init", "sbx", "--runtimes", pin); err == nil {
			t.Errorf("init accepted --runtimes %q", pin)
		}
	}
}

func TestInitNamesTheUnknownBackendRatherThanAFlagItRejects(t *testing.T) {
	got, err := run(t, t.TempDir(), "init", "podman", "--runtimes", "node@24")
	if err == nil {
		t.Fatal("init accepted a backend lid cannot run")
	}
	if strings.Contains(err.Error(), "--runtimes") {
		t.Errorf("the error blames the flag when the backend is the thing lid cannot run: %v (stderr: %s)", err, got.err)
	}
}

// sbxStartHost stands in for a machine with sbx and docker installed and one
// sandbox already created, so a start test can exercise what lid does to a
// sandbox somebody may already be working in.
type sbxStartHost struct {
	sandbox  string
	list     string
	hash     string
	settings string
	paneID   string
	herdrID  string
	stdin    io.Reader
	probes   [][]string
	argv     []string
}

// zellij answers as a session whose focused pane is terminal_7, on a tab that
// has never been renamed.
func (h *sbxStartHost) zellij(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	switch {
	case strings.Contains(strings.Join(argv, " "), "list-clients"):
		return []byte("CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1 terminal_7 lid\n"), nil
	case strings.Contains(strings.Join(argv, " "), "list-tabs"):
		return []byte(`[{"name": "Tab #1", "active": true, "are_floating_panes_visible": false}]`), nil
	}
	return nil, nil
}

// herdr answers as a Herdr window with terminal t7 in tab tb1, a tab that has
// never been renamed.
func (h *sbxStartHost) herdr(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	if strings.Join(argv, " ") == "herdr-app ls" {
		return []byte(`{"spaces": [{"id": "sp1", "tabs": [{"id": "tb1", "name": "1", "terminals": [{"terminal": "t7"}]}]}]}`), nil
	}
	return nil, nil
}

func (h *sbxStartHost) lookPath(name string) (string, error) {
	return "/usr/local/bin/" + name, nil
}

func (h *sbxStartHost) output(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	line := strings.Join(argv, " ")
	switch {
	case line == "sbx version":
		return []byte("sbx version v0.54.0 4bec4a5\n"), nil
	case strings.Contains(line, "sbx ls --json"):
		return []byte(h.list), nil
	case strings.Contains(line, "sbx ls"):
		return []byte(h.sandbox + "\n"), nil
	case strings.Contains(line, ".lid-config-hash"):
		if h.hash == "" {
			return nil, errors.New("exit status 1")
		}
		return []byte(h.hash + "\n"), nil
	case strings.Contains(line, "cat \"$HOME/.claude/settings.json\""):
		return []byte(h.settings), nil
	}
	return nil, nil
}

func (h *sbxStartHost) exec(argv []string, _, _ io.Writer) error {
	h.probes = append(h.probes, argv)
	return nil
}

func (h *sbxStartHost) attach(argv []string, _ io.Reader, _, _ io.Writer) error {
	h.argv = argv
	return nil
}

func (h *sbxStartHost) ran(fragment string) bool {
	for _, probe := range h.probes {
		if strings.Contains(strings.Join(probe, " "), fragment) {
			return true
		}
	}
	return false
}

func runSbxStart(t *testing.T, cwd string, h *sbxStartHost, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	var stdin io.Reader = strings.NewReader("")
	if h.stdin != nil {
		stdin = h.stdin
	}
	a := &app.App{
		Version: "v1.2.3",
		In:      stdin,
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
		Exec:    h.exec,
		Host:    backend.Host{LookPath: h.lookPath, Output: h.output, Exec: h.exec},
		Attach:  h.attach,
		Zellij:  zellij.Session{PaneID: h.paneID, Run: h.zellij},
		Herdr:   herdr.Session{TerminalID: h.herdrID, Run: h.herdr},
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\nstderr: %s", args, err, errBuf.String())
	}
	return result{out: out.String(), err: errBuf.String()}
}

func TestStartKeepsTheRunningSandboxWhenTheConfigHasChanged(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "built-from-an-older-config", settings: "{}"}

	res := runSbxStart(t, dir, h, "start")

	if h.ran("sbx rm") {
		t.Errorf("a config edit destroyed the sandbox and every session in it: %v", h.probes)
	}
	if !strings.Contains(res.err, "--rebuild") {
		t.Errorf("nothing says how to apply the change:\n%s", res.err)
	}
	if strings.Join(h.argv, " ") != "sbx run --name api" {
		t.Errorf("launched %v, want it to attach to the sandbox that was already there", h.argv)
	}
}

func TestStartRebuildReplacesTheSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "built-from-an-older-config", settings: "{}"}

	runSbxStart(t, dir, h, "start", "--rebuild")

	if !h.ran("sbx rm") {
		t.Errorf("--rebuild left the old sandbox standing: %v", h.probes)
	}
	if !h.ran("sbx create") {
		t.Errorf("--rebuild never created the replacement: %v", h.probes)
	}
}

func TestStartAppliesTheConfiguredClaudeSettingsBeforeLaunching(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")
	h := &sbxStartHost{sandbox: "api", settings: `{"theme": "dark"}`}

	runSbxStart(t, dir, h, "start")

	if !h.ran("claude-opus-5") {
		t.Errorf("the agent starts on whatever model the sandbox defaults to: %v", h.probes)
	}
	if h.argv == nil {
		t.Error("the agent was never launched")
	}
}

func TestStartWarnsButStillLaunchesWhenTheSettingsCannotBeApplied(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")
	h := &sbxStartHost{sandbox: "api", settings: "not json"}

	res := runSbxStart(t, dir, h, "start")

	if !strings.Contains(res.err, "settings") {
		t.Errorf("stderr says nothing about the settings lid could not apply:\n%s", res.err)
	}
	if h.argv == nil {
		t.Error("a settings problem stopped the agent from starting at all")
	}
}

func TestStartNamesTheZellijTabItWasLaunchedFrom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\ndisplay_name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", paneID: "7"}

	res := runSbxStart(t, dir, h, "start")

	if !h.ran("rename-tab") {
		t.Errorf("the tab kept its default name: %v", h.probes)
	}
	if strings.Contains(res.err, "zellij") {
		t.Errorf("zellij is wired up but still reported as ignored:\n%s", res.err)
	}
}

func TestStartNamesTheHerdrTabItWasLaunchedFrom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\ndisplay_name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}

	res := runSbxStart(t, dir, h, "start")

	if !h.ran("tab.rename --id tb1") {
		t.Errorf("the Herdr tab kept its default name: %v", h.probes)
	}
	if strings.Contains(res.err, "deprecated") {
		t.Errorf("warned about a config already on the current keys:\n%s", res.err)
	}
}

func TestStartStillNamesTheTabFromTheDeprecatedZellijKeyAndSaysToMoveIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nzellij:\n  display_name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", paneID: "7", herdrID: "t7"}

	res := runSbxStart(t, dir, h, "start")

	if !h.ran("rename-tab") || !h.ran("tab.rename") {
		t.Errorf("the old key no longer names the tab: %v", h.probes)
	}
	if !strings.Contains(res.err, "zellij.display_name") || !strings.Contains(res.err, "deprecated") {
		t.Errorf("never said zellij.display_name is deprecated:\n%s", res.err)
	}
}

func TestStartPrefersTheTopLevelDisplayNameOverTheZellijOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\ndisplay_name: new\nzellij:\n  display_name: old\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}

	runSbxStart(t, dir, h, "start")

	var name string
	for _, probe := range h.probes {
		if len(probe) > 2 && probe[2] == "tab.rename" {
			name = probe[len(probe)-1]
		}
	}
	if !strings.HasSuffix(name, " new") {
		t.Errorf("named the tab %q, want it after the top-level display_name: %v", name, h.probes)
	}
}

func TestStartPrintLeavesTheZellijTabAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nzellij:\n  display_name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", paneID: "7"}

	runSbxStart(t, dir, h, "start", "--print")

	if h.ran("rename-tab") {
		t.Errorf("--print renamed a tab it never started an agent in: %v", h.probes)
	}
}

// The broker and the sandboxes have to agree on a port. The sandbox reads it out
// of handoff.url, so the broker has to as well, or the firewall opens a port
// nothing is listening on.
func TestTheBrokerTakesItsPortFromTheProjectConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".config/lid/lid.yaml", "name: api\nhandoff:\n  enabled: true\n  url: http://host.docker.internal:9123\n")

	a := &app.App{Out: io.Discard, Err: io.Discard, Cwd: dir}
	if got := brokerPort(a, 0); got != 9123 {
		t.Errorf("brokerPort = %d, want the port from handoff.url", got)
	}
	if got := brokerPort(a, 9999); got != 9999 {
		t.Errorf("brokerPort with -P = %d, want the flag to win", got)
	}
}

func TestTheBrokerRunsOutsideAProjectOnTheDefaultPort(t *testing.T) {
	a := &app.App{Out: io.Discard, Err: io.Discard, Cwd: t.TempDir()}
	if got := brokerPort(a, 0); got != config.DefaultHandoffPort {
		t.Errorf("brokerPort = %d, want %d where there is no config to read", got, config.DefaultHandoffPort)
	}
}

// A pid alone is not proof a broker is there. Pids get recycled, and signalling
// one that now belongs to something else kills a stranger's process.
func TestStoppingABrokerRefusesToSignalAPidThatDoesNotAnswer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := writeBrokerFile(os.Getpid(), 1); err != nil {
		t.Fatal(err)
	}

	var errBuf bytes.Buffer
	a := &app.App{Out: io.Discard, Err: &errBuf, Cwd: t.TempDir()}
	err := killBroker(a)
	if err == nil {
		t.Fatal("killBroker: want a refusal when nothing answers as a broker")
	}
	if !strings.Contains(err.Error(), "recycled") {
		t.Errorf("err = %v, want it to say why the pid is not trusted", err)
	}
}

func TestStoppingABrokerClearsAPidFileForAProcessThatIsGone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := writeBrokerFile(999999999, 8787); err != nil {
		t.Fatal(err)
	}

	a := &app.App{Out: io.Discard, Err: io.Discard, Cwd: t.TempDir()}
	if err := killBroker(a); err == nil {
		t.Fatal("killBroker: want it to report no broker running")
	}
	if _, err := os.Stat(handoff.DefaultPidPath()); !os.IsNotExist(err) {
		t.Error("the stale pid file was left behind")
	}
}

func TestShellOpensAShellInTheSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "current", settings: "{}"}

	runSbxStart(t, dir, h, "shell")

	want := "sbx exec -it -w " + dir + " api -- bash --rcfile /home/agent/.lid-shellrc -i"
	if got := strings.Join(h.argv, " "); got != want {
		t.Errorf("launched %q, want %q", got, want)
	}
}

func TestSSHIsTheSameCommandAsShell(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "current", settings: "{}"}

	runSbxStart(t, dir, h, "ssh")

	want := "sbx exec -it -w " + dir + " api -- bash --rcfile /home/agent/.lid-shellrc -i"
	if got := strings.Join(h.argv, " "); got != want {
		t.Errorf("launched %q, want %q", got, want)
	}
}

// The hint has to name the command the user actually ran, or it sends them to a
// flag they were not using.
func TestShellPointsAtItsOwnRebuildFlag(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "built-from-an-older-config", settings: "{}"}

	res := runSbxStart(t, dir, h, "shell")

	if !strings.Contains(res.err, "lid shell --rebuild") {
		t.Errorf("the stale-config hint names another command:\n%s", res.err)
	}
}

func TestShellRebuildReplacesTheSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "built-from-an-older-config", settings: "{}"}

	runSbxStart(t, dir, h, "shell", "--rebuild")

	if !h.ran("sbx rm") {
		t.Errorf("--rebuild left the stale sandbox in place: %v", h.probes)
	}
}

func TestShellRunsAOneOffCommandInTheSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "current", settings: "{}"}

	runSbxStart(t, dir, h, "shell", "--", "npm", "test")

	got := strings.Join(h.argv, " ")
	if !strings.Contains(got, "sbx exec -i -w ") {
		t.Errorf("launched %q, want a one-off command rather than an interactive shell", got)
	}
	if !strings.HasSuffix(got, "bash -lc npm test") {
		t.Errorf("launched %q, want it to run the command it was given", got)
	}
}

func TestShellPassesTheCommandsOwnFlagsThrough(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", hash: "current", settings: "{}"}

	runSbxStart(t, dir, h, "shell", "--", "ls", "-la")

	if got := strings.Join(h.argv, " "); !strings.HasSuffix(got, "bash -lc ls -la") {
		t.Errorf("launched %q, want the command's own flags to reach it", got)
	}
}

func TestShellCreatesTheSandboxWhenThereIsNoneYet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{settings: "{}"}

	runSbxStart(t, dir, h, "shell")

	if !h.ran("sbx create") {
		t.Errorf("the shell was opened against a sandbox nobody created: %v", h.probes)
	}
}

func TestShellWritesTheProfileThenOpensANonoShell(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{}

	res, err := runStart(t, dir, h, "shell")
	if err != nil {
		t.Fatalf("shell: %v\nstderr: %s", err, res.err)
	}

	profile := filepath.Join(dir, ".config", "lid", ".kit", "nono.jsonc")
	if read(t, profile) == "" {
		t.Fatalf("no profile written to %s", profile)
	}
	if got := strings.Join(h.argv, " "); !strings.Contains(got, "nono shell --profile "+profile) {
		t.Errorf("launched %q, want a nono shell under the profile it wrote", got)
	}
}

func TestShellReportsTheCommandsOwnExitCode(t *testing.T) {
	dir := nonoProject(t)
	h := &startHost{exit: &app.ExitError{Code: 3}}

	_, err := runStart(t, dir, h, "shell", "--", "false")

	var exit *app.ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Errorf("err = %v, want the command's own exit code", err)
	}
}

type statusHost struct {
	list   string
	hash   string
	probes [][]string
}

func (h *statusHost) lookPath(name string) (string, error) {
	return "/usr/local/bin/" + name, nil
}

func (h *statusHost) output(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	line := strings.Join(argv, " ")
	switch {
	case line == "sbx version":
		return []byte("sbx version v0.54.0 4bec4a5\n"), nil
	case strings.Contains(line, "--version"):
		return []byte("nono 0.74.0\n"), nil
	case strings.Contains(line, "sbx ls"), strings.Contains(line, "nono ps"):
		return []byte(h.list), nil
	case strings.Contains(line, ".lid-config-hash"):
		if h.hash == "" {
			return nil, errors.New("exit status 1")
		}
		return []byte(h.hash + "\n"), nil
	}
	return nil, nil
}

func (h *statusHost) exec(argv []string, _, _ io.Writer) error {
	h.probes = append(h.probes, argv)
	return nil
}

func (h *statusHost) ran(fragment string) bool {
	for _, probe := range h.probes {
		if strings.Contains(strings.Join(probe, " "), fragment) {
			return true
		}
	}
	return false
}

func runWithHost(t *testing.T, cwd string, h *statusHost, args ...string) (result, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	a := &app.App{
		Version: "v1.2.3",
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
		Host:    backend.Host{LookPath: h.lookPath, Output: h.output, Exec: h.exec},
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.Execute()
	return result{out: out.String(), err: errBuf.String()}, err
}

func TestStatusNamesTheSandboxItLookedFor(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	got, err := runWithHost(t, dir, h, "status")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	for _, want := range []string{"api", "sbx", "claude-code", "running", "abc123"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("output is missing %q:\n%s", want, got.out)
		}
	}
}

func TestStatusSaysWhatToRunWhenThereIsNoSandboxYet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: "[]"}

	got, err := runWithHost(t, dir, h, "status")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.out, "not created") {
		t.Errorf("output does not say the sandbox is not there:\n%s", got.out)
	}
	if !strings.Contains(got.err, "lid start") {
		t.Errorf("nothing tells the user how to create it:\n%s", got.err)
	}
}

func TestStatusWarnsWhenTheConfigMovedOnFromTheSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc", "name": "api", "status": "running"}]`, hash: "from-an-older-config"}

	got, err := runWithHost(t, dir, h, "status")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.err, "has changed") || !strings.Contains(got.err, "--rebuild") {
		t.Errorf("stale sandbox is not called out with a way to fix it:\n%s", got.err)
	}
}

func TestStatusNeverStartsAnything(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: "[]"}

	if _, err := runWithHost(t, dir, h, "status"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, fragment := range []string{"sbx create", "sbx run", "docker build"} {
		if h.ran(fragment) {
			t.Errorf("'lid status' ran %q, so asking about a sandbox would build one", fragment)
		}
	}
}

func TestStopStopsARunningSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc", "name": "api", "status": "running"}]`, hash: "any"}

	got, err := runWithHost(t, dir, h, "stop")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if !h.ran("sbx stop api") {
		t.Errorf("probes = %v, want 'sbx stop api'", h.probes)
	}
	if !strings.Contains(got.out, "Stopped api") {
		t.Errorf("output does not confirm the stop:\n%s", got.out)
	}
}

func TestStopSaysSoRatherThanStoppingASandboxThatIsNotRunning(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc", "name": "api", "status": "stopped"}]`}

	got, err := runWithHost(t, dir, h, "stop")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if h.ran("sbx stop") {
		t.Error("lid stopped a sandbox that was already stopped")
	}
	if !strings.Contains(got.err, "not running") {
		t.Errorf("nothing says why there was nothing to do:\n%s", got.err)
	}
}

func TestStopOnAProjectWithNoSandboxIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: "[]"}

	got, err := runWithHost(t, dir, h, "stop")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.err, "no api sandbox") {
		t.Errorf("nothing says there was no sandbox to stop:\n%s", got.err)
	}
}

func TestStatusReportsTheNonoProfileAndItsSessions(t *testing.T) {
	dir := nonoProject(t)
	h := &statusHost{list: "[]"}

	if _, err := runStart(t, dir, &startHost{}, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}

	got, err := runWithHost(t, dir, h, "status")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.out, "nono") || !strings.Contains(got.out, "stopped") {
		t.Errorf("output does not describe the profile lid wrote:\n%s", got.out)
	}
	if !strings.Contains(got.out, "nono.jsonc") {
		t.Errorf("output does not name the profile:\n%s", got.out)
	}
}

func TestTheUpdateNoticeStaysQuietWhenNothingIsPending(t *testing.T) {
	var out, errBuf bytes.Buffer
	a := &app.App{Version: "v1.2.3", Out: &out, Err: &errBuf}

	printUpdateNotice(a)
	if errBuf.Len() != 0 {
		t.Errorf("stderr = %q, want nothing when no newer version is known", errBuf.String())
	}
}

func TestTheUpdateNoticeNamesBothVersionsAndTheCommandToRun(t *testing.T) {
	var out, errBuf bytes.Buffer
	a := &app.App{Version: "v1.2.3", PendingUpdate: "v1.3.0", Out: &out, Err: &errBuf}

	printUpdateNotice(a)
	notice := errBuf.String()
	for _, want := range []string{"v1.3.0", "v1.2.3", "lid update"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice = %q, want it to mention %q", notice, want)
		}
	}
	if out.Len() != 0 {
		t.Error("the update notice went to stdout, where it would corrupt piped output")
	}
}

func TestACommandWritingToAPipeNeverChecksForUpdates(t *testing.T) {
	if isTTY(&bytes.Buffer{}) {
		t.Error("a buffer was taken for a terminal, so every test run would reach for the network")
	}

	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if isTTY(file) {
		t.Error("'lid config > out.txt' was taken for a terminal, so a notice would land in the file")
	}
}

func TestHiddenUpdateCheckIsNotOfferedToUsers(t *testing.T) {
	root := NewRootCommand(&app.App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	for _, cmd := range root.Commands() {
		if cmd.Name() == update.CheckCommand && !cmd.Hidden {
			t.Error("the background update worker is listed in 'lid --help' as if it were a command to run")
		}
	}
}

func TestNetworkAllowOpensTheHostOnTheRunningSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	got, err := runWithHost(t, dir, h, "network", "allow", "pypi.org")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}

	if !h.ran("sbx policy allow network --sandbox api pypi.org") {
		t.Errorf("never applied the rule; ran %v", h.probes)
	}
	if !strings.Contains(got.err, "pypi.org") {
		t.Errorf("said nothing about the host:\n%s", got.err)
	}
}

// Without --persist the rule dies with the sandbox, so the config must be left
// exactly as the user wrote it.
func TestNetworkAllowWithoutPersistLeavesTheConfigAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	if _, err := runWithHost(t, dir, h, "network", "allow", "pypi.org"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); strings.Contains(body, "pypi.org") {
		t.Errorf("config was edited without --persist:\n%s", body)
	}
}

func TestNetworkAllowWithPersistWritesTheHostIntoTheConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	got, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "--persist")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); !strings.Contains(body, "pypi.org") {
		t.Errorf("config missing the host:\n%s", body)
	}
	if !h.ran("sbx policy allow network --sandbox api pypi.org") {
		t.Error("--persist saved the rule but never applied it to the running sandbox")
	}
}

func TestNetworkRemoveWithPersistDropsTheHostFromTheConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\nnetwork:\n  - pypi.org\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	if _, err := runWithHost(t, dir, h, "network", "remove", "pypi.org", "--persist"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); strings.Contains(body, "pypi.org") {
		t.Errorf("config still lists the host:\n%s", body)
	}
	if !h.ran("sbx policy rm network --sandbox api --resource pypi.org") {
		t.Errorf("never took the rule off the sandbox; ran %v", h.probes)
	}
}

// A runtime-only rule has nowhere to go when there is no sandbox, so saying so
// beats reporting a change that reached nothing.
func TestNetworkAllowWithNoSandboxSaysWhatToRun(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[]`}

	_, err := runWithHost(t, dir, h, "network", "allow", "pypi.org")

	if err == nil {
		t.Fatal("allowed a host on a sandbox that does not exist")
	}
	if !strings.Contains(err.Error(), "lid start") || !strings.Contains(err.Error(), "--persist") {
		t.Errorf("error = %q, want both ways forward named", err)
	}
}

func TestNetworkAllowWithNoSandboxStillPersists(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[]`}

	got, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "--persist")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); !strings.Contains(body, "pypi.org") {
		t.Errorf("config missing the host:\n%s", body)
	}
	if h.ran("sbx policy") {
		t.Error("touched policy for a sandbox that does not exist")
	}
	if !strings.Contains(got.err, "lid start") {
		t.Errorf("never said when this takes effect:\n%s", got.err)
	}
}

// The config lists what is allowed and has no denylist, so a --persist here
// would have nowhere to write.
func TestNetworkDenyHasNoPersistFlag(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	_, err := runWithHost(t, dir, h, "network", "deny", "evil.example", "--persist")

	if err == nil {
		t.Fatal("network deny accepted --persist")
	}
}

func TestNetworkDenyClosesTheHostOnTheRunningSandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	if _, err := runWithHost(t, dir, h, "network", "deny", "evil.example"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !h.ran("sbx policy deny network --sandbox api evil.example") {
		t.Errorf("never applied the deny; ran %v", h.probes)
	}
}

// nono reads its policy when a session starts, so --persist is the only form of
// this command it can honour, and that is not a failure worth an exit code.
func TestNetworkAllowOnNonoWithPersistEditsTheConfigAndSaysToRestart(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\n")
	h := &statusHost{list: `{"sessions": []}`}
	write(t, dir, ".config/lid/.kit/nono.jsonc", "{}")

	got, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "--persist")
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, got.err)
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); !strings.Contains(body, "pypi.org") {
		t.Errorf("config missing the host:\n%s", body)
	}
	if !strings.Contains(got.err, "lid start") {
		t.Errorf("never said how to pick the change up:\n%s", got.err)
	}
}

func TestNetworkAllowOnNonoWithoutPersistRefuses(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\n")
	h := &statusHost{list: `{"sessions": []}`}
	write(t, dir, ".config/lid/.kit/nono.jsonc", "{}")

	_, err := runWithHost(t, dir, h, "network", "allow", "pypi.org")

	if err == nil {
		t.Fatal("nono reported a runtime policy change it cannot make")
	}
	if !strings.Contains(err.Error(), "--persist") {
		t.Errorf("error = %q, want it to point at --persist", err)
	}
}

func TestNetworkRejectsAHostWrittenAsAURL(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	_, err := runWithHost(t, dir, h, "network", "allow", "https://pypi.org")

	if err == nil {
		t.Fatal("accepted a URL as a host pattern")
	}
	if h.ran("sbx policy") {
		t.Error("pushed a URL at sbx before checking it")
	}
}

func TestNetworkChecksHostsBeforeEditingAnything(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	if _, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "https://bad", "--persist"); err == nil {
		t.Fatal("accepted a batch holding a URL")
	}

	if body := read(t, filepath.Join(dir, "lid.yaml")); strings.Contains(body, "pypi.org") {
		t.Errorf("saved half a rejected batch:\n%s", body)
	}
	if h.ran("sbx policy") {
		t.Error("applied half a rejected batch")
	}
}

func TestTimedAllowRefusesToBeSaved(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	_, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "--mins", "5", "--persist")

	if err == nil {
		t.Fatal("accepted --mins with --persist")
	}
	if h.ran("sbx policy") {
		t.Error("applied a rule the flags had already ruled out")
	}
}

func TestTimedAllowRefusesAWindowItWillNotWaitOut(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}

	for _, mins := range []string{"0", "-1", "31"} {
		_, err := runWithHost(t, dir, h, "network", "allow", "pypi.org", "--mins", mins)
		if mins == "0" {
			continue
		}
		if err == nil {
			t.Errorf("--mins %s was accepted", mins)
		}
	}
}

func TestTheCountdownBarStaysOneWidthAsItFills(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 30 * time.Second, time.Minute, 2 * time.Minute} {
		bar := countdownBar(elapsed, time.Minute, 24)
		if got := len([]rune(bar)); got != 24 {
			t.Errorf("bar for %s is %d runes wide, want 24: %q", elapsed, got, bar)
		}
	}
}

func TestTheCountdownNeverCountsBelowZero(t *testing.T) {
	if got := formatRemaining(-5 * time.Second); got != "0:00" {
		t.Errorf("formatRemaining(-5s) = %q, want 0:00", got)
	}
	if got := formatRemaining(90 * time.Second); got != "1:30" {
		t.Errorf("formatRemaining(90s) = %q, want 1:30", got)
	}
}

// A timed allow that did not take the rule back again would quietly leave the
// sandbox more open than the user asked for, which is the whole risk the flag
// exists to bound.
func TestATimedAllowTakesTheRuleBackWhenTheWindowEnds(t *testing.T) {
	previous := countdownTick
	countdownTick = 5 * time.Millisecond
	defer func() { countdownTick = previous }()

	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")
	h := &statusHost{list: `[{"id": "abc123", "name": "api", "status": "running"}]`}
	var out, errBuf bytes.Buffer
	a := &app.App{
		Version: "v1.2.3",
		In:      strings.NewReader(""),
		Out:     &out,
		Err:     &errBuf,
		Cwd:     dir,
		Host:    backend.Host{LookPath: h.lookPath, Output: h.output, Exec: h.exec},
	}

	if err := runNetwork(a, backend.NetworkAllow, []string{"pypi.org"}, false, 10*time.Millisecond); err != nil {
		t.Fatalf("runNetwork: %v", err)
	}

	if !h.ran("sbx policy allow network --sandbox api pypi.org") {
		t.Errorf("never applied the allow; ran %v", h.probes)
	}
	if !h.ran("sbx policy rm network --sandbox api --resource pypi.org") {
		t.Errorf("the allow outlived its window; ran %v", h.probes)
	}
	if !strings.Contains(errBuf.String(), "pypi.org") {
		t.Errorf("never said the allow was taken back:\n%s", errBuf.String())
	}
}

func restartHost(t *testing.T, stdin string) (string, *sbxStartHost) {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	return dir, &sbxStartHost{
		sandbox:  "api",
		list:     `[{"id": "abc123", "name": "api", "status": "running"}]`,
		hash:     "built-from-an-older-config",
		settings: "{}",
		stdin:    strings.NewReader(stdin),
	}
}

func TestRestartReplacesTheSandboxWhenTheAnswerIsYes(t *testing.T) {
	dir, h := restartHost(t, "y\n")

	runSbxStart(t, dir, h, "restart")

	if !h.ran("sbx rm") {
		t.Errorf("a confirmed restart left the old sandbox in place: %v", h.probes)
	}
	if strings.Join(h.argv, " ") != "sbx run --name api" {
		t.Errorf("launched %v, want the agent started in the replacement", h.argv)
	}
}

func TestRestartLeavesTheSandboxWhereItWasWhenTheAnswerIsNo(t *testing.T) {
	dir, h := restartHost(t, "n\n")

	res := runSbxStart(t, dir, h, "restart")

	if h.ran("sbx rm") {
		t.Errorf("answering no destroyed the sandbox anyway: %v", h.probes)
	}
	if h.argv != nil {
		t.Errorf("launched %v, want nothing started after declining", h.argv)
	}
	if !strings.Contains(res.err, "Left the api sandbox where it was") {
		t.Errorf("nothing says the sandbox survived:\n%s", res.err)
	}
}

func TestRestartSaysWhatIsAboutToBeDestroyedBeforeAsking(t *testing.T) {
	dir, h := restartHost(t, "n\n")

	res := runSbxStart(t, dir, h, "restart")

	for _, want := range []string{"api", "destroys", "Continue? [y/N]"} {
		if !strings.Contains(res.err, want) {
			t.Errorf("the question is missing %q:\n%s", want, res.err)
		}
	}
}

func TestRestartWithNothingOnStdinLeavesTheSandboxStanding(t *testing.T) {
	dir, h := restartHost(t, "")

	runSbxStart(t, dir, h, "restart")

	if h.ran("sbx rm") {
		t.Errorf("a restart nobody could answer destroyed the sandbox: %v", h.probes)
	}
}

func TestRestartForceReplacesTheSandboxWithoutAsking(t *testing.T) {
	dir, h := restartHost(t, "")

	res := runSbxStart(t, dir, h, "restart", "--force")

	if !h.ran("sbx rm") {
		t.Errorf("--force did not replace the sandbox: %v", h.probes)
	}
	if strings.Contains(res.err, "Continue?") {
		t.Errorf("--force asked anyway:\n%s", res.err)
	}
}

func TestRestartDoesNotAskWhenThereIsNoSandboxToDestroy(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{list: "[]", settings: "{}", stdin: strings.NewReader("")}

	res := runSbxStart(t, dir, h, "restart")

	if strings.Contains(res.err, "Continue?") {
		t.Errorf("asked before destroying a sandbox that is not there:\n%s", res.err)
	}
	if !h.ran("sbx create") {
		t.Errorf("no sandbox was created: %v", h.probes)
	}
}

func TestRestartLeavesTheRestOfStdinForTheSessionItLaunches(t *testing.T) {
	dir, h := restartHost(t, "y\nthe agent's first keystrokes")
	rest := h.stdin.(*strings.Reader)

	runSbxStart(t, dir, h, "restart")

	left, err := io.ReadAll(rest)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != "the agent's first keystrokes" {
		t.Errorf("stdin left holding %q, want the answer read and nothing more", left)
	}
}

func TestNetworkRefusesHostsTheShellExpandedFromAPattern(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	write(t, dir, "package.json", "{}\n")
	h := &statusHost{list: `[{"name": "api", "status": "running"}]`}

	_, err := runWithHost(t, dir, h, "network", "allow", "package.json", "lid.yaml")

	if err == nil {
		t.Fatalf("a directory listing was turned into firewall rules: %v", h.probes)
	}
	for _, want := range []string{"package.json", "lid.yaml", "quote"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
	if h.ran("policy allow") {
		t.Errorf("rules were written before the pattern was caught: %v", h.probes)
	}
}

func TestNetworkStillAllowsAHostThatIsNotAFileHere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &statusHost{list: `[{"name": "api", "status": "running"}]`}

	if _, err := runWithHost(t, dir, h, "network", "allow", "pypi.org"); err != nil {
		t.Fatalf("network allow: %v", err)
	}
	if !h.ran("policy allow network --sandbox api pypi.org") {
		t.Errorf("the host never reached sbx: %v", h.probes)
	}
}

func TestTheCountdownNeverOutgrowsTheTerminalItRedrawsIn(t *testing.T) {
	hosts := strings.Repeat("some-long-host.example.com, ", 12)

	for _, width := range []int{20, 40, 80, 120} {
		line := countdownLine(hosts, 30*time.Second, time.Minute, width)
		if got := len([]rune(line)); got > width {
			t.Errorf("width %d: line is %d columns, so it wraps and cannot be redrawn in place:\n%s", width, got, line)
		}
		if !strings.HasPrefix(line, "0:30 left") {
			t.Errorf("width %d: line = %q, want the time left before anything that gets cut", width, line)
		}
		if !strings.Contains(line, "]") {
			t.Errorf("width %d: line = %q, want a bar narrow enough to close inside the terminal", width, line)
		}
	}
}

func TestTheCountdownAssumesAnOrdinaryWidthWhenNothingCanBeAsked(t *testing.T) {
	if got := terminalWidth(&bytes.Buffer{}); got != 80 {
		t.Errorf("terminalWidth off a terminal = %d, want a width the countdown can still fit in", got)
	}
}

func TestTheCountdownKeepsTheWholeLineWhenItFits(t *testing.T) {
	line := countdownLine("pypi.org", 30*time.Second, time.Minute, 200)

	if !strings.Contains(line, "pypi.org") {
		t.Errorf("line = %q, want the hosts kept when there is room for them", line)
	}
	if strings.Contains(line, "…") {
		t.Errorf("line = %q, want no cut marker on a line that fits", line)
	}
}

func TestConfigCommandShowsTheHostPathsANonoSessionGrants(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: demo\nbackend: nono\npaths:\n  read:\n    - ~/dotfiles/hooks\n  read_file:\n    - ~/dotfiles/settings.json\n  allow:\n    - ~/.cache/tool\n  deny:\n    - ~/.ssh\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, got.err)
	}
	for label, path := range map[string]string{
		"paths.read":      "~/dotfiles/hooks",
		"paths.read_file": "~/dotfiles/settings.json",
		"paths.allow":     "~/.cache/tool",
		"paths.deny":      "~/.ssh",
	} {
		if !hasGrantRow(got.out, label, path) {
			t.Errorf("no %s row grants %s:\n%s", label, path, got.out)
		}
	}
}

// hasGrantRow pairs a label with the path on its own line: the labels here
// share prefixes, so "paths.read" alone would be satisfied by the read_file row.
func hasGrantRow(out, label, value string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, label) && strings.Contains(line, value) {
			return true
		}
	}
	return false
}
