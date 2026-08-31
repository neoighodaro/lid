package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func cfgFor(t *testing.T, body string) *config.Config {
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

func render(t *testing.T, body string) string {
	t.Helper()
	out, err := Get(config.BackendNono).Render(cfgFor(t, body))
	if err != nil {
		t.Fatalf("Render: %v\nconfig:\n%s", err, body)
	}
	return out
}

// profileOf strips the generated header so the body can be asserted as JSON.
func profileOf(t *testing.T, body string) map[string]any {
	t.Helper()
	out := render(t, body)

	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			lines = append(lines, line)
		}
	}

	var profile map[string]any
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &profile); err != nil {
		t.Fatalf("profile body is not valid JSON: %v\n%s", err, out)
	}
	return profile
}

func groupsOf(t *testing.T, body string) []string {
	t.Helper()
	profile := profileOf(t, body)

	groups, ok := profile["groups"].(map[string]any)
	if !ok {
		t.Fatalf("profile has no groups object: %v", profile)
	}
	include, ok := groups["include"].([]any)
	if !ok {
		t.Fatalf("groups has no include array: %v", groups)
	}
	names := make([]string, 0, len(include))
	for _, g := range include {
		names = append(names, g.(string))
	}
	return names
}

func hasGroup(groups []string, want string) bool {
	for _, g := range groups {
		if g == want {
			return true
		}
	}
	return false
}

func TestNonoProfileCarriesAGeneratedHeaderNamingTheSourceConfig(t *testing.T) {
	out := render(t, "name: api\nbackend: nono\n")

	if !strings.HasPrefix(out, "//") {
		t.Errorf("profile does not open with a comment marking it generated:\n%s", out)
	}
	if !strings.Contains(out, "lid.yaml") {
		t.Errorf("header does not name the config to edit instead:\n%s", out)
	}
}

func TestNonoProfileExtendsOnlyABuiltInProfile(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	if got := profile["extends"]; got != "default" {
		t.Errorf("extends = %v, want \"default\": pack profiles need 'nono pull' first, so a generated profile cannot rely on one", got)
	}
}

func TestNonoProfileNamesTheSandbox(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	meta, ok := profile["meta"].(map[string]any)
	if !ok {
		t.Fatalf("profile has no meta object: %v", profile)
	}
	if meta["name"] != "api" {
		t.Errorf("meta.name = %v, want the sandbox name", meta["name"])
	}
}

func TestNonoProfileGivesTheWorkdirWriteAccess(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	workdir, ok := profile["workdir"].(map[string]any)
	if !ok {
		t.Fatalf("profile has no workdir object: %v", profile)
	}
	if workdir["access"] != "readwrite" {
		t.Errorf("workdir.access = %v, want readwrite: nono's default profile denies the cwd, so the agent could not edit the project", workdir["access"])
	}
}

func TestNonoProfileIsInteractive(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	if profile["interactive"] != true {
		t.Errorf("interactive = %v, want true: the agent is attached to a terminal", profile["interactive"])
	}
}

func TestNonoProfileIncludesEveryPlatformVariantOfAGroup(t *testing.T) {
	groups := groupsOf(t, "name: api\nbackend: nono\nagent: claude-code\n")

	for _, want := range []string{"claude_code_macos", "claude_code_linux"} {
		if !hasGroup(groups, want) {
			t.Errorf("groups missing %q: the profile must resolve on any host, not just the one that generated it\ngot %v", want, groups)
		}
	}
}

func TestNonoProfileMapsRuntimesToPolicyGroups(t *testing.T) {
	for _, tc := range []struct {
		runtimes string
		want     []string
	}{
		{"bun: \"1.4.0\"", []string{"bun_runtime"}},
		{"go: \"1.27\"", []string{"go_runtime"}},
		{"node: \"24\"", []string{"node_runtime"}},
		{"node: \"24\"\n  pnpm: \"11\"", []string{"node_runtime"}},
		{"python: \"3.12\"", []string{"python_runtime"}},
		{"rust: \"1.98.0\"", []string{"rust_runtime"}},
		{"php: \"8.5\"", []string{"homebrew_macos", "homebrew_linux"}},
	} {
		groups := groupsOf(t, "name: api\nbackend: nono\nruntimes:\n  "+tc.runtimes+"\n")
		for _, want := range tc.want {
			if !hasGroup(groups, want) {
				t.Errorf("runtimes %q: groups missing %q, got %v", tc.runtimes, want, groups)
			}
		}
	}
}

func TestNonoProfileListsEachGroupOnce(t *testing.T) {
	groups := groupsOf(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n  pnpm: \"11\"\n")

	seen := map[string]int{}
	for _, g := range groups {
		seen[g]++
	}
	if seen["node_runtime"] != 1 {
		t.Errorf("node_runtime appears %d times, want 1: node and pnpm share a group\n%v", seen["node_runtime"], groups)
	}
}

func TestNonoProfileRestrictsTheNetworkToTheConfiguredDomains(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\nnetwork:\n  - registry.npmjs.org\n")

	network, ok := profile["network"].(map[string]any)
	if !ok {
		t.Fatalf("profile has no network object: %v", profile)
	}
	if network["network_profile"] != "minimal" {
		t.Errorf("network_profile = %v, want minimal: without it nono leaves the network wide open and 'network:' would constrain nothing", network["network_profile"])
	}
	domains, ok := network["allow_domain"].([]any)
	if !ok || len(domains) != 1 || domains[0] != "registry.npmjs.org" {
		t.Errorf("allow_domain = %v, want the configured domain", network["allow_domain"])
	}
}

