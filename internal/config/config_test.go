package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/failure"
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

func TestDiscoverPrefersRootLidYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	write(t, dir, ".config/lid/lid.yaml", "name: other\n")

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(dir, "lid.yaml"); got != want {
		t.Errorf("Discover = %q, want %q", got, want)
	}
}

func TestDiscoverFindsConfigDirLidYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".config/lid/lid.yaml", "name: lid\n")

	got, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(dir, ".config", "lid", "lid.yaml"); got != want {
		t.Errorf("Discover = %q, want %q", got, want)
	}
}

func TestDiscoverIgnoresNonCandidateFiles(t *testing.T) {
	for _, rel := range []string{"config.yaml", "lid.yml", ".config/lid.yaml", ".config/other/lid.yaml"} {
		dir := t.TempDir()
		write(t, dir, rel, "name: lid\n")

		if _, err := Discover(dir); err == nil {
			t.Errorf("Discover found %s; it must not be a candidate", rel)
		}
	}
}

func TestDiscoverErrorsWhenNoConfigExists(t *testing.T) {
	_, err := Discover(t.TempDir())
	if err == nil {
		t.Fatal("Discover: want error, got nil")
	}
	if !strings.Contains(err.Error(), "lid init") {
		t.Errorf("error = %q, want it to mention 'lid init'", err.Error())
	}
}

func TestLoadParsesNameAndAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "lid" {
		t.Errorf("Name = %q, want %q", cfg.Name, "lid")
	}
	if cfg.Agent != "codex" {
		t.Errorf("Agent = %q, want %q", cfg.Agent, "codex")
	}
	if want := filepath.Join(dir, "lid.yaml"); cfg.Path != want {
		t.Errorf("Path = %q, want %q", cfg.Path, want)
	}
}

func TestLoadDefaultsAgentToClaudeCode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent != "claude-code" {
		t.Errorf("Agent = %q, want %q", cfg.Agent, "claude-code")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nruntime:\n  node: \"24\"\n")

	_, err := Load(dir, "")
	if err == nil {
		t.Fatal("Load: want error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "runtime") {
		t.Errorf("error = %q, want it to name the unknown field", err.Error())
	}
}

func TestLoadUnknownFieldErrorDoesNotLeakGoTypes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nruntime: {}\n")

	_, err := Load(dir, "")
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	if strings.Contains(err.Error(), "config.Config") {
		t.Errorf("error = %q, want no Go type names", err.Error())
	}
}

func TestLoadHonoursOverridePath(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: ignored\n")
	write(t, dir, "custom/elsewhere.yaml", "name: chosen\n")
	override := filepath.Join(dir, "custom", "elsewhere.yaml")

	cfg, err := Load(dir, override)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Name != "chosen" {
		t.Errorf("Name = %q, want %q", cfg.Name, "chosen")
	}
}

func TestLoadErrorsWhenOverridePathMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir, filepath.Join(dir, "nope.yaml"))
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
}

func TestLoadRejectsMissingName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "agent: codex\n")

	_, err := Load(dir, "")
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, want it to mention 'name'", err.Error())
	}
}

func TestLoadRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"Lid", "-lid", "my lid", "lid_1"} {
		dir := t.TempDir()
		write(t, dir, "lid.yaml", "name: "+name+"\n")

		if _, err := Load(dir, ""); err == nil {
			t.Errorf("Load(name=%q): want error, got nil", name)
		}
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: [unclosed\n")

	if _, err := Load(dir, ""); err == nil {
		t.Fatal("Load: want error, got nil")
	}
}

func loadBody(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v\nconfig:\n%s", err, body)
	}
	return cfg
}

func loadBodyErr(t *testing.T, body string) error {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	_, err := Load(dir, "")
	if err == nil {
		t.Fatalf("Load: want error, got nil\nconfig:\n%s", body)
	}
	return err
}

func TestLoadParsesTheRepositoryConfig(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", ".config", "lid", "lid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no repository config at %s", path)
	}

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	if cfg.Name != "lid" {
		t.Errorf("Name = %q, want %q", cfg.Name, "lid")
	}
}

func TestLoadDefaultsBackendToSbx(t *testing.T) {
	cfg := loadBody(t, "name: lid\n")

	if cfg.Backend != DefaultBackend {
		t.Errorf("Backend = %q, want %q", cfg.Backend, DefaultBackend)
	}
}

func TestLoadAcceptsEveryKnownBackend(t *testing.T) {
	for _, backend := range Backends {
		cfg := loadBody(t, "name: lid\nbackend: "+backend+"\n")
		if cfg.Backend != backend {
			t.Errorf("Backend = %q, want %q", cfg.Backend, backend)
		}
	}
}

func TestLoadRejectsUnknownBackend(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nbackend: sbxx\n")

	if !strings.Contains(err.Error(), `unknown backend "sbxx"`) {
		t.Errorf("error = %v, want it to name the unknown backend", err)
	}
	for _, backend := range Backends {
		if !strings.Contains(err.Error(), backend) {
			t.Errorf("error = %v, want it to list %q", err, backend)
		}
	}
}

