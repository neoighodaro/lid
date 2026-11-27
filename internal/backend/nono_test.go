package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/panes"
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

func TestNonoProfileDeclaresAnyBaseThatIsNotBuiltIn(t *testing.T) {
	for _, agent := range []string{"claude-code", "codex", "opencode", "shell"} {
		profile := profileOf(t, "name: api\nbackend: nono\nagent: "+agent+"\n")

		base, _ := profile["extends"].(string)
		if base == nonoBaseProfile {
			continue
		}
		var packs []string
		entries, _ := profile["packs"].([]any)
		for _, entry := range entries {
			name, _ := entry.(string)
			packs = append(packs, name)
		}
		if !hasGroup(packs, base) {
			t.Errorf("agent %s extends %q without declaring it in packs, so nono cannot say what is missing when it is not installed", agent, base)
		}
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

func TestNonoProfileCoversThePlatformsTheAgentPackLeavesOut(t *testing.T) {
	groups := groupsOf(t, "name: api\nbackend: nono\nagent: claude-code\n")

	if !hasGroup(groups, "claude_code_linux") {
		t.Errorf("groups missing %q: the pack carries only the macOS variant, so a teammate on Linux would lose the agent's paths\ngot %v", "claude_code_linux", groups)
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
	if network["network_profile"] != "claude-code" {
		t.Errorf("network_profile = %v, want claude-code: without a profile nono leaves the network wide open and 'network:' would constrain nothing", network["network_profile"])
	}
	domains, ok := network["allow_domain"].([]any)
	if !ok || len(domains) != 1 || domains[0] != "registry.npmjs.org" {
		t.Errorf("allow_domain = %v, want the configured domain", network["allow_domain"])
	}
}

func TestNonoKitProfileNeverGrantsThePanesListener(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []any
	}{
		{"name: api\nbackend: nono\nnetwork: [github.com]\n", []any{"github.com"}},
		{"name: api\nbackend: nono\nnetwork: [github.com]\nherdr:\n  panes: true\n", []any{"github.com"}},
		{"name: api\nbackend: nono\nnetwork: [localhost]\nherdr:\n  panes: true\nhandoff:\n  enabled: true\n", []any{"localhost"}},
	} {
		network := profileOf(t, tc.body)["network"].(map[string]any)
		if got := network["allow_domain"]; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: allow_domain = %v, want %v", tc.body, got, tc.want)
		}
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
		{"name: api\nbackend: nono\nmemory: 4g\n", "memory"},
		{"name: api\nbackend: nono\nphp:\n  extensions:\n    - redis\nruntimes:\n  php: \"8.5\"\n", "extensions"},
		{"name: api\nbackend: nono\nworkspace: /workspace/api\n", "workspace"},
		{"name: api\nbackend: nono\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\n", "bridges"},
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
		{"nono 0.73.9", false},
		{"nono 0.63.0", false},
		{"nono 1.0.0", true},
		{"nono 0.74.0-rc.1", true},
	} {
		err := checkNonoVersion(tc.output)
		if tc.ok && err != nil {
			t.Errorf("%q rejected: %v", tc.output, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%q accepted, but the pack keys the profile uses are only verified from 0.74.0", tc.output)
		}
	}
}

func TestNonoVersionCheckExplainsUnreadableOutput(t *testing.T) {
	if err := checkNonoVersion("something else entirely"); err == nil {
		t.Error("unparseable version output accepted")
	}
}

func TestNonoWarningsReadAsSentences(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nmemory: be brief\nworkspace: /workspace/api\nclaude:\n  theme: dark\nshell:\n  aliases:\n    ll: ls -la\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - redis\n"))
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

func TestNonoStatesEachReasonOnceInsteadOfPerKey(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nmemory: notes.md\nworkspace: /workspace/api\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want the keys named once under a shared reason", warnings)
	}
	for _, key := range []string{"memory", "workspace"} {
		if !strings.Contains(warnings[0], key) {
			t.Errorf("warning %q does not name %s", warnings[0], key)
		}
	}
}

func TestNonoWarningCountDoesNotGrowWithTheConfig(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nmemory: notes.md\nworkspace: /workspace/api\nclaude:\n  theme: dark\nshell:\n  aliases:\n    ll: ls -la\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - redis\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 2 {
		t.Errorf("eight unhonoured keys produced %d warnings, want one per reason:\n%s", len(warnings), strings.Join(warnings, "\n"))
	}
}

func excludesOf(t *testing.T, body string) []string {
	t.Helper()
	profile := profileOf(t, body)

	groups, ok := profile["groups"].(map[string]any)
	if !ok {
		t.Fatalf("profile has no groups object: %v", profile)
	}
	exclude, _ := groups["exclude"].([]any)
	names := make([]string, 0, len(exclude))
	for _, g := range exclude {
		name, _ := g.(string)
		names = append(names, name)
	}
	return names
}

func TestNonoAllowCommandsExcludesTheGroupThatDeniesTheCommand(t *testing.T) {
	excludes := excludesOf(t, "name: api\nbackend: nono\nallow_commands:\n  - npm\n")

	if !hasGroup(excludes, "dangerous_commands") {
		t.Errorf("allowing npm did not exclude the group that denies it, so npm stays shimmed off PATH: %v", excludes)
	}
}

func TestNonoAllowCommandsExcludesEveryGroupTheCommandsSpanOnce(t *testing.T) {
	excludes := excludesOf(t, "name: api\nbackend: nono\nallow_commands:\n  - npm\n  - pip\n  - brew\n  - apt\n")

	want := []string{"dangerous_commands", "dangerous_commands_linux", "dangerous_commands_macos"}
	if len(excludes) != len(want) {
		t.Fatalf("got %v, want exactly %v: npm and pip share a group, so it must appear once", excludes, want)
	}
	for i, name := range want {
		if excludes[i] != name {
			t.Errorf("exclude[%d] is %q, want %q: the order must not shift between runs", i, excludes[i], name)
		}
	}
}

func TestNonoOmitsExcludeWhenTheConfigAllowsNoCommand(t *testing.T) {
	if excludes := excludesOf(t, "name: api\nbackend: nono\n"); len(excludes) != 0 {
		t.Errorf("a config that allows nothing still subtracts groups: %v", excludes)
	}
}

func TestNonoRejectsACommandItDoesNotDenyInTheFirstPlace(t *testing.T) {
	_, err := Get(config.BackendNono).Render(cfgFor(t, "name: api\nbackend: nono\nallow_commands:\n  - ls\n"))
	if err == nil {
		t.Fatal("lid accepted 'allow_commands: [ls]', which unblocks nothing and hides the typo")
	}
	if !strings.Contains(err.Error(), "ls") {
		t.Errorf("the error does not name the command that is wrong: %v", err)
	}
}

func TestNonoWarnsThatAllowingOneCommandUnblocksItsWholeGroup(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nallow_commands:\n  - npm\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "npm") || !strings.Contains(joined, "rm") {
		t.Errorf("nothing warns that allowing npm also unblocks rm, which nono denies in the same group: %v", warnings)
	}
}

// filesystemOf reads the profile's filesystem section as lists per key, which
// is absent when no declared runtime needs to write outside the project.
func filesystemOf(t *testing.T, body string) map[string][]string {
	t.Helper()
	profile := profileOf(t, body)

	section, ok := profile["filesystem"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string][]string, len(section))
	for key, raw := range section {
		entries, _ := raw.([]any)
		for _, entry := range entries {
			path, _ := entry.(string)
			out[key] = append(out[key], path)
		}
	}
	return out
}