func TestNonoProfileOmitsAnEmptyDomainList(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	network := profile["network"].(map[string]any)
	if _, ok := network["allow_domain"]; ok {
		t.Errorf("allow_domain is present with nothing configured: %v", network)
	}
}

func TestNonoProfileInjectsTheResolvedEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FROM_FILE=1\nOVERRIDDEN=file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "name: api\nbackend: nono\nenv_file:\n  - .env\nenv:\n  OVERRIDDEN: inline\n"
	if err := os.WriteFile(filepath.Join(dir, "lid.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := Get(config.BackendNono).Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, `"FROM_FILE": "1"`) {
		t.Errorf("env_file entry missing from set_vars:\n%s", out)
	}
	if !strings.Contains(out, `"OVERRIDDEN": "inline"`) {
		t.Errorf("inline env does not win over env_file:\n%s", out)
	}
}

func TestNonoProfileOmitsAnEmptyEnvironment(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\n")

	if _, ok := profile["environment"]; ok {
		t.Errorf("environment is present with nothing configured: %v", profile["environment"])
	}
}

func TestNonoProfileIsByteIdenticalAcrossRenders(t *testing.T) {
	body := "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n  go: \"1.27\"\n  python: \"3.12\"\nenv:\n  B: 2\n  A: 1\n  C: 3\nnetwork:\n  - b.example.com\n  - a.example.com\n"

	first := render(t, body)
	for i := 0; i < 20; i++ {
		if got := render(t, body); got != first {
			t.Fatalf("profile varies between renders:\n%s\n---\n%s", first, got)
		}
	}
}

func TestNonoRejectsAnAgentItCannotSandbox(t *testing.T) {
	_, err := Get(config.BackendNono).Render(cfgFor(t, "name: api\nbackend: nono\nagent: gemini\n"))
	if err == nil {
		t.Fatal("Render accepted an agent with no nono groups")
	}
	if !strings.Contains(err.Error(), "gemini") || !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("error = %v, want it to name the agent and list the supported ones", err)
	}
}

func TestNonoSupportsTheAgentsItHasGroupsFor(t *testing.T) {
	for _, agent := range []string{"claude-code", "codex", "opencode", "shell"} {
		if _, err := Get(config.BackendNono).Render(cfgFor(t, "name: api\nbackend: nono\nagent: "+agent+"\n")); err != nil {
			t.Errorf("agent %s rejected: %v", agent, err)
		}
	}
}

func TestNonoWarnsAboutConfigItCannotHonour(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{"name: api\nbackend: nono\nclaude:\n  theme: dark\n", "claude"},
		{"name: api\nbackend: nono\nshell:\n  aliases:\n    ll: ls -la\n", "aliases"},
		{"name: api\nbackend: nono\nmemory: 4g\n", "memory"},
		{"name: api\nbackend: nono\nphp:\n  extensions:\n    - redis\nruntimes:\n  php: \"8.5\"\n", "extensions"},
		{"name: api\nbackend: nono\nworkspace: /workspace/api\n", "workspace"},
		{"name: api\nbackend: nono\nhandoff:\n  enabled: true\n", "handoff"},
		{"name: api\nbackend: nono\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\n", "bridges"},
		{"name: api\nbackend: nono\nscripts:\n  on_start:\n    - echo hi\n", "on_start"},
	} {
		warnings, err := Get(config.BackendNono).Validate(cfgFor(t, tc.body))
		if err != nil {
			t.Fatalf("Validate: %v\nconfig:\n%s", err, tc.body)
		}
		joined := strings.Join(warnings, "\n")
		if !strings.Contains(joined, tc.want) {
			t.Errorf("no warning mentions %q:\nconfig:\n%s\nwarnings: %v", tc.want, tc.body, warnings)
		}
	}
}

func TestNonoWarnsAboutNothingWhenTheConfigIsFullyHonoured(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\nnetwork:\n  - example.com\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestNonoWarningsAreStableAcrossCalls(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nmemory: 4g\nworkspace: /workspace/api\nclaude:\n  theme: dark\n")

	first, err := Get(config.BackendNono).Validate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, err := Get(config.BackendNono).Validate(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, "|") != strings.Join(first, "|") {
			t.Fatalf("warning order varies:\n%v\n%v", first, got)
		}
	}
}

func TestNonoVersionFloorMatchesTheNewestKeyTheProfileUses(t *testing.T) {
	for _, tc := range []struct {
		output string
		ok     bool
	}{
		{"nono 0.74.0", true},
		{"nono 0.63.0", true},
		{"nono 0.62.9", false},
		{"nono 1.0.0", true},
		{"nono 0.63.0-rc.1", true},
	} {
		err := checkNonoVersion(tc.output)
		if tc.ok && err != nil {
			t.Errorf("%q rejected: %v", tc.output, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%q accepted, but environment.set_vars only exists from 0.63.0", tc.output)
		}
	}
}

func TestNonoVersionCheckExplainsUnreadableOutput(t *testing.T) {
	if err := checkNonoVersion("something else entirely"); err == nil {
		t.Error("unparseable version output accepted")
	}
}

func TestNonoWarningsReadAsSentences(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nmemory: be brief\nworkspace: /workspace/api\nhandoff:\n  enabled: true\nclaude:\n  theme: dark\nshell:\n  aliases:\n    ll: ls -la\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\nscripts:\n  on_start:\n    - echo hi\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - redis\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Fatal("no warnings for a config full of unhonoured keys")
	}
	for _, warning := range warnings {
		if strings.Contains(warning, "::") {
			t.Errorf("warning has a doubled separator: %q", warning)
		}
	}
}
