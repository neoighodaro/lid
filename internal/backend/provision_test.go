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
	missing   bool
	version   string
	reject    string
	installed []string
	calls     [][]string
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
			if len(argv) > 1 && argv[1] == "list" {
				return []byte(strings.Join(f.installed, "\n") + "\n"), nil
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

	got, err := Get("nono").Provision(h.host(), cfg, Options{})
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

	got, err := Get("nono").Provision(h.host(), cfg, Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if !h.ran("nono", "profile", "validate", got.Path) {
		t.Errorf("nothing validated the profile lid wrote: %v", h.calls)
	}
}

func TestProvisionLeavesAnUnchangedProfileAlone(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	b := Get("nono")
	h := &fakeHost{}
	if _, err := b.Provision(h.host(), cfg, Options{}); err != nil {
		t.Fatalf("first Provision: %v", err)
	}

	again := &fakeHost{}
	got, err := b.Provision(again.host(), cfg, Options{})
	if err != nil {
		t.Fatalf("second Provision: %v", err)
	}

	if got.Changed {
		t.Error("Changed = true when the config had not moved")
	}
	if again.ran("nono", "profile", "validate") {
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

	got, err := Get("nono").Provision(h.host(), cfg, Options{})
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
		_, err := Get("nono").Provision(h.host(), cfg, Options{})
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

func TestStartArgvLeavesTheAuditTrailOffUnlessAsked(t *testing.T) {
	for body, want := range map[string]bool{
		"name: lid\nbackend: nono\n":              true,
		"name: lid\nbackend: nono\naudit: true\n": false,
	} {
		argv, err := Get("nono").StartArgv(cfgFor(t, body), "/tmp/nono.jsonc")
		if err != nil {
			t.Fatalf("StartArgv: %v", err)
		}

		got := strings.Join(argv, " ")
		if strings.Contains(got, "--no-audit") != want {
			t.Errorf("%q: argv = %q, want --no-audit %v", body, got, want)
		}
		if want && strings.Index(got, "--no-audit") > strings.Index(got, " -- ") {
			t.Errorf("argv = %q, want --no-audit before the agent's command", got)
		}
	}
}

func TestStartArgvKeepsNoAuditOffNonoShell(t *testing.T) {
	argv, err := Get("nono").StartArgv(cfgFor(t, "name: lid\nagent: shell\n"), "/tmp/nono.jsonc")
	if err != nil {
		t.Fatalf("StartArgv: %v", err)
	}
	if got := strings.Join(argv, " "); strings.Contains(got, "--no-audit") {
		t.Errorf("argv = %q, but nono shell has no --no-audit flag", got)
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

	got, err := Get("nono").Provision((&fakeHost{}).host(), cfg, Options{})
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

func TestStartArgvGrantsTheProjectDirectoryWithoutPrompting(t *testing.T) {
	for _, body := range []string{"name: lid\n", "name: lid\nagent: shell\n"} {
		argv, err := Get("nono").StartArgv(cfgFor(t, body), "/tmp/nono.jsonc")
		if err != nil {
			t.Fatalf("StartArgv: %v", err)
		}
		if !contains(argv, "--allow-cwd") {
			t.Errorf("argv %q has no --allow-cwd, so nono asks to share the project on every start", strings.Join(argv, " "))
		}
	}
}

// ran reports whether the host was asked to run a command containing every
// fragment, so a test names the call it cares about rather than an index.
func (f *fakeHost) ran(fragments ...string) bool {
	for _, call := range f.calls {
		joined := strings.Join(call, " ")
		found := true
		for _, fragment := range fragments {
			if !strings.Contains(joined, fragment) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

func TestRenderExtendsTheAgentsPackProfile(t *testing.T) {
	profile, err := Get("nono").Render(cfgFor(t, "name: api\nbackend: nono\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, `"extends": "nolabs-ai/claude"`) {
		t.Errorf("profile does not extend the agent's pack:\n%s", profile)
	}
	if strings.Contains(profile, "claude_code_macos") {
		t.Errorf("profile still hand-picks the groups the pack owns:\n%s", profile)
	}
	if !strings.Contains(profile, "nolabs-ai/claude") || !strings.Contains(profile, `"packs"`) {
		t.Errorf("profile does not declare the pack it depends on:\n%s", profile)
	}
}

func TestRenderExtendsDefaultForAnAgentWithNoPack(t *testing.T) {
	profile, err := Get("nono").Render(cfgFor(t, "name: api\nbackend: nono\nagent: shell\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, `"extends": "default"`) {
		t.Errorf("an agent with no pack lost its base profile:\n%s", profile)
	}
	if strings.Contains(profile, `"packs"`) {
		t.Errorf("profile declares a pack dependency it does not have:\n%s", profile)
	}
}

func TestRenderKeepsRuntimeGroupsThePackDoesNotCarry(t *testing.T) {
	profile, err := Get("nono").Render(cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  go: \"1.25\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, "go_runtime") {
		t.Errorf("declared runtime lost its group:\n%s", profile)
	}
}

func TestProvisionInstallsTheAgentsPackOnAMachineWithoutIt(t *testing.T) {
	h := &fakeHost{}
	cfg := cfgFor(t, "name: api\nbackend: nono\n")

	got, err := Get("nono").Provision(h.host(), cfg, Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !h.ran("nono", "pull", "nolabs-ai/claude") {
		t.Errorf("nothing pulled the pack the profile extends: %v", h.calls)
	}
	if len(got.Notes) == 0 {
		t.Error("the pull went unreported, so a first run looks like it hung")
	}
}

func TestProvisionLeavesAnAlreadyInstalledPackAlone(t *testing.T) {
	h := &fakeHost{installed: []string{"nolabs-ai/claude        0.1.0   2026-08-31 19:59"}}

	got, err := Get("nono").Provision(h.host(), cfgFor(t, "name: api\nbackend: nono\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if h.ran("nono", "pull") {
		t.Errorf("re-pulled a pack that is already installed: %v", h.calls)
	}
	if len(got.Notes) != 0 {
		t.Errorf("notes = %v, want silence when nothing was installed", got.Notes)
	}
}

func TestProvisionChecksThePackEvenWhenTheProfileIsUnchanged(t *testing.T) {
	h := &fakeHost{}
	cfg := cfgFor(t, "name: api\nbackend: nono\n")

	if _, err := Get("nono").Provision(h.host(), cfg, Options{}); err != nil {
		t.Fatalf("first Provision: %v", err)
	}

	second := &fakeHost{}
	if _, err := Get("nono").Provision(second.host(), cfg, Options{}); err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	if !second.ran("nono", "pull", "nolabs-ai/claude") {
		t.Errorf("an unchanged profile skipped the pack check, so a fresh machine never gets it: %v", second.calls)
	}
}

func TestProvisionExplainsAFailedPackPull(t *testing.T) {
	h := &fakeHost{reject: "error: pack not found in registry"}

	f := wantFailure(t, func() error {
		_, err := Get("nono").Provision(h.host(), cfgFor(t, "name: api\nbackend: nono\n"), Options{})
		return err
	}())

	if !strings.Contains(f.What, "nolabs-ai/claude") {
		t.Errorf("the error does not name the pack: %+v", f)
	}
	if !strings.Contains(f.What, "not found in registry") {
		t.Errorf("nono's own diagnostic is lost: %+v", f)
	}
}

func TestProvisionPointsAtAllowCommandsWhenADeclaredRuntimeCannotInstall(t *testing.T) {
	got, err := Get("nono").Provision((&fakeHost{}).host(), cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, "npm") || !strings.Contains(joined, "allow_commands") {
		t.Errorf("node is declared but nothing says npm is denied or how to allow it: %v", got.Notes)
	}
}

func TestProvisionStopsPointingAtAllowCommandsOnceTheCommandIsAllowed(t *testing.T) {
	got, err := Get("nono").Provision((&fakeHost{}).host(), cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\nallow_commands:\n  - npm\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	for _, note := range got.Notes {
		if strings.Contains(note, "allow_commands") {
			t.Errorf("still pointing at a key the config already sets: %q", note)
		}
	}
}

func TestProvisionSaysNothingAboutARuntimeWhosePackageManagerIsNotDenied(t *testing.T) {
	got, err := Get("nono").Provision((&fakeHost{installed: []string{"nolabs-ai/claude 0.1.0"}}).host(),
		cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  go: \"1.27\"\n  rust: \"1.90\"\n  bun: \"1.3\"\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(got.Notes) != 0 {
		t.Errorf("go, rust and bun drive their own toolchains, which nono never denied: %v", got.Notes)
	}
}

func TestProvisionRepeatsNoPointerForAProfileItDidNotRewrite(t *testing.T) {
	h := &fakeHost{installed: []string{"nolabs-ai/claude 0.1.0"}}
	cfg := cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n")

	if _, err := Get("nono").Provision(h.host(), cfg, Options{}); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	got, err := Get("nono").Provision(h.host(), cfg, Options{})
	if err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	if len(got.Notes) != 0 {
		t.Errorf("every start repeats the pointer, which is the noise the grouping fixed: %v", got.Notes)
	}
}