func TestUnknownBackendErrorListsBackendsInStableOrder(t *testing.T) {
	assertStableError(t, &Config{Name: "lid", Agent: DefaultAgent, Backend: "nope"})
}

func TestLoadDefaultsBaseImage(t *testing.T) {
	cfg := loadBody(t, "name: lid\n")

	if cfg.BaseImage != DefaultBaseImage {
		t.Errorf("BaseImage = %q, want %q", cfg.BaseImage, DefaultBaseImage)
	}
}

func TestLoadHonoursBaseImageOverride(t *testing.T) {
	cfg := loadBody(t, "name: lid\nbase_image: ubuntu:24.04\n")

	if cfg.BaseImage != "ubuntu:24.04" {
		t.Errorf("BaseImage = %q, want %q", cfg.BaseImage, "ubuntu:24.04")
	}
}

func TestLoadDoesNotDefaultBaseImageForNono(t *testing.T) {
	cfg := loadBody(t, "name: lid\nbackend: nono\n")

	if cfg.BaseImage != "" {
		t.Errorf("BaseImage = %q, want empty: nono builds no image", cfg.BaseImage)
	}
}

func TestNonoRejectsDockerOnlyKeys(t *testing.T) {
	for _, tc := range []struct{ field, body string }{
		{"base_image", "base_image: ubuntu:24.04\n"},
		{"template", "template: my-template\n"},
		{"packages", "packages:\n  - ripgrep\n"},
	} {
		err := loadBodyErr(t, "name: lid\nbackend: nono\n"+tc.body)
		if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("error for %s = %v, want it to name the field", tc.field, err)
		}
		if !strings.Contains(err.Error(), "nono") {
			t.Errorf("error for %s = %v, want it to name the backend", tc.field, err)
		}
	}
}

func TestNonoAcceptsRuntimes(t *testing.T) {
	cfg := loadBody(t, "name: lid\nbackend: nono\nruntimes:\n  node: 22\n")

	if cfg.Runtimes["node"] != "22" {
		t.Errorf("Runtimes[node] = %q, want %q", cfg.Runtimes["node"], "22")
	}
}

func TestLoadSetsProjectDir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".config/lid/lid.yaml", "name: lid\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProjectDir != dir {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, dir)
	}
}

func TestLoadParsesPackagesAndNetwork(t *testing.T) {
	cfg := loadBody(t, "name: lid\npackages:\n  - ripgrep\n  - jq\nnetwork:\n  - api.github.com\n")

	if len(cfg.Packages) != 2 || cfg.Packages[0] != "ripgrep" {
		t.Errorf("Packages = %v", cfg.Packages)
	}
	if len(cfg.Network) != 1 || cfg.Network[0] != "api.github.com" {
		t.Errorf("Network = %v", cfg.Network)
	}
}

func TestLoadParsesQuotedRuntimeVersions(t *testing.T) {
	cfg := loadBody(t, "name: lid\nruntimes:\n  node: \"24\"\n  go: \"1.27\"\n")

	if cfg.Runtimes["node"] != "24" {
		t.Errorf("node = %q, want %q", cfg.Runtimes["node"], "24")
	}
	if cfg.Runtimes["go"] != "1.27" {
		t.Errorf("go = %q, want %q", cfg.Runtimes["go"], "1.27")
	}
}

func TestLoadAcceptsUnquotedRuntimeVersions(t *testing.T) {
	cfg := loadBody(t, "name: lid\nruntimes:\n  node: 24\n")

	if cfg.Runtimes["node"] != "24" {
		t.Errorf("node = %q, want %q", cfg.Runtimes["node"], "24")
	}
}

func TestLoadPreservesTrailingZeroInUnquotedVersion(t *testing.T) {
	cfg := loadBody(t, "name: lid\nruntimes:\n  python: 3.10\n")

	if cfg.Runtimes["python"] != "3.10" {
		t.Errorf("python = %q, want %q — a float round-trip would give 3.1", cfg.Runtimes["python"], "3.10")
	}
}

func TestLoadRejectsUnknownRuntime(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nruntimes:\n  cobol: \"85\"\n")

	if !strings.Contains(err.Error(), "cobol") {
		t.Errorf("error = %q, want it to name the runtime", err.Error())
	}
}

func assertStableError(t *testing.T, cfg *Config) {
	t.Helper()
	var first string
	for i := 0; i < 50; i++ {
		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate: want error, got nil")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("error text varies between runs:\n%q\n%q", first, err.Error())
		}
	}
}

func TestUnknownRuntimeErrorListsRuntimesInStableOrder(t *testing.T) {
	assertStableError(t, &Config{
		Name:     "lid",
		Agent:    DefaultAgent,
		Backend:  DefaultBackend,
		Runtimes: map[string]Scalar{"cobol": "85"},
	})
}

func TestLoadRejectsPnpmWithoutNode(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nruntimes:\n  pnpm: \"11\"\n")

	if !strings.Contains(err.Error(), "node") {
		t.Errorf("error = %q, want it to mention node", err.Error())
	}
}

func TestLoadAcceptsPnpmWithNode(t *testing.T) {
	loadBody(t, "name: lid\nruntimes:\n  node: \"24\"\n  pnpm: \"11\"\n")
}

