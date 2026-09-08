package backend

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// settingsFake stands in for a sandbox that already has a settings.json of its
// own — the one the base image and the agent's own plugin installs leave behind.
type settingsFake struct {
	current string
	fail    bool
	calls   [][]string
}

func (f *settingsFake) host() Host {
	return Host{
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if f.fail {
				return []byte("Error: sandbox is not running\n"), errors.New("exit status 1")
			}
			if strings.Contains(strings.Join(argv, " "), "cat ") {
				return []byte(f.current), nil
			}
			return nil, nil
		},
	}
}

// written returns the settings body lid asked the sandbox to write.
func (f *settingsFake) written(t *testing.T) map[string]any {
	t.Helper()
	for _, call := range f.calls {
		script := call[len(call)-1]
		if !strings.Contains(script, "settings.json") || strings.HasPrefix(script, "cat ") {
			continue
		}
		_, body, found := strings.Cut(script, "<<'LID_FILE'\n")
		if !found {
			t.Fatalf("the write script has no heredoc:\n%s", script)
		}
		body, _, _ = strings.Cut(body, "\nLID_FILE")
		var settings map[string]any
		if err := json.Unmarshal([]byte(body), &settings); err != nil {
			t.Fatalf("lid wrote something that is not JSON: %v\n%s", err, body)
		}
		return settings
	}
	t.Fatalf("nothing was written to settings.json\ncalls: %v", f.calls)
	return nil
}

func TestPrepareAgentAppliesTheConfiguredModel(t *testing.T) {
	f := &settingsFake{current: `{"theme": "dark"}`}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if got := f.written(t)["model"]; got != "claude-opus-5" {
		t.Errorf("model = %v, want the model the config asks for", got)
	}
}

func TestPrepareAgentKeepsSettingsTheConfigSaysNothingAbout(t *testing.T) {
	f := &settingsFake{current: `{"statusLine": {"type": "command", "command": "dashboard"}}`}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if _, ok := f.written(t)["statusLine"]; !ok {
		t.Error("the sandbox's own statusLine was dropped, so the dashboard stops rendering")
	}
}

func TestPrepareAgentEnablesEveryPluginLidInstalls(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, "name: api\nclaude:\n  plugins:\n    install:\n      - dash@market\n      - powers@market\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	enabled, _ := f.written(t)["enabledPlugins"].(map[string]any)
	for _, plugin := range []string{"dash@market", "powers@market"} {
		if enabled[plugin] != true {
			t.Errorf("plugin %q is installed but not enabled: %v", plugin, enabled)
		}
	}
}

func TestPrepareAgentLetsTheConfigDisableAPluginItInstalls(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, `name: api
claude:
  plugins:
    install:
      - dash@market
  settings:
    enabledPlugins:
      "dash@market": false
`)

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	enabled, _ := f.written(t)["enabledPlugins"].(map[string]any)
	if enabled["dash@market"] != false {
		t.Errorf("enabledPlugins = %v, want the config to win over the install list", enabled)
	}
}

func TestPrepareAgentMergesIntoNestedObjectsRatherThanReplacingThem(t *testing.T) {
	f := &settingsFake{current: `{"enabledPlugins": {"already@market": true}}`}
	cfg := sbxCfg(t, "name: api\nclaude:\n  plugins:\n    install:\n      - dash@market\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	enabled, _ := f.written(t)["enabledPlugins"].(map[string]any)
	if enabled["already@market"] != true {
		t.Errorf("enabledPlugins = %v, want the plugins the sandbox already had", enabled)
	}
}

func TestPrepareAgentWritesNothingWhenTheSandboxAlreadyMatches(t *testing.T) {
	f := &settingsFake{current: `{"model": "claude-opus-5"}`}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "LID_SETTINGS") {
			t.Errorf("rewrote settings that already match the config: %v", call)
		}
	}
}

func TestPrepareAgentLeavesSettingsItCannotParseAlone(t *testing.T) {
	f := &settingsFake{current: "this is not json"}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	err := Get("sbx").PrepareAgent(f.host(), cfg)
	if err == nil {
		t.Fatal("PrepareAgent silently accepted settings it could not read")
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "LID_SETTINGS") {
			t.Errorf("overwrote settings lid could not read first: %v", call)
		}
	}
}

func TestPrepareAgentSaysWhatWentWrongWhenTheSandboxCannotBeReached(t *testing.T) {
	f := &settingsFake{fail: true}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	f2 := wantFailure(t, Get("sbx").PrepareAgent(f.host(), cfg))
	if !strings.Contains(f2.What, "not running") {
		t.Errorf("the sandbox's own complaint is lost: %+v", f2)
	}
}

func TestPrepareAgentDoesNothingForAConfigWithNoClaudeSettings(t *testing.T) {
	f := &settingsFake{current: "{}"}

	if err := Get("sbx").PrepareAgent(f.host(), sbxCfg(t, "name: api\n")); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("probed the sandbox with nothing to apply: %v", f.calls)
	}
}