func TestNonoGrantsWriteOnThePackageCacheOfADeclaredRuntime(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n")

	if !hasGroup(fs["allow"], "$HOME/.npm") {
		t.Errorf("node is declared but its npm cache is read-only, so 'npm install' cannot write _cacache: %v", fs)
	}
}

func TestNonoGrantsNothingForARuntimeTheConfigDoesNotDeclare(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\nruntimes:\n  go: \"1.27\"\n")

	if !hasGroup(fs["allow"], "$HOME/go/pkg") {
		t.Errorf("go is declared but its module cache is read-only, so 'go build' cannot populate it: %v", fs)
	}
	if hasGroup(fs["allow"], "$HOME/.npm") {
		t.Errorf("a go-only project was granted the node cache: %v", fs)
	}
}

func TestNonoGrantsNoWritesWhenNoDeclaredRuntimeNeedsToWrite(t *testing.T) {
	if fs := filesystemOf(t, "name: api\nbackend: nono\n"); len(fs["allow"]) > 0 {
		t.Errorf("a config with no runtimes still grants writes: %v", fs)
	}
	if fs := filesystemOf(t, "name: api\nbackend: nono\nruntimes:\n  php: \"8.4\"\n"); len(fs["allow"]) > 0 {
		t.Errorf("php writes through homebrew, which lid does not grant, yet writes were granted: %v", fs)
	}
}