func TestLoadRejectsUnknownAgent(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nagent: clippy\n")

	if !strings.Contains(err.Error(), "clippy") {
		t.Errorf("error = %q, want it to name the agent", err.Error())
	}
}

func TestUnknownAgentErrorListsAgentsInStableOrder(t *testing.T) {
	assertStableError(t, &Config{Name: "lid", Agent: "clippy", Backend: DefaultBackend})
}

func TestLoadAcceptsPortForms(t *testing.T) {
	for _, port := range []string{"3000", "\"8080:80\"", "\"8080:80/tcp\"", "\"5432:5432/udp\""} {
		cfg := loadBody(t, "name: lid\nports:\n  - "+port+"\n")
		if len(cfg.Ports) != 1 {
			t.Fatalf("Ports = %v for %s", cfg.Ports, port)
		}
	}
}

func TestLoadRejectsBadPorts(t *testing.T) {
	for _, port := range []string{"\"0\"", "\"70000\"", "\"http\"", "\"8080:\"", "\"8080:80/sctp\""} {
		err := loadBodyErr(t, "name: lid\nports:\n  - "+port+"\n")
		if !strings.Contains(err.Error(), "port") {
			t.Errorf("error for %s = %q, want it to mention port", port, err.Error())
		}
	}
}

func TestLoadRejectsRelativeWorkspace(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nworkspace: workspace\n")

	if !strings.Contains(err.Error(), "workspace") {
		t.Errorf("error = %q, want it to mention workspace", err.Error())
	}
}

func TestLoadAcceptsAbsoluteWorkspace(t *testing.T) {
	cfg := loadBody(t, "name: lid\nworkspace: /workspace\n")

	if cfg.Workspace != "/workspace" {
		t.Errorf("Workspace = %q", cfg.Workspace)
	}
}

func TestLoadRejectsTemplateWithPackages(t *testing.T) {
	err := loadBodyErr(t, "name: lid\ntemplate: my/image:v1\npackages:\n  - jq\n")

	if !strings.Contains(err.Error(), "packages") {
		t.Errorf("error = %q, want it to mention packages", err.Error())
	}
}

func TestLoadRejectsTemplateWithRuntimes(t *testing.T) {
	err := loadBodyErr(t, "name: lid\ntemplate: my/image:v1\nruntimes:\n  node: \"24\"\n")

	if !strings.Contains(err.Error(), "runtimes") {
		t.Errorf("error = %q, want it to mention runtimes", err.Error())
	}
}

func TestLoadRejectsTemplateWithBridges(t *testing.T) {
	err := loadBodyErr(t, "name: lid\ntemplate: my/image:v1\nbridges:\n  - name: pgsql\n    port: 5432\n    host_port: 53432\n")

	if !strings.Contains(err.Error(), "bridges") {
		t.Errorf("error = %q, want it to mention bridges", err.Error())
	}
}

func TestLoadRejectsTemplateWithBaseImage(t *testing.T) {
	err := loadBodyErr(t, "name: lid\ntemplate: my/image:v1\nbase_image: debian:trixie\n")

	if !strings.Contains(err.Error(), "base_image") {
		t.Errorf("error = %q, want it to mention base_image", err.Error())
	}
}

// The default base image is what lid builds a generated Dockerfile on, so a
// custom template must not pick one up and trip the exclusion above.
func TestATemplateIsNotGivenABaseImageToIgnore(t *testing.T) {
	cfg := loadBody(t, "name: lid\ntemplate: my/image:v1\n")

	if cfg.BaseImage != "" {
		t.Errorf("BaseImage = %q, want none: the template is already built", cfg.BaseImage)
	}
}

func TestLoadParsesBridges(t *testing.T) {
	cfg := loadBody(t, "name: lid\nbridges:\n  - name: pgsql\n    port: 5432\n    host_port: 53432\n    env: DB_HOST\n")

	if len(cfg.Bridges) != 1 {
		t.Fatalf("Bridges = %v", cfg.Bridges)
	}
	b := cfg.Bridges[0]
	if b.Name != "pgsql" || b.Port != 5432 || b.HostPort != 53432 || b.Env != "DB_HOST" {
		t.Errorf("Bridge = %+v", b)
	}
}

func TestLoadRejectsBridgeWithoutName(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nbridges:\n  - port: 5432\n    host_port: 53432\n")

	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, want it to mention name", err.Error())
	}
}

func TestLoadRejectsBridgeWithBadPorts(t *testing.T) {
	for _, body := range []string{
		"name: lid\nbridges:\n  - name: pgsql\n    port: 0\n    host_port: 53432\n",
		"name: lid\nbridges:\n  - name: pgsql\n    port: 5432\n    host_port: 99999\n",
	} {
		err := loadBodyErr(t, body)
		if !strings.Contains(err.Error(), "port") {
			t.Errorf("error = %q, want it to mention port", err.Error())
		}
	}
}

