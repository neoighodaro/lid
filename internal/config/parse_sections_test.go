package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const klarBody = `name: klar
agent: claude-code
backend: nono

runtimes:
  node: "24"
  pnpm: "12.10.1"
network:
  - localhost
  - claude.ai
memory: |
  klar.money is a personal finance tool
claude:
  theme: dark

backends:
  nono:
    paths:
      read:
        - ~/Developer/ck/yulo-web
      deny:
        - ~/.bash_history

  sbx:
    packages:
      - ripgrep
      - jq
    network:
      - registry.npmjs.org
`

func writeLidYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	return filepath.Join(dir, "lid.yaml")
}

func parseFor(t *testing.T, body, backend string) *Config {
	t.Helper()
	cfg, err := ParseFor(writeLidYAML(t, body), backend)
	if err != nil {
		t.Fatalf("ParseFor(%q): %v\nconfig:\n%s", backend, err, body)
	}
	return cfg
}

func parseForErr(t *testing.T, body, backend string) error {
	t.Helper()
	_, err := ParseFor(writeLidYAML(t, body), backend)
	if err == nil {
		t.Fatalf("ParseFor(%q): want error, got nil\nconfig:\n%s", backend, body)
	}
	return err
}

func TestABackendSectionAppliesOnlyToItsBackend(t *testing.T) {
	sbx := parseFor(t, klarBody, "sbx")
	if !reflect.DeepEqual(sbx.Packages, []string{"ripgrep", "jq"}) {
		t.Errorf("sbx packages = %v", sbx.Packages)
	}
	if !reflect.DeepEqual(sbx.Network, []string{"localhost", "claude.ai", "registry.npmjs.org"}) {
		t.Errorf("sbx network = %v", sbx.Network)
	}
	if sbx.Paths.IsSet() {
		t.Errorf("sbx picked up nono's paths: %+v", sbx.Paths)
	}

	nono := parseFor(t, klarBody, "nono")
	if !reflect.DeepEqual(nono.Paths.Read, []string{"~/Developer/ck/yulo-web"}) {
		t.Errorf("nono paths.read = %v", nono.Paths.Read)
	}
	if len(nono.Packages) != 0 {
		t.Errorf("nono picked up sbx's packages: %v", nono.Packages)
	}
	if !reflect.DeepEqual(nono.Network, []string{"localhost", "claude.ai"}) {
		t.Errorf("nono network = %v", nono.Network)
	}
}

func TestTheFlagBackendOverridesTheFile(t *testing.T) {
	cfg := parseFor(t, "name: x\nbackend: nono\n", "sbx")

	if cfg.Backend != BackendSbx {
		t.Errorf("Backend = %q, want sbx", cfg.Backend)
	}
	if cfg.BaseImage != DefaultBaseImage {
		t.Errorf("BaseImage = %q, want the default an sbx config gets", cfg.BaseImage)
	}
}

func TestOverridingToNonoDoesNotDefaultABaseImage(t *testing.T) {
	cfg := parseFor(t, "name: x\n", "nono")

	if cfg.Backend != BackendNono || cfg.BaseImage != "" {
		t.Errorf("Backend = %q, BaseImage = %q; want nono and no base image", cfg.Backend, cfg.BaseImage)
	}
}

func TestBackendSourceSaysWhereTheBackendCameFrom(t *testing.T) {
	if got := parseFor(t, "name: x\nbackend: nono\n", "").BackendSource; got != SourceFile {
		t.Errorf("declared backend: source = %q, want file", got)
	}
	if got := parseFor(t, "name: x\n", "").BackendSource; got != SourceDefault {
		t.Errorf("no backend key: source = %q, want default", got)
	}

	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	cfg, err := LoadFor(dir, "", "sbx")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackendSource != SourceFlag {
		t.Errorf("LoadFor with a backend: source = %q, want flag", cfg.BackendSource)
	}
}

func TestLoadForRejectsAnUnknownBackend(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")

	_, err := LoadFor(dir, "", "sbox")
	if err == nil || !strings.Contains(err.Error(), `no backend named "sbox"`) {
		t.Errorf("err = %v, want the unknown backend named", err)
	}
}

func TestATypoInASectionIsAnUnknownField(t *testing.T) {
	err := parseForErr(t, "name: x\nbackends:\n  sbx:\n    pakages: [jq]\n", "")

	if !strings.Contains(err.Error(), `line 4: unknown field "pakages"`) {
		t.Errorf("err = %v, want the typo and its line", err)
	}
}

