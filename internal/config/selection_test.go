package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/failure"
)

func rememberNono(t *testing.T, dir string) {
	t.Helper()
	cfg, err := LoadFor(dir, "", BackendNono)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Remember(); err != nil {
		t.Fatal(err)
	}
}

func loadSelected(t *testing.T, dir, override, backend string) *Config {
	t.Helper()
	cfg, err := LoadSelected(dir, override, backend)
	if err != nil {
		t.Fatalf("LoadSelected: %v", err)
	}
	return cfg
}

func TestARememberedBackendWinsOverTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\nbackend: sbx\n")
	rememberNono(t, dir)

	cfg := loadSelected(t, dir, "", "")
	if cfg.Backend != BackendNono || cfg.BackendSource != SourceSelection || cfg.Selected != BackendNono {
		t.Errorf("Backend = %q, source = %q, Selected = %q; want nono from the selection", cfg.Backend, cfg.BackendSource, cfg.Selected)
	}
}

func TestLoadForIgnoresTheRecord(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\nbackend: sbx\n")
	rememberNono(t, dir)

	cfg, err := LoadFor(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != BackendSbx || cfg.BackendSource != SourceFile {
		t.Errorf("Backend = %q, source = %q; want the file's sbx", cfg.Backend, cfg.BackendSource)
	}
}

func TestTheFlagWinsOverTheRememberedBackend(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	rememberNono(t, dir)

	cfg := loadSelected(t, dir, "", BackendSbx)
	if cfg.Backend != BackendSbx || cfg.BackendSource != SourceFlag || cfg.Selected != BackendNono {
		t.Errorf("Backend = %q, source = %q, Selected = %q", cfg.Backend, cfg.BackendSource, cfg.Selected)
	}
}

func TestForgetReturnsToTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\nbackend: sbx\n")
	rememberNono(t, dir)

	cfg := loadSelected(t, dir, "", "")
	if err := cfg.Forget(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Forget(); err != nil {
		t.Errorf("forgetting twice: %v", err)
	}
	if cfg := loadSelected(t, dir, "", ""); cfg.BackendSource != SourceFile || cfg.Selected != "" {
		t.Errorf("source = %q, Selected = %q after Forget; want the file", cfg.BackendSource, cfg.Selected)
	}
}

func TestTheRecordDoesNotFollowAnotherConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	write(t, dir, "lid.sbx.yaml", "name: x\nbackend: sbx\n")
	rememberNono(t, dir)

	cfg := loadSelected(t, dir, filepath.Join(dir, "lid.sbx.yaml"), "")
	if cfg.Selected != "" || cfg.Backend != BackendSbx {
		t.Errorf("Selected = %q, Backend = %q; want the sidecar's sbx untouched by the record", cfg.Selected, cfg.Backend)
	}
}

func TestTheRecordFollowsTheDirectoryThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	rememberNono(t, dir)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	if cfg := loadSelected(t, link, "", ""); cfg.Selected != BackendNono {
		t.Errorf("Selected = %q through a symlink, want nono", cfg.Selected)
	}
}

func TestACorruptRecordIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	write(t, dir, ".config/lid/.kit/backend.json", "{")

	_, err := LoadSelected(dir, "", "")
	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("err = %v, want a failure", err)
	}
	if !strings.Contains(f.In, "backend.json") || !strings.Contains(f.Fix, "delete it") {
		t.Errorf("err = %+v, want it to name backend.json and say to delete it", f)
	}
}

func TestARecordNamingAnUnknownBackendIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	write(t, dir, ".config/lid/.kit/backend.json", `{"backend":"sbox","config":"`+filepath.Join(dir, "lid.yaml")+`"}`)

	if _, err := LoadSelected(dir, "", ""); err == nil {
		t.Error("a record naming sbox loaded without error")
	}
}

func TestRememberWritesIntoTheKitDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	rememberNono(t, dir)

	data, err := os.ReadFile(SelectionPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var sel Selection
	if err := json.Unmarshal(data, &sel); err != nil {
		t.Fatal(err)
	}
	if sel.Backend != BackendNono || !filepath.IsAbs(sel.Config) || filepath.Base(sel.Config) != "lid.yaml" {
		t.Errorf("record = %+v, want nono and the absolute config path", sel)
	}
	if want := filepath.Join(dir, ".config", "lid", ".kit", "backend.json"); SelectionPath(dir) != want {
		t.Errorf("SelectionPath = %q, want %q", SelectionPath(dir), want)
	}
}

func TestForgetSelectionClearsWithoutAConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: x\n")
	rememberNono(t, dir)

	if err := ForgetSelection(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SelectionPath(dir)); !os.IsNotExist(err) {
		t.Errorf("backend.json still there: %v", err)
	}
}
