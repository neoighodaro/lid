package template

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/runtime"
)

func cfgFor(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lid.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v\nconfig:\n%s", err, body)
	}
	return cfg
}

func render(t *testing.T, body string) string {
	t.Helper()
	out, err := Render(cfgFor(t, body))
	if err != nil {
		t.Fatalf("Render: %v\nconfig:\n%s", err, body)
	}
	return out
}

func renderErr(t *testing.T, cfg *config.Config) error {
	t.Helper()
	out, err := Render(cfg)
	if err == nil {
		t.Fatalf("Render: want error, got:\n%s", out)
	}
	return err
}

func TestRenderStartsFromTheDefaultBaseImage(t *testing.T) {
	out := render(t, "name: lid\n")

	if want := "FROM " + config.DefaultBaseImage; !strings.HasPrefix(out, want) {
		t.Errorf("Dockerfile does not start with %q:\n%s", want, out)
	}
}

func TestRenderHonoursTheConfiguredBaseImage(t *testing.T) {
	out := render(t, "name: lid\nbase_image: ubuntu:24.04\n")

	if !strings.HasPrefix(out, "FROM ubuntu:24.04") {
		t.Errorf("Dockerfile does not use the configured base image:\n%s", out)
	}
	if strings.Contains(out, config.DefaultBaseImage) {
		t.Errorf("Dockerfile still mentions the default base image:\n%s", out)
	}
}

func TestRenderInstallsConfiguredPackages(t *testing.T) {
	out := render(t, "name: lid\npackages:\n  - ripgrep\n  - jq\n")

	if !strings.Contains(out, "locales ripgrep jq") {
		t.Errorf("packages are not installed alongside locales:\n%s", out)
	}
}

func TestRenderWithoutPackagesStillInstallsLocales(t *testing.T) {
	out := render(t, "name: lid\n")

	if !strings.Contains(out, "--no-install-recommends locales \\\n") {
		t.Errorf("locales line is malformed without packages:\n%s", out)
	}
}

func TestRenderPlacesRuntimeBlocksOnTheRightSideOfUserAgent(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  node: 22\n  python: \"3.12\"\n")

	split := strings.Index(out, "\nUSER agent")
	if split < 0 {
		t.Fatalf("Dockerfile never drops to the agent user:\n%s", out)
	}
	root, agent := out[:split], out[split:]

	if !strings.Contains(root, "deb.nodesource.com") {
		t.Errorf("node install belongs before USER agent:\n%s", out)
	}
	if !strings.Contains(agent, "uv python install 3.12") {
		t.Errorf("python install belongs after USER agent:\n%s", out)
	}
	if !strings.Contains(agent, `ENV PATH="/home/agent/.local/bin:$PATH"`) {
		t.Errorf("env block belongs after USER agent:\n%s", out)
	}
}

func TestRenderIsDeterministicAcrossRuntimeOrder(t *testing.T) {
	body := "name: lid\nruntimes:\n  rust: 1.81.0\n  node: 22\n  go: \"1.23\"\n  bun: 1.1.0\n"

	first := render(t, body)
	for i := 0; i < 20; i++ {
		if got := render(t, body); got != first {
			t.Fatalf("Dockerfile varies between renders:\n%s\n---\n%s", first, got)
		}
	}
}

func TestRenderValidatesRuntimeVersions(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  php: \"9.9\"\n")

	err := renderErr(t, cfg)
	if !strings.Contains(err.Error(), "php") || !strings.Contains(err.Error(), "9.9") {
		t.Errorf("error = %v, want it to name the runtime and version", err)
	}
}

func TestRenderPassesPHPExtensionsToTheRuntime(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - imagick\n")

	if !strings.Contains(out, "php8.5-imagick") {
		t.Errorf("configured php extension is missing:\n%s", out)
	}
}

func TestRenderRejectsAnUnknownRuntime(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	cfg.Runtimes = map[string]config.Scalar{"perl": "5"}

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "perl") {
		t.Errorf("error = %v, want it to name the runtime", err)
	}
}

func TestRenderKeepsClaudeSetupForTheClaudeAgent(t *testing.T) {
	out := render(t, "name: lid\nagent: claude-code\nclaude:\n  plugins:\n    marketplaces:\n      - obra/superpowers\n    install:\n      - superpowers@obra\n")

	for _, want := range []string{
		"RUN claude update || true",
		"RUN claude plugin marketplace add obra/superpowers",
		"RUN claude plugin install superpowers@obra",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, out)
		}
	}
}

