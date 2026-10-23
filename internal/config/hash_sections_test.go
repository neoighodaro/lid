package config

import (
	"testing"
)

func hashFor(t *testing.T, body, backend string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", body)
	cfg, err := LoadFor(dir, "", backend)
	if err != nil {
		t.Fatalf("LoadFor(%q): %v\nconfig:\n%s", backend, err, body)
	}
	hash, err := cfg.FileHash()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// Pinned against the code before backends sections existed. Do not recompute
// it: a change here marks every existing sbx sandbox stale.
func TestTheHashOfAConfigWithoutSectionsIsUnchanged(t *testing.T) {
	body := "name: klar\nagent: claude-code\nbackend: sbx\nruntimes:\n  node: \"24\"\n  python: 3.10\npackages:\n  - ripgrep\nnetwork:\n  - localhost\n  - claude.ai\nports:\n  - 3000\nenv:\n  FOO: bar\nmemory: |\n  notes\nclaude:\n  theme: dark\n  plugins:\n    install:\n      - x@y\n"

	const want = "7d846cf23e5a80bfc713883385c0497ba8e0edffeefff5ffe0b74d713d66068d"
	if got := hashFor(t, body, ""); got != want {
		t.Errorf("hash = %s, want %s", got, want)
	}
}

const sectionedBody = "name: x\nbackend: sbx\nnetwork: [a]\nbackends:\n  sbx:\n    packages: [git]\n  nono:\n    paths:\n      read: [/opt]\n"

func TestEditingAnSbxSectionChangesTheSbxHash(t *testing.T) {
	edited := "name: x\nbackend: sbx\nnetwork: [a]\nbackends:\n  sbx:\n    packages: [git, jq]\n  nono:\n    paths:\n      read: [/opt]\n"

	if hashFor(t, sectionedBody, "sbx") == hashFor(t, edited, "sbx") {
		t.Error("adding a package to backends.sbx left the sbx hash unchanged")
	}
}

func TestEditingANonoSectionLeavesTheSbxHashAlone(t *testing.T) {
	edited := "name: x\nbackend: sbx\nnetwork: [a]\nbackends:\n  sbx:\n    packages: [git]\n  nono:\n    paths:\n      read: [/opt, /srv]\n"

	if hashFor(t, sectionedBody, "sbx") != hashFor(t, edited, "sbx") {
		t.Error("editing backends.nono changed the sbx hash")
	}
}

func TestFlippingTheDeclaredDefaultLeavesTheSbxHashAlone(t *testing.T) {
	flipped := "name: x\nbackend: nono\nnetwork: [a]\nbackends:\n  sbx:\n    packages: [git]\n  nono:\n    paths:\n      read: [/opt]\n"

	if hashFor(t, sectionedBody, "sbx") != hashFor(t, flipped, "sbx") {
		t.Error("changing backend: from sbx to nono changed the sbx hash")
	}
}

func TestTheSameFileHashesDifferentlyPerBackend(t *testing.T) {
	if hashFor(t, sectionedBody, "sbx") == hashFor(t, sectionedBody, "nono") {
		t.Error("sbx and nono see different configs but hash the same")
	}
}