func TestNonoKeepsExecutableDirectoriesOutOfTheRuntimeWriteGrant(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\nruntimes:\n  rust: \"1.90\"\n")

	if !hasGroup(fs["allow"], "$HOME/.cargo") {
		t.Fatalf("cargo cannot take its .package-cache lock without write on CARGO_HOME: %v", fs)
	}
	if !hasGroup(fs["deny"], "$HOME/.cargo/bin") {
		t.Errorf("granting CARGO_HOME left ~/.cargo/bin writable, so the sandbox can plant a binary the host runs: %v", fs)
	}
	for _, path := range fs["allow"] {
		if strings.HasSuffix(path, "/bin") {
			t.Errorf("granted %q, which is on the host PATH", path)
		}
	}
}

func TestNonoRuntimeGrantsKeepAFixedOrder(t *testing.T) {
	body := "name: api\nbackend: nono\nruntimes:\n  rust: \"1.90\"\n  node: \"24\"\n  go: \"1.27\"\n"

	first := filesystemOf(t, body)["allow"]
	for i := 0; i < 5; i++ {
		if got := filesystemOf(t, body)["allow"]; strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("the grant order shifted between renders: %v then %v", first, got)
		}
	}
	if strings.Join(first, ",") != "$HOME/go/pkg,$HOME/.npm,$HOME/.cargo" {
		t.Errorf("grants are not in config.Runtimes order: %v", first)
	}
}

func TestNonoNetworkProfileDefaultsToNonosProfileForTheAgent(t *testing.T) {
	for body, want := range map[string]string{
		"name: api\nbackend: nono\n":               "claude-code",
		"name: api\nbackend: nono\nagent: codex\n": "codex",
		"name: api\nbackend: nono\nagent: shell\n": "developer",
	} {
		network := profileOf(t, body)["network"].(map[string]any)
		if network["network_profile"] != want {
			t.Errorf("network_profile = %v, want %s\n%s", network["network_profile"], want, body)
		}
	}
}

func TestNonoNetworkProfileCanBeChosen(t *testing.T) {
	network := profileOf(t, "name: api\nbackend: nono\nnetwork_profile: minimal\n")["network"].(map[string]any)

	if network["network_profile"] != "minimal" {
		t.Errorf("network_profile = %v, want the configured minimal", network["network_profile"])
	}
}

// nono's own default is "block": false, so an unprofiled network is an open one.
func TestNonoNeverLeavesTheNetworkUnprofiled(t *testing.T) {
	for _, agent := range nonoAgentNames() {
		network := profileOf(t, "name: api\nbackend: nono\nagent: "+agent+"\n")["network"].(map[string]any)
		if profile, _ := network["network_profile"].(string); profile == "" {
			t.Errorf("agent %s renders no network_profile, which leaves nono blocking nothing", agent)
		}
	}
}

func TestNonoProfileAllowsTheConfiguredPortsToBeListenedOn(t *testing.T) {
	network := profileOf(t, "name: api\nbackend: nono\nports:\n  - 3000\n  - 5173/tcp\n")["network"].(map[string]any)

	ports, ok := network["listen_port"].([]any)
	if !ok || len(ports) != 2 || ports[0] != float64(3000) || ports[1] != float64(5173) {
		t.Errorf("listen_port = %v, want [3000 5173]", network["listen_port"])
	}
}