func TestPrepareAgentLeavesNonClaudeAgentsAlone(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, "name: api\nagent: codex\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("wrote claude settings for a sandbox running %s: %v", cfg.Agent, f.calls)
	}
}

// nono merges the claude: block into the project's own settings file, never
// into the ~/.claude/settings.json the user's own agent reads.
func TestNonoNeverRewritesTheSettingsInYourOwnHome(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, "name: api\nbackend: nono\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("nono").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a host session had its settings rewritten: %v", f.calls)
	}
}

// TestSettingsAreWrittenByAScriptARealShellAccepts runs the generated script
// against a real bash so the heredoc quoting is proven rather than assumed:
// settings are data, and a $ in one of them must reach the file intact.
func TestSettingsAreWrittenByAScriptARealShellAccepts(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n    env:\n      TOKEN: \"$KEEP_ME\"\n")
	body, changed, err := applySettings(`{"statusLine": {"command": "node dashboard"}}`, claudeSettings(cfg))
	if err != nil || !changed {
		t.Fatalf("applySettings: err=%v changed=%v", err, changed)
	}

	argv := sbxWriteHomeFileArgv(cfg, claudeSettingsFile, body)
	home := t.TempDir()
	cmd := exec.Command("bash", "-c", argv[len(argv)-1])
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the write script failed: %v\n%s", err, out)
	}

	written := map[string]any{}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("the shell wrote something that is not JSON: %v\n%s", err, data)
	}
	env, _ := written["env"].(map[string]any)
	if env["TOKEN"] != "$KEEP_ME" {
		t.Errorf("env = %v, want the shell to leave the value alone", env)
	}
	if _, ok := written["statusLine"]; !ok {
		t.Errorf("the sandbox's own settings did not survive the write:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json.lid")); err == nil {
		t.Error("the half-written file was left behind in ~/.claude")
	}
}

// body returns the content lid asked the sandbox to write to the named file.
func (f *settingsFake) body(t *testing.T, name string) (string, bool) {
	t.Helper()
	for _, call := range f.calls {
		script := call[len(call)-1]
		if !strings.Contains(script, "mv ") || !strings.Contains(script, name) {
			continue
		}
		_, body, found := strings.Cut(script, "<<'LID_FILE'\n")
		if !found {
			t.Fatalf("the write script has no heredoc:\n%s", script)
		}
		body, _, _ = strings.Cut(body, "\nLID_FILE")
		return body, true
	}
	return "", false
}

func TestPrepareAgentGivesTheAgentTheMemoryTheConfigWrites(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, "name: api\nmemory: |\n  Ship small changes.\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	body, ok := f.body(t, "CLAUDE.md")
	if !ok {
		t.Fatalf("the memory never reached the sandbox: %v", f.calls)
	}
	if !strings.Contains(body, "Ship small changes.") {
		t.Errorf("memory = %q, want what the config says", body)
	}
}

func TestPrepareAgentLeavesTheAgentsMemoryAloneWhenTheConfigHasNone(t *testing.T) {
	f := &settingsFake{current: "{}"}
	cfg := sbxCfg(t, "name: api\nclaude:\n  settings:\n    model: claude-opus-5\n")

	if err := Get("sbx").PrepareAgent(f.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if body, ok := f.body(t, "CLAUDE.md"); ok {
		t.Errorf("wrote memory %q for a config that asks for none", body)
	}
}

func TestPrepareAgentSaysWhatWentWrongWhenTheMemoryCannotBeWritten(t *testing.T) {
	f := &settingsFake{fail: true}
	cfg := sbxCfg(t, "name: api\nmemory: be careful\n")

	err := Get("sbx").PrepareAgent(f.host(), cfg)
	if err == nil {
		t.Fatal("PrepareAgent said nothing about a sandbox it could not reach")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error = %q, want it to name the sandbox", err)
	}
}

// Memory is prose the user wrote, so backticks, $ and quotes in it must reach
// the file as typed rather than being run by the shell that writes it.
func TestMemoryIsWrittenByAScriptARealShellAccepts(t *testing.T) {
	cfg := sbxCfg(t, "name: api\n")
	memory := "Run `date` never. Keep $HOME alone. \"Quote\" freely."

	argv := sbxWriteHomeFileArgv(cfg, claudeMemoryFile, memory)
	home := t.TempDir()
	cmd := exec.Command("bash", "-c", argv[len(argv)-1])
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the write script failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != memory {
		t.Errorf("memory = %q, want %q", data, memory)
	}
}

func TestSettingsSurviveAnUpdateNoticePrintedAheadOfThem(t *testing.T) {
	current := "  update 1.4.0 available (current: 1.3.0)\n{\n  \"theme\": \"dark\"\n}\n"

	body, changed, err := applySettings(current, map[string]any{"themeId": "lid"})
	if err != nil {
		t.Fatalf("applySettings: %v", err)
	}
	if !changed {
		t.Fatal("want the settings rewritten with the new key")
	}
	if !strings.Contains(body, `"theme": "dark"`) {
		t.Fatalf("got %q, want the theme the sandbox already had kept", body)
	}
}
