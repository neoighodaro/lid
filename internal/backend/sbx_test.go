package backend

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
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

func (f *sbxFake) lines() string {
	var out []string
	for _, call := range f.calls {
		out = append(out, strings.Join(call, " "))
	}
	return strings.Join(out, "\n")
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

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{})
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

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nenv:\n  B: 2\n  A: 1\n"), Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx create", "-e A=1 -e B=2") {
		t.Errorf("environment was not passed in sorted order\ncalls: %v", f.calls)
	}
}

func TestSbxCreatePublishesTheConfiguredPorts(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nports:\n  - 8080:3000\n"), Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx create", "-p 8080:3000") {
		t.Errorf("the port was not published\ncalls: %v", f.calls)
	}
}

func TestSbxAllowsTheConfiguredDomains(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nnetwork:\n  - a.example.com\n  - b.example.com\n"), Options{}); err != nil {
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

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{})
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

func TestSbxKeepsASandboxTheConfigHasMovedOnAndSaysSo(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "stale"}

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if f.ran("sbx rm") {
		t.Errorf("a config edit destroyed a sandbox somebody could be working in\ncalls: %v", f.calls)
	}
	if !provisioned.Stale {
		t.Error("Stale = false, so nothing tells the user their edit has not been applied")
	}
}

func TestSbxRebuildReplacesTheSandbox(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "stale"}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\n"), Options{Rebuild: true}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx rm", "api") {
		t.Errorf("--rebuild left the old sandbox in place\ncalls: %v", f.calls)
	}
	if !f.ran("sbx create") {
		t.Errorf("no sandbox was created to replace the old one\ncalls: %v", f.calls)
	}
}

func TestSbxRebuildReplacesASandboxThatStillMatchesTheConfig(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	f := &sbxFake{sandboxes: []string{"api"}, hash: hash}

	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{Rebuild: true}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx rm", "api") {
		t.Errorf("--rebuild is how a user rebuilds a sandbox the config still matches\ncalls: %v", f.calls)
	}
}

func TestSbxSaysNothingIsStaleWhenTheSandboxMatches(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	f := &sbxFake{sandboxes: []string{"api"}, hash: hash}

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provisioned.Stale {
		t.Error("Stale = true for a sandbox built from this very config")
	}
}

func TestSbxSaysNothingIsStaleWhenTheSandboxHasNoRecordedHash(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}}

	provisioned, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\n"), Options{})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provisioned.Stale {
		t.Error("a sandbox lid cannot ask about was reported as drifted")
	}
}

func TestSbxUsesAConfiguredTemplateWithoutBuilding(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\ntemplate: my-image:1\n"), Options{}); err != nil {
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

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nscripts:\n  on_create:\n    - composer install\n"), Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx exec", "composer install") {
		t.Errorf("the on_create script never ran\ncalls: %v", f.calls)
	}
}

func TestSbxDoesNotWarnAboutClaudeSettingsItNowApplies(t *testing.T) {
	warnings, err := Get(config.BackendSbx).Validate(sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: claude.settings reaches the sandbox now", warnings)
	}
}

func TestSbxDoesNotWarnAboutAThemeItNowInstalls(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  theme: dark\n")
	warnings, err := Get(config.BackendSbx).Validate(cfg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: claude.theme reaches the sandbox now", warnings)
	}
}

// A built-in theme is a name Claude Code already knows, so it belongs in
// settings.json and nothing needs to be written into the sandbox's home.
func TestABuiltInThemeIsNamedInTheSettingsAndNotInstalled(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  theme: light-daltonized\n")

	settings := claudeSettings(cfg)
	if settings["theme"] != "light-daltonized" || settings["themeId"] != "light-daltonized" {
		t.Errorf("settings = %v, want theme and themeId set to light-daltonized", settings)
	}

	body, err := claudeThemeBody(cfg)
	if err != nil {
		t.Fatalf("claudeThemeBody: %v", err)
	}
	if body != "" {
		t.Errorf("body = %q, want nothing to install for a built-in theme", body)
	}
}