func TestRenderOmitsClaudeSetupForOtherAgents(t *testing.T) {
	out := render(t, "name: lid\nagent: codex\nclaude:\n  plugins:\n    install:\n      - superpowers@obra\n")

	if strings.Contains(out, "RUN claude") {
		t.Errorf("Dockerfile runs claude setup for a non-claude agent:\n%s", out)
	}
	if !strings.Contains(out, "USER agent") {
		t.Errorf("Dockerfile must still drop to the agent user:\n%s", out)
	}
}

func TestRenderRefusesACustomTemplate(t *testing.T) {
	cfg := cfgFor(t, "name: lid\ntemplate: my-template\n")

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "my-template") {
		t.Errorf("error = %v, want it to name the template", err)
	}
}

func TestRenderRefusesABackendThatBuildsNoImage(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nbackend: nono\n")

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "nono") {
		t.Errorf("error = %v, want it to name the backend", err)
	}
}

func TestRenderEndsWithASingleTrailingNewline(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  node: 22\n")

	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("Dockerfile should end in exactly one newline, got %q", out[len(out)-5:])
	}
}

type recorder struct {
	calls [][]string
	err   error
}

func (r *recorder) exec(argv []string, _, _ io.Writer) error {
	r.calls = append(r.calls, argv)
	return r.err
}

func TestBuildWritesTheDockerfileIntoTheKitDir(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err != nil {
		t.Fatalf("Build: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg.KitDir(), "Dockerfile"))
	if err != nil {
		t.Fatalf("reading generated Dockerfile: %v", err)
	}
	if !strings.HasPrefix(string(data), "FROM ") {
		t.Errorf("generated Dockerfile looks wrong:\n%s", data)
	}
}

func TestBuildInvokesDockerBuildWithTheImageNameAndKitDir(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("ran %d commands, want 1: %v", len(rec.calls), rec.calls)
	}
	want := []string{"docker", "build", "--pull", "-t", cfg.SandboxImageName(), cfg.KitDir()}
	if strings.Join(rec.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", rec.calls[0], want)
	}
}

func TestBuildReportsAFailedDockerBuild(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{err: errors.New("exit status 1")}

	err := (&Builder{Exec: rec.exec}).Build(cfg)
	if err == nil {
		t.Fatal("Build: want error, got nil")
	}
	if !strings.Contains(err.Error(), "docker build") {
		t.Errorf("error = %v, want it to name the failing command", err)
	}
}

func TestBuildDoesNotRunDockerWhenRenderingFails(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  php: \"9.9\"\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err == nil {
		t.Fatal("Build: want error, got nil")
	}
	if len(rec.calls) != 0 {
		t.Errorf("ran %v, want nothing", rec.calls)
	}
}

func TestRenderInstallsNodeBeforePnpm(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  pnpm: 9.12.0\n  node: 22\n")

	node := strings.Index(out, "nodesource")
	pnpm := strings.Index(out, "npm install -g pnpm")
	if node < 0 || pnpm < 0 {
		t.Fatalf("expected both runtimes in the Dockerfile:\n%s", out)
	}
	if node > pnpm {
		t.Errorf("pnpm is installed before the npm it needs:\n%s", out)
	}
}

// The starter config is the first thing a user sees, so every version it
// suggests has to be one the runtimes actually accept. This guards the whole
// commented block, which no other test reads.
func TestStarterConfigSuggestsInstallableRuntimeVersions(t *testing.T) {
	starter, err := config.Starter("probe")
	if err != nil {
		t.Fatalf("Starter: %v", err)
	}

	suggestion := regexp.MustCompile(`^\s*#\s*([a-z]+):\s*"?([^"\s]+)"?\s*$`)
	inRuntimes := false
	found := 0

	for _, line := range strings.Split(starter, "\n") {
		if strings.HasPrefix(line, "runtimes:") {
			inRuntimes = true
			continue
		}
		if inRuntimes && strings.TrimSpace(line) == "" {
			break
		}
		m := suggestion.FindStringSubmatch(line)
		if !inRuntimes || m == nil {
			continue
		}
		name, version := m[1], m[2]
		r := runtime.Get(name, runtime.Options{})
		if r == nil {
			t.Errorf("starter suggests runtime %q, which is not registered", name)
			continue
		}
		if err := r.Validate(version); err != nil {
			t.Errorf("starter suggests %s: %q, which is invalid: %v", name, version, err)
		}
		found++
	}

	if found == 0 {
		t.Fatal("no runtime suggestions found in the starter config")
	}
}