func TestLoadParsesScripts(t *testing.T) {
	cfg := loadBody(t, "name: lid\nscripts:\n  on_create:\n    - pnpm install\n  on_start:\n    - ./warm.sh\n")

	if len(cfg.Scripts.OnCreate) != 1 || cfg.Scripts.OnCreate[0] != "pnpm install" {
		t.Errorf("OnCreate = %v", cfg.Scripts.OnCreate)
	}
	if len(cfg.Scripts.OnStart) != 1 || cfg.Scripts.OnStart[0] != "./warm.sh" {
		t.Errorf("OnStart = %v", cfg.Scripts.OnStart)
	}
}

func TestLoadRejectsDeprecatedStartupKey(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nstartup:\n  - pnpm install\n")

	if !strings.Contains(err.Error(), "startup") {
		t.Errorf("error = %q, want it to name the key", err.Error())
	}
}

func TestLoadParsesShellAndClaudeAndZellijAndMemory(t *testing.T) {
	cfg := loadBody(t, `name: lid
memory: |
  Be careful.
zellij:
  display_name: Lid
shell:
  directory: /home/agent/project
  aliases:
    g: git
claude:
  theme: ./theme.json
  plugins:
    marketplaces:
      - anthropics/claude-plugins-official
    install:
      - superpowers@claude-plugins-official
  settings:
    alwaysThinkingEnabled: true
php:
  extensions:
    - sqlite3
`)

	if cfg.Memory != "Be careful.\n" {
		t.Errorf("Memory = %q", cfg.Memory)
	}
	if cfg.Zellij.DisplayName != "Lid" {
		t.Errorf("Zellij.DisplayName = %q", cfg.Zellij.DisplayName)
	}
	if cfg.Shell.Directory != "/home/agent/project" || cfg.Shell.Aliases["g"] != "git" {
		t.Errorf("Shell = %+v", cfg.Shell)
	}
	if cfg.Claude.Theme != "./theme.json" {
		t.Errorf("Claude.Theme = %q", cfg.Claude.Theme)
	}
	if len(cfg.Claude.Plugins.Marketplaces) != 1 || len(cfg.Claude.Plugins.Install) != 1 {
		t.Errorf("Claude.Plugins = %+v", cfg.Claude.Plugins)
	}
	if cfg.Claude.Settings["alwaysThinkingEnabled"] != true {
		t.Errorf("Claude.Settings = %v", cfg.Claude.Settings)
	}
	if len(cfg.PHP.Extensions) != 1 || cfg.PHP.Extensions[0] != "sqlite3" {
		t.Errorf("PHP = %+v", cfg.PHP)
	}
}

func TestLoadAcceptsArbitraryClaudeSettings(t *testing.T) {
	cfg := loadBody(t, "name: lid\nclaude:\n  settings:\n    env:\n      SOME_FLAG: \"1\"\n    nested:\n      deeply:\n        ok: true\n")

	if cfg.Claude.Settings["nested"] == nil {
		t.Error("nested settings dropped")
	}
}

func TestLoadDoesNotDefaultHandoffURL(t *testing.T) {
	cfg := loadBody(t, "name: lid\nhandoff:\n  enabled: true\n")

	if cfg.Handoff.URL != "" {
		t.Errorf("Handoff.URL = %q, want empty — the backend supplies the default", cfg.Handoff.URL)
	}
}

func TestLoadRejectsHandoffURLWithoutPort(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nhandoff:\n  enabled: true\n  url: http://example.com\n")

	if !strings.Contains(err.Error(), "handoff") {
		t.Errorf("error = %q, want it to mention handoff", err.Error())
	}
}

func TestLoadAcceptsHandoffURLWithPort(t *testing.T) {
	cfg := loadBody(t, "name: lid\nhandoff:\n  enabled: true\n  url: http://localhost:8787\n")

	if cfg.Handoff.URL != "http://localhost:8787" {
		t.Errorf("Handoff.URL = %q", cfg.Handoff.URL)
	}
}

func TestLoadRejectsNetworkEntryWithScheme(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nnetwork:\n  - https://api.github.com\n")

	if !strings.Contains(err.Error(), "network") {
		t.Errorf("error = %q, want it to mention network", err.Error())
	}
}

func TestLoadRejectsTrailingHyphenName(t *testing.T) {
	loadBodyErr(t, "name: lid-\n")
}

func TestLoadRejectsOverlongName(t *testing.T) {
	loadBodyErr(t, "name: "+strings.Repeat("a", 64)+"\n")
}

func TestLoadAcceptsNameAtLengthLimit(t *testing.T) {
	loadBody(t, "name: "+strings.Repeat("a", 63)+"\n")
}

func TestLoadRejectsEmptyConfigFile(t *testing.T) {
	err := loadBodyErr(t, "")

	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, want it to mention the missing name", err.Error())
	}
}

func TestKitDirSitsUnderTheProjectConfigDir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, ".config", "lid", ".kit")
	if got := cfg.KitDir(); got != want {
		t.Errorf("KitDir = %q, want %q", got, want)
	}
}

func TestSandboxImageName(t *testing.T) {
	cfg := loadBody(t, "name: lid\n")

	if got := cfg.SandboxImageName(); got != "lid-sandbox" {
		t.Errorf("SandboxImageName = %q, want %q", got, "lid-sandbox")
	}
}

