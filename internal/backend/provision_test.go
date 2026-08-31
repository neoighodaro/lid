package backend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/failure"
)

// fakeHost stands in for a machine with nono installed, recording what lid
// would run so the tests never need the real binary.
type fakeHost struct {
	missing bool
	version string
	reject  string
	calls   [][]string
}

func (f *fakeHost) host() Host {
	return Host{
		LookPath: func(name string) (string, error) {
			if f.missing {
				return "", errors.New("executable file not found in $PATH")
			}
			return "/opt/homebrew/bin/" + name, nil
		},
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if len(argv) > 1 && argv[1] == "--version" {
				version := f.version
				if version == "" {
					version = "nono 0.74.0"
				}
				return []byte(version + "\n"), nil
			}
			if f.reject != "" {
				return []byte(f.reject), errors.New("exit status 2")
			}
			return []byte("Result: valid\n"), nil
		},
	}
}

func wantFailure(t *testing.T, err error) *failure.Error {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("error is not a *failure.Error, so it renders as one flat line: %v", err)
	}
	return f
}

func TestEnsureAvailableSaysHowToInstallNonoWhenItIsMissing(t *testing.T) {
	h := &fakeHost{missing: true}

	f := wantFailure(t, Get("nono").EnsureAvailable(h.host()))

	if f.Fix == "" {
		t.Errorf("nothing tells the user how to get nono: %+v", f)
	}
	if len(h.calls) != 0 {
		t.Errorf("ran %v with no nono installed", h.calls)
	}
}

func TestEnsureAvailableRejectsANonoTooOldForTheProfilesLidWrites(t *testing.T) {
	h := &fakeHost{version: "nono 0.40.0"}

	f := wantFailure(t, Get("nono").EnsureAvailable(h.host()))

	if !strings.Contains(f.What, "0.40.0") {
		t.Errorf("the error does not name the version found: %+v", f)
	}
	if !strings.Contains(f.Fix, "update") {
		t.Errorf("the fix does not say to update: %+v", f)
	}
}

func TestEnsureAvailableAcceptsASupportedNono(t *testing.T) {
	h := &fakeHost{version: "nono 0.74.0"}

	if err := Get("nono").EnsureAvailable(h.host()); err != nil {
		t.Fatalf("EnsureAvailable: %v", err)
	}
}

func TestProvisionWritesTheProfileWhereNonoCanBeGivenIt(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  node: 22\n")
	h := &fakeHost{}

	got, err := Get("nono").Provision(h.host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if !got.Changed {
		t.Error("Changed = false on the first provision")
	}
	body, readErr := os.ReadFile(got.Path)
	if readErr != nil {
		t.Fatalf("no profile at %s: %v", got.Path, readErr)
	}
	if !strings.Contains(string(body), "node_runtime") {
		t.Errorf("the written profile is not the rendered one:\n%s", body)
	}
	if dir := filepath.Dir(got.Path); dir != cfg.KitDir() {
		t.Errorf("profile written to %s, want it under the kit dir %s", dir, cfg.KitDir())
	}
}

func TestProvisionHasNonoValidateTheProfileItWrote(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	h := &fakeHost{}

	got, err := Get("nono").Provision(h.host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if len(h.calls) != 1 {
		t.Fatalf("ran %d commands, want 1 validate: %v", len(h.calls), h.calls)
	}
	if want := strings.Join([]string{"nono", "profile", "validate", got.Path}, " "); strings.Join(h.calls[0], " ") != want {
		t.Errorf("argv = %q, want %q", strings.Join(h.calls[0], " "), want)
	}
}

func TestProvisionLeavesAnUnchangedProfileAlone(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	b := Get("nono")
	h := &fakeHost{}
	if _, err := b.Provision(h.host(), cfg); err != nil {
		t.Fatalf("first Provision: %v", err)
	}

	again := &fakeHost{}
	got, err := b.Provision(again.host(), cfg)
	if err != nil {
		t.Fatalf("second Provision: %v", err)
	}

	if got.Changed {
		t.Error("Changed = true when the config had not moved")
	}
	if len(again.calls) != 0 {
		t.Errorf("re-validated an unchanged profile: %v", again.calls)
	}
}

func TestProvisionRewritesAProfileLeftBehindByAnOlderConfig(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	path := filepath.Join(cfg.KitDir(), "nono.jsonc")
	if err := os.MkdirAll(cfg.KitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// stale\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &fakeHost{}

	got, err := Get("nono").Provision(h.host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if !got.Changed {
		t.Error("Changed = false with a stale profile on disk")
	}
	if body, _ := os.ReadFile(path); strings.Contains(string(body), "stale") {
		t.Errorf("the stale profile survived:\n%s", body)
	}
}

func TestProvisionReportsWhatNonoSaidWhenItRejectsTheProfile(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	h := &fakeHost{reject: "Error: unknown group 'node_runtime'\n"}

	f := wantFailure(t, func() error {
		_, err := Get("nono").Provision(h.host(), cfg)
		return err
	}())

	if !strings.Contains(f.What+f.Fix, "unknown group") {
		t.Errorf("nono's own complaint is nowhere in the error: %+v", f)
	}
}

func TestStartArgvRunsTheAgentUnderTheProfile(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")

	argv, err := Get("nono").StartArgv(cfg, "/tmp/nono.jsonc")
	if err != nil {
		t.Fatalf("StartArgv: %v", err)
	}

	got := strings.Join(argv, " ")
	for _, want := range []string{
		"nono run",
		"--profile /tmp/nono.jsonc",
		"--name lid",
		"--workdir " + cfg.ProjectDir,
		"-- claude",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q is missing %q", got, want)
		}
	}
}

func TestStartArgvHandsTheShellAgentToNonoShell(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nagent: shell\n")

	argv, err := Get("nono").StartArgv(cfg, "/tmp/nono.jsonc")
	if err != nil {
		t.Fatalf("StartArgv: %v", err)
	}

	got := strings.Join(argv, " ")
	if !strings.HasPrefix(got, "nono shell ") {
		t.Errorf("argv = %q, want it to start an interactive shell", got)
	}
	if strings.Contains(got, " -- ") {
		t.Errorf("argv = %q, want no command after the separator", got)
	}
}

func TestStartArgvRefusesAnAgentNonoCannotSandbox(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nagent: gemini\nbackend: nono\n")

	if _, err := Get("nono").StartArgv(cfg, "/tmp/nono.jsonc"); err == nil {
		t.Fatal("StartArgv accepted an agent the backend cannot launch")
	}
}

func TestEveryAgentNonoSandboxesHasACommandToLaunch(t *testing.T) {
	for agent := range nonoAgentGroups {
		if _, ok := nonoAgentCommands[agent]; !ok {
			t.Errorf("agent %q passes Validate but StartArgv cannot launch it", agent)
		}
	}
	for agent := range nonoAgentCommands {
		if _, ok := nonoAgentGroups[agent]; !ok {
			t.Errorf("agent %q can be launched but Validate rejects it", agent)
		}
	}
}

func TestProvisionNamesTheProfileRelativeToTheProject(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")

	got, err := Get("nono").Provision((&fakeHost{}).host(), cfg)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if filepath.IsAbs(got.Ref) {
		t.Errorf("Ref = %q, want a project-relative path", got.Ref)
	}
	if !strings.HasSuffix(got.Path, got.Ref) {
		t.Errorf("Ref %q does not name Path %q", got.Ref, got.Path)
	}
}