func TestNonoProfileOmitsListenPortWithNoPortsConfigured(t *testing.T) {
	network := profileOf(t, "name: api\nbackend: nono\n")["network"].(map[string]any)

	if _, ok := network["listen_port"]; ok {
		t.Errorf("listen_port is present with nothing configured: %v", network)
	}
}

func TestNonoWarnsThatItCannotRemapAPort(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nports:\n  - 8080:3000\n"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "3000") {
		t.Errorf("warnings = %v, want one naming the port the sandbox actually gets", warnings)
	}
}

func TestNonoRejectsAPortItCannotGrant(t *testing.T) {
	_, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nports:\n  - 5000/udp\n"))

	if err == nil {
		t.Fatal("a udp port was accepted, and nono grants TCP listeners only")
	}
	if !strings.Contains(err.Error(), "5000") {
		t.Errorf("error = %q, want it to name the port", err.Error())
	}
}

func TestNonoProfileDropsTheDeprecatedInteractiveKey(t *testing.T) {
	if _, ok := profileOf(t, "name: api\nbackend: nono\n")["interactive"]; ok {
		t.Error("the profile still sets interactive, which nono parses and ignores")
	}
}

func TestNonoExtendsTheCodexPack(t *testing.T) {
	profile := profileOf(t, "name: api\nbackend: nono\nagent: codex\n")

	if profile["extends"] != "nolabs-ai/codex" {
		t.Errorf("extends = %v, want the codex pack: reassembling what the pack maintains drifts from it", profile["extends"])
	}
}

type nonoStatusFake struct {
	sessions string
	calls    [][]string
}

func (f *nonoStatusFake) host() Host {
	return Host{
		LookPath: func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil },
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			return []byte(f.sessions), nil
		},
		Exec: func(argv []string, stdout, stderr io.Writer) error {
			f.calls = append(f.calls, argv)
			return nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func (f *nonoStatusFake) ran(fragment string) bool {
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), fragment) {
			return true
		}
	}
	return false
}