func TestFileHashIsStableAndContentDependent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	second, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	if first != second {
		t.Errorf("FileHash unstable: %q then %q", first, second)
	}

	write(t, dir, "lid.yaml", "name: lid\nagent: codex\n")
	third, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	if third == first {
		t.Error("FileHash did not change after the file changed")
	}
}

// The hash decides whether a sandbox is stale, so keys lid applies on every
// start must not appear in it: changing one would ask for a rebuild that
// rebuilds nothing.
func TestKeysAppliedOnEveryStartDoNotMakeASandboxStale(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}

	for _, added := range []struct {
		key  string
		body string
	}{
		{"claude.theme", "name: lid\nclaude:\n  theme: dark\n"},
		{"claude.settings", "name: lid\nclaude:\n  settings:\n    model: claude-opus-5\n"},
		{"memory", "name: lid\nmemory: be nice\n"},
		{"zellij", "name: lid\nzellij:\n  display_name: Lid\n"},
		{"shell", "name: lid\nshell:\n  aliases:\n    g: git\n"},
	} {
		write(t, dir, "lid.yaml", added.body)
		after, err := cfg.FileHash()
		if err != nil {
			t.Fatalf("FileHash: %v", err)
		}
		if after != before {
			t.Errorf("adding %s changed the hash, so the sandbox would be reported stale", added.key)
		}
	}

	write(t, dir, "lid.yaml", "name: lid\npackages:\n  - jq\n")
	after, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	if after == before {
		t.Error("adding packages left the hash alone, so a real image change would go unnoticed")
	}
}

func TestFileHashErrorsWhenFileGone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\n")
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.Path); err != nil {
		t.Fatal(err)
	}

	if _, err := cfg.FileHash(); err == nil {
		t.Fatal("FileHash: want error, got nil")
	}
}

func TestResolvedEnvMergesFilesThenInlineEnv(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "SHARED=from-first\nONLY_FIRST=1\n")
	write(t, dir, ".env.sandbox", "SHARED=from-second\n")
	write(t, dir, "lid.yaml", "name: lid\nenv_file:\n  - .env\n  - .env.sandbox\nenv:\n  SHARED: from-inline\n  INLINE: yes\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := cfg.ResolvedEnv()
	if err != nil {
		t.Fatalf("ResolvedEnv: %v", err)
	}

	if env["SHARED"] != "from-inline" {
		t.Errorf("SHARED = %q, want inline env to win", env["SHARED"])
	}
	if env["ONLY_FIRST"] != "1" {
		t.Errorf("ONLY_FIRST = %q", env["ONLY_FIRST"])
	}
	if env["INLINE"] != "yes" {
		t.Errorf("INLINE = %q", env["INLINE"])
	}
}

func TestLoadSucceedsWhenEnvFileMissing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nenv_file:\n  - .env\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load must not read env files: %v", err)
	}
	if _, err := cfg.ResolvedEnv(); err == nil {
		t.Fatal("ResolvedEnv: want error for missing .env, got nil")
	}
}

func TestResolvedEnvAcceptsAbsoluteEnvFilePaths(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "outside/.env", "FOO=bar\n")
	write(t, dir, "lid.yaml", "name: lid\nenv_file:\n  - "+filepath.Join(dir, "outside", ".env")+"\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := cfg.ResolvedEnv()
	if err != nil {
		t.Fatalf("ResolvedEnv: %v", err)
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q", env["FOO"])
	}
}

func TestSanitizeName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"lid", "lid"},
		{"My_Project", "my-project"},
		{"my lid app", "my-lid-app"},
		{"--Weird__Name--", "weird-name"},
		{"api.v2", "api-v2"},
		{"Ünïcödé", "ncd"},
		{"...", ""},
		{"", ""},
		{strings.Repeat("a", 80), strings.Repeat("a", 63)},
	} {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeNameProducesValidNames(t *testing.T) {
	for _, in := range []string{"My_Project", "my lid app", "--Weird__Name--", "api.v2", strings.Repeat("a", 80)} {
		cfg := &Config{Name: SanitizeName(in), Agent: DefaultAgent, Backend: DefaultBackend}
		if err := cfg.Validate(); err != nil {
			t.Errorf("SanitizeName(%q) = %q, which fails validation: %v", in, cfg.Name, err)
		}
	}
}

func TestStarterConfigParsesAndValidates(t *testing.T) {
	body, err := Starter(StarterOptions{Name: "my-project"})
	if err != nil {
		t.Fatalf("Starter: %v", err)
	}

	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("the starter config does not load: %v\n%s", err, body)
	}
	if cfg.Name != "my-project" {
		t.Errorf("Name = %q, want %q", cfg.Name, "my-project")
	}
	if cfg.Agent != DefaultAgent {
		t.Errorf("Agent = %q, want %q", cfg.Agent, DefaultAgent)
	}
}

func TestStarterRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "Not Valid", "-nope"} {
		if _, err := Starter(StarterOptions{Name: name}); err == nil {
			t.Errorf("Starter(%q): want error, got nil", name)
		}
	}
}

