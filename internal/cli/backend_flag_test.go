package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func rememberBackend(t *testing.T, dir, backend string) {
	t.Helper()
	cfg, err := config.LoadFor(dir, "", backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Remember(); err != nil {
		t.Fatal(err)
	}
}

func TestStartPrintTakesTheBackendAsAnArgument(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")

	got, err := run(t, dir, "start", "nono", "--print")
	if err != nil {
		t.Fatalf("start nono --print: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.out, `"extends": "nolabs-ai/claude"`) || strings.Contains(got.out, "FROM ") {
		t.Errorf("want the nono profile, got:\n%s", got.out)
	}
}

func TestStartPrintTakesTheBackendFlag(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\n")

	got, err := run(t, dir, "start", "-b", "nono", "--print")
	if err != nil {
		t.Fatalf("start -b nono --print: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.out, `"extends": "nolabs-ai/claude"`) {
		t.Errorf("want the nono profile, got:\n%s", got.out)
	}
}

func TestTheBackendGivenTwiceDifferentlyIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")

	for _, args := range [][]string{
		{"start", "nono", "--backend", "sbx", "--print"},
		{"init", "nono", "-b", "sbx"},
	} {
		_, err := run(t, dir, args...)
		if err == nil || !strings.Contains(err.Error(), "given twice") {
			t.Errorf("%v: err = %v, want the double backend refused", args, err)
		}
	}
}

func TestTheBackendGivenTwiceTheSameIsFine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")

	if got, err := run(t, dir, "start", "nono", "-b", "nono", "--print"); err != nil {
		t.Errorf("start nono -b nono --print: %v\n%s", err, got.err)
	}
}

func TestInitTakesTheBackendFlag(t *testing.T) {
	dir := t.TempDir()

	if got, err := run(t, dir, "init", "-b", "nono"); err != nil {
		t.Fatalf("init -b nono: %v\n%s", err, got.err)
	}
	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != config.BackendNono {
		t.Errorf("Backend = %q, want nono", cfg.Backend)
	}
}

func TestConfigSaysTheBackendCameFromTheFlag(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")

	got, err := run(t, dir, "config", "-b", "nono")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.out, "nono, from --backend") {
		t.Errorf("output:\n%s", got.out)
	}
}

func TestConfigSaysTheBackendIsRemembered(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	rememberBackend(t, dir, config.BackendNono)

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.out, "nono, remembered until 'lid stop'") {
		t.Errorf("output:\n%s", got.out)
	}
}

func TestConfigSaysTheBackendCameFromTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\n")

	got, err := run(t, dir, "config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.out, "nono, from lid.yaml") {
		t.Errorf("output:\n%s", got.out)
	}
}

func TestAnUnknownBackendFlagIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")

	_, err := run(t, dir, "config", "-b", "sbox")
	if err == nil || !strings.Contains(err.Error(), `no backend named "sbox"`) {
		t.Errorf("err = %v", err)
	}
}

func TestBuildIgnoresTheRememberedBackend(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	rememberBackend(t, dir, config.BackendNono)

	got, err := runBuild(t, dir, &execRecorder{}, "build", "--print")
	if err != nil {
		t.Fatalf("build --print: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.out, "FROM "+config.DefaultBaseImage) {
		t.Errorf("want the sbx Dockerfile, got:\n%s", got.out)
	}
}

func TestEachStarterShowsABackendsSection(t *testing.T) {
	for backend, other := range map[string]string{config.BackendSbx: "#   nono:", config.BackendNono: "#   sbx:"} {
		dir := t.TempDir()
		if _, err := run(t, dir, "init", backend); err != nil {
			t.Fatalf("init %s: %v", backend, err)
		}
		body := read(t, filepath.Join(dir, ".config", "lid", "lid.yaml"))
		if !strings.Contains(body, "# backends:") || !strings.Contains(body, other) {
			t.Errorf("the %s starter has no backends example for the other backend:\n%s", backend, body)
		}
	}
}

func TestStartAndRestartHelpMentionTheBackend(t *testing.T) {
	for cmd, want := range map[string]string{"start": "until 'lid stop'", "restart": "lid restart <backend>"} {
		got, err := run(t, t.TempDir(), cmd, "--help")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.out, want) {
			t.Errorf("%s --help is missing %q:\n%s", cmd, want, got.out)
		}
	}
}