func provisionedNono(t *testing.T, body string) (*config.Config, *nonoStatusFake) {
	t.Helper()
	cfg := cfgFor(t, body)
	f := &nonoStatusFake{sessions: "[]"}
	if _, err := Get(config.BackendNono).Provision(f.host(), cfg, Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	f.calls = nil
	return cfg, f
}

func TestNonoReportsNoProfileAsNotCreated(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\n")
	f := &nonoStatusFake{sessions: "[]"}

	state, err := Get(config.BackendNono).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Exists {
		t.Error("a project that has never been started is reported as created")
	}
	if f.ran("nono ps") {
		t.Error("lid asked nono for sessions before there was a profile to match them against")
	}
}

func TestNonoWithAProfileAndNoSessionsIsStopped(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")

	state, err := Get(config.BackendNono).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Exists || state.Running || state.Stale {
		t.Fatalf("state = %+v, want a profile that exists, matches the config, and has no sessions", state)
	}
	if len(state.Rows) == 0 || !strings.Contains(state.Rows[0][1], "nono.jsonc") {
		t.Errorf("rows = %v, want the profile lid generated", state.Rows)
	}
}

func TestNonoCountsOnlyTheSessionsRunningThisProjectsProfile(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")
	f.sessions = fmt.Sprintf(`{"sessions": [
		{"id": "s1", "profile": %q},
		{"id": "s2", "profile": %q},
		{"id": "s3", "profile": "/somewhere/else/nono.jsonc"}
	]}`, nonoProfilePath(cfg), nonoProfilePath(cfg))

	state, err := Get(config.BackendNono).Status(f.host(), cfg, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Running {
		t.Fatal("sessions on this profile were not reported as running")
	}
	if got := state.Rows[len(state.Rows)-1]; got != [2]string{"sessions", "2"} {
		t.Errorf("last row = %v, want the two sessions on this profile and not the third", got)
	}
}

func TestNonoCallsTheProfileStaleWhenTheConfigMovedOn(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")
	if err := os.WriteFile(filepath.Join(cfg.ProjectDir, "lid.yaml"), []byte("name: api\nbackend: nono\nnetwork: [api.github.com]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := config.Load(cfg.ProjectDir, "")
	if err != nil {
		t.Fatal(err)
	}

	state, err := Get(config.BackendNono).Status(f.host(), changed, StatusOptions{Stale: true})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Stale {
		t.Error("a profile written before the config changed is not reported as stale")
	}
}

func TestNonoStopsEverySessionOnTheProfile(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")
	f.sessions = fmt.Sprintf(`[{"id": "s1", "profile": %q}, {"id": "s2", "profile": %q}, {"id": "s3", "profile": "/elsewhere"}]`,
		nonoProfilePath(cfg), nonoProfilePath(cfg))

	if err := Get(config.BackendNono).Stop(f.host(), cfg); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !f.ran("nono stop s1") || !f.ran("nono stop s2") {
		t.Errorf("calls = %v, want both sessions on this profile stopped", f.calls)
	}
	if f.ran("nono stop s3") {
		t.Error("lid stopped a session belonging to another project")
	}
}

func TestNonoSaysSoWhenTheProfileIsThereButUnreadable(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")
	path := nonoProfilePath(cfg)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Get(config.BackendNono).Status(f.host(), cfg, StatusOptions{Stale: true}); err == nil {
		t.Fatal("a profile lid could not read was reported as a project that was never started")
	}
}

func TestNonoDoesNotRenderTheProfileWhenNobodyAskedAboutStaleness(t *testing.T) {
	cfg, f := provisionedNono(t, "name: api\nbackend: nono\n")
	if err := os.WriteFile(filepath.Join(cfg.ProjectDir, "lid.yaml"), []byte("name: api\nbackend: nono\nnetwork: [api.github.com]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := config.Load(cfg.ProjectDir, "")
	if err != nil {
		t.Fatal(err)
	}

	state, err := Get(config.BackendNono).Status(f.host(), changed, StatusOptions{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !state.Exists {
		t.Fatal("the profile written by Provision was not found")
	}
	if state.Stale {
		t.Error("staleness was reported without being probed for")
	}
}

// A nono session is caged around the project, so anything the agent reads
// elsewhere - hooks a dotfiles repo owns, a tool's global config - is denied
// until the config names it. Without this the only way through is nono's own
// prompt, which has to be answered again every session.
func TestNonoGrantsTheHostPathsTheConfigNames(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\npaths:\n  read:\n    - ~/dotfiles/claude\n  read_file:\n    - ~/dotfiles/claude/CLAUDE.md\n  allow:\n    - ~/.cache/tool\n  deny:\n    - ~/.ssh\n")

	for key, want := range map[string]string{
		"read":      "$HOME/dotfiles/claude",
		"read_file": "$HOME/dotfiles/claude/CLAUDE.md",
		"allow":     "$HOME/.cache/tool",
		"deny":      "$HOME/.ssh",
	} {
		if !hasGroup(fs[key], want) {
			t.Errorf("the profile's filesystem.%s does not carry %s, so the session still refuses it: %v", key, want, fs)
		}
	}
}

func TestNonoDeniesOnlyLidsCacheDirWhenNothingElseNeedsTheFilesystem(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\n")

	if len(fs) != 1 || len(fs["deny"]) != 2 {
		t.Errorf("filesystem = %v, want only lid's own denies", fs)
	}
}

func TestNonoDeniesTheAgentLidsPaneDir(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	root, _ := panes.Root()
	dir, _ := panes.Path()

	for name, body := range map[string]string{
		"bare":            "name: api\nbackend: nono\n",
		"cache dir grant": "name: api\nbackend: nono\npaths:\n  allow:\n    - ~/\n",
	} {
		fs := filesystemOf(t, body)
		for _, want := range []string{root, dir} {
			want = "$HOME/" + strings.TrimPrefix(want, cache+"/")
			if !hasGroup(fs["deny"], want) {
				t.Errorf("%s: the agent can write %s, where lid keeps what runs on the host: %v", name, want, fs)
			}
		}
	}
}

// pnpm reads its global config before it reads a project, so granting the store
// alone leaves every command in a caged session asking for the same directory.
func TestNonoGrantsPnpmsGlobalConfigDirectoryAndNotOnlyItsStore(t *testing.T) {
	fs := filesystemOf(t, "name: api\nbackend: nono\nruntimes:\n  node: \"24\"\n  pnpm: \"11\"\n")

	if !hasGroup(fs["allow"], "$HOME/.config/pnpm") {
		t.Errorf("pnpm is declared but its global config directory is denied, so every pnpm command prompts: %v", fs)
	}
}

// seedLocalSettings puts a project-local settings file next to the config, the
// way a project that already keeps one would.
func seedLocalSettings(t *testing.T, cfg *config.Config, body string) string {
	t.Helper()
	path := filepath.Join(cfg.ProjectDir, claudeLocalSettingsFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func localSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatalf("settings are not JSON after lid wrote them: %v\n%s", err, body)
	}
	return settings
}

func TestNonoMergesTheClaudeBlockIntoTheProjectsOwnSettings(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  settings:\n    alwaysThinkingEnabled: true\n")
	path := seedLocalSettings(t, cfg, `{"permissions": {"allow": ["Bash(ls:*)"]}}`)

	if err := Get(config.BackendNono).PrepareAgent(Host{}, cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	settings := localSettings(t, path)
	if settings["alwaysThinkingEnabled"] != true {
		t.Errorf("got %v, want the setting the config names", settings["alwaysThinkingEnabled"])
	}
	if settings["permissions"] == nil {
		t.Error("the permissions the project already had were dropped")
	}
}

func TestNonoLeavesAProjectWithNoLocalSettingsFileAlone(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  settings:\n    alwaysThinkingEnabled: true\n")

	if err := Get(config.BackendNono).PrepareAgent(Host{}, cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.ProjectDir, claudeLocalSettingsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lid created a settings file the project never kept")
	}
}

func TestNonoConfigWinsOverWhatTheSettingsFileAlreadySaid(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  settings:\n    model: opus\n")
	path := seedLocalSettings(t, cfg, `{"model": "sonnet"}`)

	if err := Get(config.BackendNono).PrepareAgent(Host{}, cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if got := localSettings(t, path)["model"]; got != "opus" {
		t.Fatalf("model = %v, want the config's value to win", got)
	}
}

// A custom theme is a file lid writes into a sandbox's home. A nono session's
// home is the user's own, so the setting would point at a theme that is not
// there and Claude would fall back to its default anyway.
func TestNonoKeepsACustomThemeOutOfTheProjectsSettings(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  theme: ./theme.json\n  settings:\n    alwaysThinkingEnabled: true\n")
	path := seedLocalSettings(t, cfg, `{}`)

	if err := Get(config.BackendNono).PrepareAgent(Host{}, cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	settings := localSettings(t, path)
	if _, ok := settings["theme"]; ok {
		t.Errorf("theme = %v, want no theme naming a file the host does not have", settings["theme"])
	}
	if settings["alwaysThinkingEnabled"] != true {
		t.Error("the rest of the block should still land")
	}
}

func TestNonoCarriesABuiltInThemeIntoTheProjectsSettings(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  theme: dark\n")
	path := seedLocalSettings(t, cfg, `{}`)

	if err := Get(config.BackendNono).PrepareAgent(Host{}, cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if got := localSettings(t, path)["theme"]; got != "dark" {
		t.Fatalf("theme = %v, want the built-in theme the config names", got)
	}
}

func TestNonoSaysTheClaudeBlockIsIgnoredWhenThereIsNoFileToMergeInto(t *testing.T) {
	warnings, err := Get(config.BackendNono).Validate(cfgFor(t, "name: api\nbackend: nono\nclaude:\n  theme: dark\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(warnings, claudeLocalSettingsFile) {
		t.Fatalf("warnings = %v, want one naming the file the settings would have gone into", warnings)
	}
}

func TestNonoStopsCallingTheClaudeBlockIgnoredOnceItHasSomewhereToLand(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nclaude:\n  settings:\n    alwaysThinkingEnabled: true\n")
	seedLocalSettings(t, cfg, `{}`)

	warnings, err := Get(config.BackendNono).Validate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hasWarning(warnings, "ignoring claude") {
		t.Fatalf("warnings = %v, want no claim the block was ignored", warnings)
	}
}

func hasWarning(warnings []string, substring string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substring) {
			return true
		}
	}
	return false
}