func TestOnlyClaudeCodeIsVerified(t *testing.T) {
	if len(VerifiedAgents) != 1 || VerifiedAgents[0] != DefaultAgent {
		t.Errorf("VerifiedAgents = %v, want just %q — add an agent here only once lid does its setup and it has been exercised", VerifiedAgents, DefaultAgent)
	}
}

func TestVerifiedAgentsAreAllKnown(t *testing.T) {
	for _, agent := range VerifiedAgents {
		if !contains(KnownAgents, agent) {
			t.Errorf("verified agent %q is missing from KnownAgents", agent)
		}
	}
}

func TestAgentVerified(t *testing.T) {
	if !AgentVerified("claude-code") {
		t.Error("AgentVerified(claude-code) = false, want true")
	}
	for _, agent := range []string{"codex", "gemini", "shell", "nonsense"} {
		if AgentVerified(agent) {
			t.Errorf("AgentVerified(%q) = true, want false", agent)
		}
	}
}

func TestLoadAcceptsKnownButUnverifiedAgent(t *testing.T) {
	cfg := loadBody(t, "name: lid\nagent: codex\n")

	if cfg.Agent != "codex" {
		t.Errorf("Agent = %q, want %q", cfg.Agent, "codex")
	}
	if AgentVerified(cfg.Agent) {
		t.Error("codex should not be reported as verified")
	}
}

func TestUnknownAgentErrorListsEveryKnownAgent(t *testing.T) {
	err := loadBodyErr(t, "name: lid\nagent: clode-code\n")

	for _, agent := range KnownAgents {
		if !strings.Contains(err.Error(), agent) {
			t.Errorf("error = %q, want it to list %q", err.Error(), agent)
		}
	}
}

func TestClaudeIsSet(t *testing.T) {
	if (Claude{}).IsSet() {
		t.Error("an empty claude section reports as set")
	}
	for name, c := range map[string]Claude{
		"theme":        {Theme: "dark"},
		"marketplaces": {Plugins: ClaudePlugins{Marketplaces: []string{"a/b"}}},
		"install":      {Plugins: ClaudePlugins{Install: []string{"x@y"}}},
		"settings":     {Settings: map[string]any{"k": "v"}},
	} {
		if !c.IsSet() {
			t.Errorf("claude with %s reports as unset", name)
		}
	}
}

func TestIsClaudeAgent(t *testing.T) {
	if !IsClaudeAgent(AgentClaudeCode) {
		t.Errorf("IsClaudeAgent(%q) = false", AgentClaudeCode)
	}
	for _, agent := range KnownAgents {
		if agent != AgentClaudeCode && IsClaudeAgent(agent) {
			t.Errorf("IsClaudeAgent(%q) = true", agent)
		}
	}
}

func TestConfigErrorsCarryTheirLocationAndFix(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"backend exclusion", "name: lid\nbackend: nono\npackages:\n  - ripgrep\n", "packages"},
		{"unknown backend", "name: lid\nbackend: sbxx\n", "backend"},
		{"unknown runtime", "name: lid\nruntimes:\n  cobol: \"85\"\n", "cobol"},
		{"template exclusion", "name: lid\ntemplate: Dockerfile\npackages:\n  - ripgrep\n", "packages"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "lid.yaml")
		if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := Parse(path)
		if err == nil {
			t.Fatalf("%s: Parse succeeded", tc.name)
		}

		var f *failure.Error
		if !errors.As(err, &f) {
			t.Errorf("%s: error is not a failure.Error: %v", tc.name, err)
			continue
		}
		if f.In != "lid.yaml" {
			t.Errorf("%s: In = %q, want the config relative to the project", tc.name, f.In)
		}
		if f.Fix == "" {
			t.Errorf("%s: no fix offered for %q", tc.name, f.What)
		}
		if !strings.Contains(f.What, tc.want) {
			t.Errorf("%s: What = %q, want it to mention %q", tc.name, f.What, tc.want)
		}
	}
}

func TestRefNamesTheConfigRelativeToTheProject(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, ".config", "lid")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "lid.yaml")
	if err := os.WriteFile(path, []byte("name: lid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Ref(); got != filepath.Join(".config", "lid", "lid.yaml") {
		t.Errorf("Ref() = %q, want the path relative to the project", got)
	}
}

func TestMissingConfigPutsTheDirectoryInTheLocationNotTheProblem(t *testing.T) {
	dir := t.TempDir()

	_, err := Load(dir, "")
	if err == nil {
		t.Fatal("Load succeeded with no config present")
	}

	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("error is not a failure.Error: %v", err)
	}
	if strings.Contains(f.What, dir) {
		t.Errorf("the directory crowds the problem line: %q", f.What)
	}
	if f.In != dir {
		t.Errorf("In = %q, want the directory searched", f.In)
	}
}