func TestATypoInTheBaseDoesNotLeakGoTypes(t *testing.T) {
	err := parseForErr(t, "name: x\nnetwrk: [a]\nbackends:\n  sbx: {}\n", "")

	if !strings.Contains(err.Error(), `line 2: unknown field "netwrk"`) || strings.Contains(err.Error(), "struct") {
		t.Errorf("err = %v, want a clean unknown-field message", err)
	}
}

func TestANestedBackendsKeyIsAnUnknownField(t *testing.T) {
	err := parseForErr(t, "name: x\nbackends:\n  sbx:\n    backends: {}\n", "")

	if !strings.Contains(err.Error(), `unknown field "backends"`) {
		t.Errorf("err = %v, want a nested backends key rejected", err)
	}
}

func TestAnInactiveSectionIsStillChecked(t *testing.T) {
	err := parseForErr(t, "name: x\nbackend: sbx\nbackends:\n  nono:\n    packages: [jq]\n", "")

	if !strings.Contains(err.Error(), "backends.nono") || !strings.Contains(err.Error(), "cannot use packages") {
		t.Errorf("err = %v, want the nono section named", err)
	}
}

func TestCheckingSbxDoesNotLeakIntoTheNonoCheck(t *testing.T) {
	body := "name: x\nbackend: nono\nbackends:\n  sbx:\n    packages: [jq]\n  nono:\n    paths:\n      read: [/opt]\n"

	cfg := parseFor(t, body, "")
	if cfg.Backend != BackendNono || len(cfg.Packages) != 0 {
		t.Errorf("Backend = %q, Packages = %v", cfg.Backend, cfg.Packages)
	}
}

func TestTheSharedBaseIsStillCheckedForTheActiveBackend(t *testing.T) {
	err := parseForErr(t, "name: x\nbackend: nono\npackages: [jq]\nbackends:\n  sbx: {}\n", "")

	if !strings.Contains(err.Error(), "the nono backend cannot use packages") {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), "backends.") {
		t.Errorf("err = %v, want the active backend's error unprefixed", err)
	}
}

func TestASectionWithNoSectionForTheActiveBackendIsTheBase(t *testing.T) {
	base := "name: x\nnetwork: [a]\nruntimes:\n  python: 3.10\n"

	with := parseFor(t, base+"backends:\n  nono:\n    network: [b]\n", "sbx")
	without := parseFor(t, base, "sbx")
	with.Path, without.Path = "", ""
	with.ProjectDir, without.ProjectDir = "", ""
	if !reflect.DeepEqual(with, without) {
		t.Errorf("with a nono-only section:\n%+v\nwithout:\n%+v", with, without)
	}
}

func TestScalarLiteralsSurviveTheMerge(t *testing.T) {
	cfg := parseFor(t, "name: x\nbackends:\n  sbx:\n    runtimes:\n      python: 3.10\n", "sbx")

	if cfg.Runtimes["python"] != "3.10" {
		t.Errorf("python = %q, want 3.10", cfg.Runtimes["python"])
	}
}

func TestATemplateInTheSbxSectionRejectsSharedRuntimes(t *testing.T) {
	err := parseForErr(t, "name: x\nruntimes:\n  node: \"24\"\nbackends:\n  sbx:\n    template: my/template\n", "sbx")

	if !strings.Contains(err.Error(), "template and runtimes cannot both be set") {
		t.Errorf("err = %v", err)
	}
}

func TestASectionKeepsRepeatedScriptLines(t *testing.T) {
	cfg := parseFor(t, "name: x\nscripts:\n  on_start: [cd a, make, cd b, make]\nbackends:\n  sbx:\n    scripts:\n      on_start: [echo]\n", "sbx")

	if want := []string{"cd a", "make", "cd b", "make", "echo"}; !reflect.DeepEqual(cfg.Scripts.OnStart, want) {
		t.Errorf("on_start = %v, want %v", cfg.Scripts.OnStart, want)
	}
}

func TestAMisshapenSectionDoesNotLeakGoTypes(t *testing.T) {
	for _, body := range []string{
		"name: x\nbackends: [x]\n",
		"name: x\nbackends:\n  sbx: 5\n",
	} {
		err := parseForErr(t, body, "")
		if strings.Contains(err.Error(), "config.") || strings.Contains(err.Error(), "map[") {
			t.Errorf("err = %v, want no Go types", err)
		}
	}
}

func TestATypoInTheDeclaredBackendFailsEvenWhenOverridden(t *testing.T) {
	err := parseForErr(t, "name: x\nbackend: nono2\n", "nono")

	if !strings.Contains(err.Error(), `"nono2"`) {
		t.Errorf("err = %v, want the typo named", err)
	}
}