func TestAThemeFileIsInstalledUnderLidsOwnName(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  theme: ./mocha.json\n")
	if err := os.WriteFile(filepath.Join(cfg.ProjectDir, "mocha.json"), []byte(`{"base":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if id := claudeThemeID(cfg); id != "custom:lid" {
		t.Errorf("theme id = %q, want custom:lid", id)
	}

	f := &sbxFake{hash: "written"}
	if err := sbxApplyTheme(f.host(), cfg); err != nil {
		t.Fatalf("sbxApplyTheme: %v", err)
	}
	if !strings.Contains(f.lines(), claudeThemeFile) || !strings.Contains(f.lines(), `{"base":"dark"}`) {
		t.Errorf("the theme was never written into the sandbox\ncalls:\n%s", f.lines())
	}
}

// A theme path that does not resolve is the kind of typo that would otherwise
// leave the agent on its default theme with nothing said about it.
func TestAThemeThatCannotBeReadSaysSoRatherThanBeingSkipped(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  theme: ./nowhere.json\n")

	err := sbxApplyTheme((&sbxFake{hash: "written"}).host(), cfg)
	if err == nil {
		t.Fatal("a missing theme file was accepted")
	}
	if !strings.Contains(err.Error(), "nowhere.json") {
		t.Errorf("error = %q, want it to name the theme it could not read", err)
	}
}

func TestAThemeThatIsNotJSONIsRefused(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  theme: ./broken.json\n")
	if err := os.WriteFile(filepath.Join(cfg.ProjectDir, "broken.json"), []byte("base: dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := sbxApplyTheme((&sbxFake{hash: "written"}).host(), cfg)
	if err == nil {
		t.Fatal("a theme that is not JSON was accepted")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %q, want it to say the theme is not JSON", err)
	}
}

func TestSbxDoesNotWarnAboutMemoryItNowGivesTheAgent(t *testing.T) {
	warnings, err := Get(config.BackendSbx).Validate(sbxCfg(t, "name: api\nmemory: be nice\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: memory reaches the sandbox now", warnings)
	}
}

// Memory lands in ~/.claude/CLAUDE.md, which only Claude reads, so an agent lid
// has no memory file for still has to be told its memory is going nowhere.
func TestSbxStillWarnsAboutMemoryForAnAgentItCannotGiveItTo(t *testing.T) {
	warnings, err := Get(config.BackendSbx).Validate(sbxCfg(t, "name: api\nagent: codex\nmemory: be nice\n"))
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

func TestSbxBootstrapsTheDashboardShimWhenTheDashboardIsInstalled(t *testing.T) {
	f := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@claude-dashboard\n")

	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	// Without this the shim only appears once the plugin's own SessionStart hook
	// has run, which is after Claude already tried to render the status line.
	if !f.ran("sbx exec -w / api", "ensure-statusline.mjs", "CLAUDE_PLUGIN_DATA") {
		t.Errorf("the dashboard shim was never bootstrapped\ncalls: %v", f.calls)
	}
}

func TestSbxLeavesTheDashboardAloneWhenItIsNotInstalled(t *testing.T) {
	f := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - superpowers@claude-plugins-official\n")

	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if f.ran("ensure-statusline.mjs") {
		t.Errorf("lid ran the dashboard hook for a sandbox without the dashboard\ncalls: %v", f.calls)
	}
}

func TestSbxLeavesTheDashboardAloneForAnAgentThatIsNotClaude(t *testing.T) {
	f := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nagent: codex\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@claude-dashboard\n")

	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	if f.ran("ensure-statusline.mjs") {
		t.Errorf("lid ran a Claude plugin hook for agent codex\ncalls: %v", f.calls)
	}
}

func TestSbxFindsTheDashboardWhicheverMarketplaceItCameFrom(t *testing.T) {
	f := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@my-fork\n")

	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	// The plugin's data directory is named <plugin>-<marketplace>, so a fork's
	// shim lands somewhere else entirely.
	if !f.ran("cache/my-fork/claude-dashboard", "data/claude-dashboard-my-fork") {
		t.Errorf("the fork's cache and data paths were not used\ncalls: %v", f.calls)
	}
}

func TestTheDashboardShimScriptIsWhatARealShellAccepts(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	home := t.TempDir()

	cache := filepath.Join(home, ".claude", "plugins", "cache", "claude-dashboard", "claude-dashboard")
	for _, version := range []string{"1.9.0", "1.32.0"} {
		hooks := filepath.Join(cache, version, "hooks")
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, "ensure-statusline.mjs"), []byte("//"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Stub node so the script's plumbing is what is under test, not the plugin.
	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$CLAUDE_PLUGIN_ROOT\" \"$CLAUDE_PLUGIN_DATA\" \"$1\" > " + filepath.Join(bin, "ran") + "\n"
	if err := os.WriteFile(filepath.Join(bin, "node"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	argv := sbxDashboardShimArgv(cfg, "claude-dashboard")
	cmd := exec.Command("bash", "-c", argv[len(argv)-1])
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the shim script failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(bin, "ran"))
	if err != nil {
		t.Fatalf("the hook never ran: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// sort -V, not sort: 1.32.0 is newer than 1.9.0 but sorts before it.
	if want := filepath.Join(cache, "1.32.0"); lines[0] != want {
		t.Errorf("CLAUDE_PLUGIN_ROOT = %q, want %q", lines[0], want)
	}
	if want := filepath.Join(home, ".claude", "plugins", "data", "claude-dashboard-claude-dashboard"); lines[1] != want {
		t.Errorf("CLAUDE_PLUGIN_DATA = %q, want %q", lines[1], want)
	}
	if !strings.HasSuffix(lines[2], filepath.Join("1.32.0", "hooks", "ensure-statusline.mjs")) {
		t.Errorf("hook = %q, want the newest version's hook", lines[2])
	}
}

func TestTheDashboardShimScriptIsQuietWhenThePluginIsMissing(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")

	argv := sbxDashboardShimArgv(cfg, "claude-dashboard")
	cmd := exec.Command("bash", "-c", argv[len(argv)-1])
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("an empty sandbox aborted provisioning: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Errorf("output = %q, want nothing; provisioning noise reads as a failure", out)
	}
}

func TestSbxPointsTheStatusLineAtTheDashboardWithoutBeingAsked(t *testing.T) {
	f := &settingsFake{current: `{}`}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@claude-dashboard\n")

	if err := (sbx{}).PrepareAgent(f.host(), cfg); err != nil {
		t.Fatal(err)
	}

	line, ok := f.written(t)["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("no statusLine was written, so the dashboard renders nothing\nsettings: %v", f.written(t))
	}
	// Byte-identical to what the plugin writes itself, so its own
	// migrateStatusLine() recognises the form and never rewrites it.
	want := `node "/home/agent/.claude/plugins/data/claude-dashboard-claude-dashboard/statusline.mjs"`
	if line["command"] != want {
		t.Errorf("command = %v, want %q", line["command"], want)
	}
	if line["type"] != "command" {
		t.Errorf("type = %v, want \"command\"", line["type"])
	}
}

func TestSbxLetsTheConfigOverrideTheStatusLineItWouldPick(t *testing.T) {
	f := &settingsFake{current: `{}`}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@claude-dashboard\n  settings:\n    statusLine:\n      type: command\n      command: my-own-statusline\n")

	if err := (sbx{}).PrepareAgent(f.host(), cfg); err != nil {
		t.Fatal(err)
	}

	line := f.written(t)["statusLine"].(map[string]any)
	if line["command"] != "my-own-statusline" {
		t.Errorf("command = %v, want the config's own; lid overrode what the user asked for", line["command"])
	}
}

func TestSbxSetsNoStatusLineWithoutTheDashboard(t *testing.T) {
	f := &settingsFake{current: `{}`}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - superpowers@claude-plugins-official\n")

	if err := (sbx{}).PrepareAgent(f.host(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.written(t)["statusLine"]; ok {
		t.Error("lid set a dashboard statusLine for a sandbox without the dashboard")
	}
}

func TestSbxPointsTheStatusLineAtTheForkTheDashboardCameFrom(t *testing.T) {
	f := &settingsFake{current: `{}`}
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@my-fork\n")

	if err := (sbx{}).PrepareAgent(f.host(), cfg); err != nil {
		t.Fatal(err)
	}

	line := f.written(t)["statusLine"].(map[string]any)
	if !strings.Contains(line["command"].(string), "data/claude-dashboard-my-fork/statusline.mjs") {
		t.Errorf("command = %v, want the fork's own data directory", line["command"])
	}
}

// Auto mode's classifier calls api.anthropic.com from inside the sandbox, and
// without it every tool call that needs a verdict fails.
func TestSbxAllowsTheAnthropicAPIForClaude(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	if allow := sbxNetworkAllow(cfg); !contains(allow, "api.anthropic.com") {
		t.Errorf("allow = %v, want api.anthropic.com for claude-code", allow)
	}
}

func TestSbxAddsNoAgentHostsForOtherAgents(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nagent: codex\n")
	if allow := sbxNetworkAllow(cfg); contains(allow, "api.anthropic.com") {
		t.Errorf("allow = %v, want no anthropic host for a non-claude agent", allow)
	}
}

// The broker is reached through host.docker.internal but allowed as localhost,
// so the default URL must not put host.docker.internal in the policy.
func TestABrokerOnTheHostIsAllowedAsLocalhost(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")
	if allow := sbxNetworkAllow(cfg); !contains(allow, "localhost:8787") || contains(allow, "host.docker.internal:8787") {
		t.Errorf("allow = %v, want localhost:8787 and not host.docker.internal", allow)
	}
}

// A broker somewhere else on the network is not localhost, and allowing
// localhost for it would leave the sandbox blocked from the URL it was handed.
func TestABrokerElsewhereIsAllowedUnderItsOwnHost(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n  url: http://192.168.1.5:9000\n")
	if allow := sbxNetworkAllow(cfg); !contains(allow, "192.168.1.5:9000") || contains(allow, "localhost:9000") {
		t.Errorf("allow = %v, want [192.168.1.5:9000], the host the agent is told to reach", allow)
	}
}

// Swallowing this error to keep the handoff variables would hand the sandbox a
// create command with every configured variable quietly missing.
func TestAnEnvFileThatCannotBeReadStopsTheStart(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nenv_file:\n  - .nope\nenv:\n  NODE_ENV: development\n")

	_, err := Get(config.BackendSbx).Validate(cfg)
	if err == nil {
		t.Fatal("a missing env_file was accepted, and every variable would be dropped in silence")
	}
	var f *failure.Error
	if !errors.As(err, &f) || f.In != ".nope" {
		t.Errorf("error = %v (in %q), want it to name the entry it could not read", err, f.In)
	}
}

func TestSbxSaysWhenAnAgentGetsNoneOfItsClaudeBlock(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nagent: codex\nclaude:\n  settings:\n    model: x\nmemory: be nice\n")

	warnings, err := Get(config.BackendSbx).Validate(cfg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "claude") || !strings.Contains(warnings[0], "memory") {
		t.Errorf("warnings = %v, want one naming claude and memory", warnings)
	}
}

func TestPHPExtensionsWithoutThePHPRuntimeAreCalledOut(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nphp:\n  extensions:\n    - imagick\n")

	warnings, err := Get(config.BackendSbx).Validate(cfg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "php.extensions") {
		t.Errorf("warnings = %v, want one naming php.extensions", warnings)
	}

	withPHP := sbxCfg(t, "name: api\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - imagick\n")
	warnings, err = Get(config.BackendSbx).Validate(withPHP)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: the php runtime installs them", warnings)
	}
}

type sbxStatusFake struct {
	list  string
	hash  string
	calls [][]string
}

func (f *sbxStatusFake) host() Host {
	return Host{
		LookPath: func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if strings.Contains(strings.Join(argv, " "), "sbx ls") {
				return []byte(f.list), nil
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

func (f *sbxStatusFake) ran(fragment string) bool {
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), fragment) {
			return true
		}
	}
	return false
}

func TestSbxReportsASandboxItCannotFindAsNotCreated(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\n")
	f := &sbxStatusFake{list: `[{"id": "abc", "name": "other", "status": "running"}]`}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Exists {
		t.Error("a sandbox nobody created is reported as existing, so 'lid stop' would try to stop nothing")
	}
}

func TestSbxSeparatesAStoppedSandboxFromARunningOne(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\n")
	f := &sbxStatusFake{list: `[{"id": "abc123", "name": "api", "status": "stopped"}]`}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Exists || state.Running {
		t.Fatalf("state = %+v, want a sandbox that exists but is not running", state)
	}
	if len(state.Rows) == 0 || state.Rows[0][1] != "abc123" {
		t.Errorf("rows = %v, want the sandbox id sbx reported", state.Rows)
	}
	if f.ran(sbxConfigHash) {
		t.Error("lid read the config hash out of a stopped sandbox, which cannot answer")
	}
}

func TestSbxCallsARunningSandboxStaleWhenTheConfigMovedOn(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\npackages: [ripgrep]\n")
	f := &sbxStatusFake{list: `{"sandboxes": [{"id": "abc", "name": "api", "status": "running"}]}`, hash: "from-an-older-config"}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Running || !state.Stale {
		t.Errorf("state = %+v, want a running sandbox whose config has changed", state)
	}
}

func TestSbxDoesNotCallASandboxStaleWhenItMatchesTheConfig(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\npackages: [ripgrep]\n")
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatal(err)
	}
	f := &sbxStatusFake{list: `[{"id": "abc", "name": "api", "status": "running"}]`, hash: hash}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Stale {
		t.Error("a sandbox built from this very config is reported as stale, which asks for a pointless rebuild")
	}
}

func TestSbxStopLeavesTheSandboxInPlace(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\n")
	f := &sbxStatusFake{}

	if err := Get(config.BackendSbx).Stop(f.host(), cfg); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !f.ran("sbx stop api") {
		t.Errorf("calls = %v, want 'sbx stop api'", f.calls)
	}
	if f.ran("sbx rm") {
		t.Error("stop removed the sandbox, so the next start would rebuild it from scratch")
	}
}

func TestSbxSaysSoWhenItCannotReadWhatSbxPrinted(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\n")
	f := &sbxStatusFake{list: "not json at all"}

	if _, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{Stale: true}); err == nil {
		t.Fatal("unreadable output was treated as an empty list, so lid would say the sandbox is not created")
	}
}

func TestSbxFindsTheSandboxListPastAnEmptyKeyBesideIt(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\n")
	f := &sbxStatusFake{list: `{"errors": [], "results": [{"id": "abc", "name": "api", "status": "running"}]}`}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Exists {
		t.Error("an empty key sorted ahead of the sandboxes hid them, so lid reported a running sandbox as not created")
	}
}

func TestSbxDoesNotEnterTheSandboxWhenNobodyAskedAboutStaleness(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nbackend: sbx\npackages: [ripgrep]\n")
	f := &sbxStatusFake{list: `[{"id": "abc", "name": "api", "status": "running"}]`, hash: "from-an-older-config"}

	state, err := Get(config.BackendSbx).Status(f.host(), cfg, StatusOptions{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Running {
		t.Fatal("the running sandbox was not found")
	}
	if f.ran(sbxConfigHash) {
		t.Error("'lid stop' paid for a round trip into the sandbox to answer a question it never asks")
	}
	if state.Stale {
		t.Error("staleness was reported without being probed for")
	}
}

// TestLidsOwnCommandsInASandboxNameADirectoryThatAlwaysExists guards the case
// that made 'lid shell' unusable: a sandbox whose project directory has stopped
// answering. Without -w the runtime calls getcwd on the directory the sandbox
// starts in, which is the project, and refuses the command outright — so even
// reading a settings file under $HOME fails. Naming a directory makes it chdir
// instead, which resolves afresh and works.
func TestLidsOwnCommandsInASandboxNameADirectoryThatAlwaysExists(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nagent: claude-code\nworkspace: /workspace/api\nclaude:\n  plugins:\n    install:\n      - claude-dashboard@claude-dashboard\n")

	housekeeping := map[string][]string{
		"reading the agent's settings": sbxReadSettingsArgv(cfg),
		"writing a file into the home": sbxWriteHomeFileArgv(cfg, ".lid-shellrc", "body"),
		"recording the config hash":    sbxHashArgv(cfg, "abc"),
		"bootstrapping the dashboard":  sbxDashboardShimArgv(cfg, "claude-dashboard"),
		"reading the config hash":      sbxReadHashArgv(cfg),
	}
	for what, argv := range housekeeping {
		if !namesWorkingDir(argv) {
			t.Errorf("%s runs without -w, so it fails outright once the project directory stops answering: %v", what, argv)
		}
	}

	f := &sbxFake{}
	if _, err := Get(config.BackendSbx).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		if len(call) > 1 && call[0] == "sbx" && call[1] == "exec" && !namesWorkingDir(call) && !isOnCreateScript(call) {
			t.Errorf("provisioning runs a command without -w: %v", call)
		}
	}
}

// namesWorkingDir reports whether an argv tells the runtime which directory to
// run in rather than leaving it to inherit the sandbox's own.
func namesWorkingDir(argv []string) bool {
	for _, arg := range argv {
		if arg == "-w" {
			return true
		}
	}
	return false
}

// isOnCreateScript spots the one thing lid runs that does want the project
// directory: a script the user wrote, which expects to start where its project
// is.
func isOnCreateScript(argv []string) bool {
	for _, arg := range argv {
		if arg == "-lc" {
			return true
		}
	}
	return false
}

// TestAFailedCommandInASandboxDoesNotClaimTheSandboxIsDown covers a fix that
// used to be a guess. lid cannot tell a sandbox that is down from one that
// refused the command, and saying the wrong one sends whoever reads it after
// the wrong thing while the runtime's own explanation sits right above.
func TestAFailedCommandInASandboxDoesNotClaimTheSandboxIsDown(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nagent: claude-code\n")
	h := Host{
		LookPath: func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		Output: func(argv []string) ([]byte, error) {
			return []byte("OCI runtime exec failed: getcwd: Operation not permitted"), errors.New("exit status 1")
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	_, err := Get(config.BackendSbx).ShellArgv(h, cfg, "", nil)
	if err == nil {
		t.Fatal("a sandbox that refused the command is reported as fine")
	}
	if !strings.Contains(err.Error(), "getcwd") {
		t.Errorf("the runtime's own explanation is dropped: %v", err)
	}
	if strings.Contains(err.Error(), "has to be running") {
		t.Errorf("lid still asserts a cause it cannot know: %v", err)
	}
}

func TestSbxClonesTheProjectIntoTheSandboxWhenAsked(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\nclone: true\n"), Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !f.ran("sbx create", "--clone") {
		t.Errorf("the sandbox was not created in clone mode\ncalls: %v", f.calls)
	}
}

func TestSbxBindMountsTheProjectUnlessCloneIsAskedFor(t *testing.T) {
	f := &sbxFake{}

	if _, err := Get(config.BackendSbx).Provision(f.host(), sbxCfg(t, "name: api\n"), Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if f.ran("sbx create", "--clone") {
		t.Errorf("the sandbox was cloned without being asked\ncalls: %v", f.calls)
	}
}