func TestEveryConfigErrorArrivesAsAFailureWithItsLocation(t *testing.T) {
	for _, body := range []string{
		"agent: claude-code\n",
		"name: Bad Name\n",
		"name: " + strings.Repeat("a", MaxNameLen+1) + "\n",
		"name: lid\nagent: nonesuch\n",
		"name: lid\nbackend: podman\n",
		"name: lid\nruntimes:\n  perl: \"5\"\n",
		"name: lid\nruntimes:\n  pnpm: \"10\"\n",
		"name: lid\nworkspace: relative/path\n",
		"name: lid\nnetwork:\n  - https://example.com\n",
		"name: lid\nnetwork:\n  - \"\"\n",
		"name: lid\nhandoff:\n  enabled: true\n  url: nope\n",
		"name: lid\nports:\n  - 99999\n",
		"name: lid\nports:\n  - not-a-port\n",
		"name: lid\nbridges:\n  - port: 5432\n    host_port: 5432\n",
		"name: lid\nbridges:\n  - name: db\n    port: 0\n    host_port: 5432\n",
		"name: lid\nbridges:\n  - name: db\n    port: 5432\n    host_port: 0\n",
		"name: lid\nnot_a_field: 1\n",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "lid.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := Parse(path)
		if err == nil {
			t.Errorf("config accepted:\n%s", body)
			continue
		}

		var f *failure.Error
		if !errors.As(err, &f) {
			t.Errorf("error renders as one flat line (%v) for:\n%s", err, body)
			continue
		}
		if f.In == "" {
			t.Errorf("error does not say which file it came from (%v) for:\n%s", err, body)
		}
	}
}

func TestResolvedEnvNamesTheEnvFileEntryAsWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lid.yaml")
	if err := os.WriteFile(path, []byte("name: lid\nenv_file:\n  - .env.missing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err = cfg.ResolvedEnv()

	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("error is not a *failure.Error: %v", err)
	}
	if f.In != ".env.missing" {
		t.Errorf("In = %q, want the env_file entry as written in the config", f.In)
	}
}

func TestStarterConfigParsesAndValidatesForEveryBackend(t *testing.T) {
	for _, backend := range Backends {
		body, err := Starter(StarterOptions{Name: "probe", Backend: backend})
		if err != nil {
			t.Fatalf("Starter(%s): %v", backend, err)
		}

		dir := t.TempDir()
		write(t, dir, "lid.yaml", body)

		cfg, err := Load(dir, "")
		if err != nil {
			t.Fatalf("the %s starter config does not load: %v\n%s", backend, err, body)
		}
		if cfg.Backend != backend {
			t.Errorf("the %s template wrote backend %q", backend, cfg.Backend)
		}
	}
}

// The templates duplicate the keys both backends honour, so nothing stops a new
// key landing in one and being forgotten in the other. This is that stop.
func TestStarterTemplatesOfferTheKeysBothBackendsHonour(t *testing.T) {
	shared := []string{"name:", "agent:", "backend:", "runtimes:", "network:", "ports:", "env:", "env_file:", "shell:"}

	for _, backend := range Backends {
		body, err := Starter(StarterOptions{Name: "probe", Backend: backend})
		if err != nil {
			t.Fatalf("Starter(%s): %v", backend, err)
		}
		for _, key := range shared {
			if !strings.Contains(body, key) {
				t.Errorf("the %s template does not offer %s, which that backend honours", backend, key)
			}
		}
	}
}

func TestStarterPinsRuntimesInTheOrderRuntimesLists(t *testing.T) {
	pins, err := ParseRuntimePins([]string{"rust@1.98.0", "node@24", "go@1.27"})
	if err != nil {
		t.Fatalf("ParseRuntimePins: %v", err)
	}

	body, err := Starter(StarterOptions{Name: "probe", Backend: BackendSbx, Runtimes: pins})
	if err != nil {
		t.Fatalf("Starter: %v", err)
	}

	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("a pinned starter config does not load: %v\n%s", err, body)
	}
	if len(cfg.Runtimes) != 3 || cfg.Runtimes["rust"] != "1.98.0" {
		t.Errorf("Runtimes = %v", cfg.Runtimes)
	}
	if got := strings.Index(body, "go:"); got > strings.Index(body, "node:") {
		t.Errorf("pins are not in Runtimes order:\n%s", body)
	}
}

func TestStarterRejectsABackendLidDoesNotHave(t *testing.T) {
	if _, err := Starter(StarterOptions{Name: "probe", Backend: "podman"}); err == nil {
		t.Error("Starter rendered a config for a backend lid cannot run")
	}
}

func TestSbxRejectsAllowCommandsItCannotLift(t *testing.T) {
	cfg := &Config{Name: "api", Agent: DefaultAgent, Backend: BackendSbx, AllowCommands: []string{"npm"}}

	if err := cfg.Validate(); err == nil {
		t.Error("allow_commands was accepted under sbx, where it silently does nothing")
	}
}

func TestNonoRejectsANetworkProfileNonoDoesNotHave(t *testing.T) {
	cfg := &Config{Name: "api", Agent: DefaultAgent, Backend: BackendNono, NetworkProfile: "wide-open"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("network_profile 'wide-open' was accepted, and nono would fail on it later")
	}
	if !strings.Contains(err.Error(), "minimal") {
		t.Errorf("error = %q, want it to name the profiles that exist", err.Error())
	}
}

func TestSbxRejectsANetworkProfileItCannotApply(t *testing.T) {
	cfg := &Config{Name: "api", Agent: DefaultAgent, Backend: BackendSbx, NetworkProfile: "minimal"}

	if err := cfg.Validate(); err == nil {
		t.Error("network_profile was accepted under sbx, where it silently does nothing")
	}
}

