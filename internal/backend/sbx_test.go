package backend

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

// sbxFake stands in for a machine with sbx and docker installed, recording
// every command lid would run so the tests never need either binary.
type sbxFake struct {
	missing   bool
	sandboxes []string
	hash      string
	calls     [][]string
}

func (f *sbxFake) host() Host {
	return Host{
		LookPath: func(name string) (string, error) {
			if f.missing {
				return "", errors.New("executable file not found in $PATH")
			}
			return "/usr/local/bin/" + name, nil
		},
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if len(argv) > 2 && argv[1] == "ls" {
				return []byte(strings.Join(f.sandboxes, "\n") + "\n"), nil
			}
			if f.hash == "" {
				return nil, errors.New("exit status 1")
			}
			return []byte(f.hash + "\n"), nil
		},
		Exec: func(argv []string, stdout, stderr io.Writer) error {
			f.calls = append(f.calls, argv)
			return nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func (f *sbxFake) ran(fragments ...string) bool {
	for _, call := range f.calls {
		line := strings.Join(call, " ")
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func sbxCfg(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lid.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v\nconfig:\n%s", err, body)
	}
	return cfg
}

func TestSbxIsARegisteredBackend(t *testing.T) {
	b := Get(config.BackendSbx)
	if b == nil {
		t.Fatal("no sbx backend is registered, so 'lid start' cannot run the default backend")
	}
	if b.Name() != config.BackendSbx {
		t.Errorf("Name = %q, want %q", b.Name(), config.BackendSbx)
	}
}

func TestSbxNeedsTheSbxCli(t *testing.T) {
	f := &sbxFake{missing: true}

	err := Get(config.BackendSbx).EnsureAvailable(f.host())
	if err == nil {
		t.Fatal("a machine without sbx was accepted")
	}
	if !strings.Contains(err.Error(), "sbx") {
		t.Errorf("error = %q, want it to name sbx", err.Error())
	}
}

func TestSbxStartAttachesToTheNamedSandbox(t *testing.T) {
	argv, err := Get(config.BackendSbx).StartArgv(sbxCfg(t, "name: api\n"), "")
	if err != nil {
		t.Fatalf("StartArgv: %v", err)
	}
	if strings.Join(argv, " ") != "sbx run --name api" {
		t.Errorf("argv = %v, want sbx run --name api", argv)
	}
}

func TestSbxBuildsTheImageAndCreatesTheSandbox(t *testing.T) {
	f := &sbxFake{}
	cfg := sbxCfg(t, "name: api\n")

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !provisioned.Changed {
		t.Error("Changed = false, want true: the sandbox was created")
	}
	for _, want := range [][]string{
		{"docker build", "api-sandbox"},
		{"docker image save", "api-sandbox"},
		{"sbx template load"},
		{"sbx create", "--template api-sandbox", "--name api", "claude"},
	} {
		if !f.ran(want...) {
			t.Errorf("never ran %v\ncalls: %v", want, f.calls)
		}
	}
}

func TestSbxCreatePassesTheResolvedEnvironment(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nenv:\n  B: 2\n  A: 1\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx create", "-e A=1 -e B=2") {
		t.Errorf("environment was not passed in sorted order\ncalls: %v", f.calls)
	}
}

func TestSbxCreatePublishesTheConfiguredPorts(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nports:\n  - 8080:3000\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx create", "-p 8080:3000") {
		t.Errorf("the port was not published\ncalls: %v", f.calls)
	}
}

func TestSbxAllowsTheConfiguredDomains(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nnetwork:\n  - a.example.com\n  - b.example.com\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx policy allow network", "--sandbox api", "a.example.com,b.example.com") {
		t.Errorf("the network allowlist was never applied\ncalls: %v", f.calls)
	}
}

func TestSbxLeavesASandboxThatMatchesTheConfigAlone(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	f := &sbxFake{sandboxes: []string{"api"}, hash: hash}

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provisioned.Changed {
		t.Error("Changed = true, want false: the sandbox already matches the config")
	}
	if f.ran("docker build") || f.ran("sbx create") {
		t.Errorf("an up-to-date sandbox was rebuilt\ncalls: %v", f.calls)
	}
}

func TestSbxReplacesASandboxTheConfigHasMovedOn(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "stale"}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx rm", "api") {
		t.Errorf("the stale sandbox was not removed\ncalls: %v", f.calls)
	}
	if !f.ran("sbx create") {
		t.Errorf("no sandbox was created to replace the stale one\ncalls: %v", f.calls)
	}
}

func TestSbxUsesAConfiguredTemplateWithoutBuilding(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\ntemplate: my-image:1\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if f.ran("docker build") {
		t.Errorf("lid built an image over a template the user supplied\ncalls: %v", f.calls)
	}
	if !f.ran("sbx create", "--template my-image:1") {
		t.Errorf("the configured template was not used\ncalls: %v", f.calls)
	}
}

func TestSbxRunsOnCreateScriptsInTheNewSandbox(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nscripts:\n  on_create:\n    - composer install\n")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx exec", "composer install") {
		t.Errorf("the on_create script never ran\ncalls: %v", f.calls)
	}
}

func TestSbxWarnsAboutTheKeysItDoesNotHonourYet(t *testing.T) {
	warnings, err := Get(config.BackendSbx).Validate(sbxCfg(t, "name: api\nmemory: be nice\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "memory") {
		t.Errorf("warnings = %v, want one naming memory", warnings)
	}
}

func TestSbxWarnsAboutNothingWhenTheConfigIsFullyHonoured(t *testing.T) {
	warnings, err := Get(config.BackendSbx).Validate(sbxCfg(t, "name: api\nenv:\n  A: 1\nnetwork:\n  - example.com\nports:\n  - 3000\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: every key here is honoured", warnings)
	}
}

func TestSbxRenderShowsTheDockerfileAndThePlan(t *testing.T) {
	rendered, err := Get(config.BackendSbx).Render(sbxCfg(t, "name: api\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(rendered, "FROM ") {
		t.Errorf("rendered output is not a Dockerfile:\n%s", rendered)
	}
	if !strings.Contains(rendered, "sbx create") {
		t.Errorf("rendered output does not show what lid would run:\n%s", rendered)
	}
}
