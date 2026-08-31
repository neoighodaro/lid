package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadIgnoresUnknownFields(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: lid\nruntimes:\n  node: \"24\"\nnetwork:\n  - api.github.com\n")

	if _, err := Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
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