func TestStarterNonoTemplateOffersTheKeysOnlyNonoHonours(t *testing.T) {
	body, err := Starter(StarterOptions{Name: "probe", Backend: BackendNono})
	if err != nil {
		t.Fatalf("Starter: %v", err)
	}
	for _, key := range []string{"allow_commands:", "network_profile:"} {
		if !strings.Contains(body, key) {
			t.Errorf("the nono template does not offer %s, which only that backend honours", key)
		}
	}
}

// hashOfConfig loads a config from its own directory and returns the hash that
// decides whether a sandbox is stale.
func hashOfConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	return hash
}

// The keys lid applies on every start must not mark a sandbox stale: telling a
// user to rebuild for a change that needs no rebuild ends the session they are
// in for nothing.
func TestFileHashIgnoresTheKeysAppliedOnEveryStart(t *testing.T) {
	base := "name: api\npackages:\n  - jq\nclaude:\n  settings:\n    model: claude-opus-5\nmemory: be brief\nzellij:\n  display_name: api\n"
	for _, tt := range []struct {
		what string
		body string
	}{
		{"claude.settings", "name: api\npackages:\n  - jq\nclaude:\n  settings:\n    model: claude-sonnet-5\nmemory: be brief\nzellij:\n  display_name: api\n"},
		{"memory", "name: api\npackages:\n  - jq\nclaude:\n  settings:\n    model: claude-opus-5\nmemory: say more\nzellij:\n  display_name: api\n"},
		{"zellij.display_name", "name: api\npackages:\n  - jq\nclaude:\n  settings:\n    model: claude-opus-5\nmemory: be brief\nzellij:\n  display_name: web\n"},
	} {
		t.Run(tt.what, func(t *testing.T) {
			if hashOfConfig(t, base) != hashOfConfig(t, tt.body) {
				t.Errorf("changing %s marked the sandbox stale, but lid applies it on every start", tt.what)
			}
		})
	}
}

func TestFileHashStillNoticesWhatOnlyARebuildCanApply(t *testing.T) {
	base := "name: api\npackages:\n  - jq\n"
	for _, tt := range []struct {
		what string
		body string
	}{
		{"packages", "name: api\npackages:\n  - jq\n  - ripgrep\n"},
		{"claude.plugins", "name: api\npackages:\n  - jq\nclaude:\n  plugins:\n    install:\n      - superpowers@claude-plugins-official\n"},
		{"runtimes", "name: api\npackages:\n  - jq\nruntimes:\n  node: \"24\"\n"},
	} {
		t.Run(tt.what, func(t *testing.T) {
			if hashOfConfig(t, base) == hashOfConfig(t, tt.body) {
				t.Errorf("changing %s left the sandbox looking current, but only a rebuild applies it", tt.what)
			}
		})
	}
}

func TestATemplateAndOnStartCannotBothBeSet(t *testing.T) {
	err := loadBodyErr(t, "name: lid\ntemplate: my/image:v1\nscripts:\n  on_start:\n    - echo hi\n")

	if !strings.Contains(err.Error(), "on_start") {
		t.Errorf("error = %q, want it to name on_start: lid bakes it into the image it no longer generates", err.Error())
	}
}

func TestHandoffURLDefaultsPerBackend(t *testing.T) {
	sandbox := loadBody(t, "name: lid\nhandoff:\n  enabled: true\n")
	if got := sandbox.HandoffURL(); got != "http://host.docker.internal:8787" {
		t.Errorf("sbx HandoffURL = %q, want the host as a container sees it", got)
	}

	host := loadBody(t, "name: lid\nbackend: nono\nhandoff:\n  enabled: true\n")
	if got := host.HandoffURL(); got != "http://localhost:8787" {
		t.Errorf("nono HandoffURL = %q, want plain localhost", got)
	}
}

func TestHandoffPortFollowsTheConfiguredURL(t *testing.T) {
	cfg := loadBody(t, "name: lid\nhandoff:\n  enabled: true\n  url: http://host.docker.internal:9999\n")

	if got := cfg.HandoffPort(); got != 9999 {
		t.Errorf("HandoffPort = %d, want 9999", got)
	}
}

// The port has to be explicit because the network rule and the broker both read
// it back out of this URL; the scheme has to be one curl and lid's own broker
// can both speak.
func TestAHandoffURLMustBeHTTPWithAPort(t *testing.T) {
	for _, url := range []string{
		"host.docker.internal:8787",
		"http://host.docker.internal",
		"ftp://host.docker.internal:8787",
		"ws://host.docker.internal:8787",
	} {
		loadBodyErr(t, "name: api\nhandoff:\n  enabled: true\n  url: "+url+"\n")
	}
	for _, url := range []string{
		"http://host.docker.internal:8787",
		"https://broker.example.com:443",
	} {
		cfg := loadBody(t, "name: api\nhandoff:\n  enabled: true\n  url: "+url+"\n")
		if cfg.HandoffURL() != url {
			t.Errorf("HandoffURL = %q, want %q", cfg.HandoffURL(), url)
		}
	}
}
