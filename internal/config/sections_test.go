package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func merged(t *testing.T, body, backend string) map[string]any {
	t.Helper()
	doc, _, err := effectiveDocument([]byte(body), backend)
	if err != nil {
		t.Fatalf("effectiveDocument(%s): %v\nconfig:\n%s", backend, err, body)
	}
	var out map[string]any
	if err := doc.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mergedErr(t *testing.T, body, backend string) error {
	t.Helper()
	_, _, err := effectiveDocument([]byte(body), backend)
	if err == nil {
		t.Fatalf("effectiveDocument(%s): want error, got nil\nconfig:\n%s", backend, body)
	}
	return err
}

func list(items ...any) []any { return items }

func assertKey(t *testing.T, m map[string]any, key string, want any) {
	t.Helper()
	if got := m[key]; !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", key, got, want)
	}
}

func TestSectionListsAppendToTheBase(t *testing.T) {
	body := "name: x\nnetwork: [a, b]\nbackends:\n  sbx:\n    network: [c]\n"

	assertKey(t, merged(t, body, "sbx"), "network", list("a", "b", "c"))
	assertKey(t, merged(t, body, "nono"), "network", list("a", "b"))
}

func TestSectionMapsMergeKeyByKey(t *testing.T) {
	body := "name: x\nclaude:\n  theme: dark\n  plugins:\n    install: [x]\nbackends:\n  sbx:\n    claude:\n      plugins:\n        install: [y]\n"

	claude := merged(t, body, "sbx")["claude"].(map[string]any)
	assertKey(t, claude, "theme", "dark")
	assertKey(t, claude["plugins"].(map[string]any), "install", list("x", "y"))
}

func TestSectionScalarsReplace(t *testing.T) {
	body := "name: x\nagent: claude-code\nbackends:\n  sbx:\n    agent: codex\n"

	assertKey(t, merged(t, body, "sbx"), "agent", "codex")
}

func TestSectionListsDropDuplicates(t *testing.T) {
	body := "name: x\nnetwork: [a, b]\nbackends:\n  sbx:\n    network: [b, c]\n"

	assertKey(t, merged(t, body, "sbx"), "network", list("a", "b", "c"))
}

func TestConcatenatedListsDedupFirstWins(t *testing.T) {
	body := "name: x\nnetwork: [a, a]\nports: [1, 1]\nbackends:\n  sbx:\n    network: [c, c, a]\n"

	got := merged(t, body, "sbx")
	assertKey(t, got, "network", list("a", "c"))
	assertKey(t, got, "ports", list(1, 1))
}

func TestANullSectionValueChangesNothing(t *testing.T) {
	body := "name: x\nnetwork: [a, b]\nbackends:\n  sbx:\n    network:\n      # - c\n"

	assertKey(t, merged(t, body, "sbx"), "network", list("a", "b"))
}

func TestABareSectionChangesNothing(t *testing.T) {
	for _, body := range []string{
		"name: x\nnetwork: [a]\nbackends:\n  sbx:\n",
		"name: x\nnetwork: [a]\nbackends:\n",
	} {
		got := merged(t, body, "sbx")
		assertKey(t, got, "network", list("a"))
		if _, ok := got["backends"]; ok {
			t.Errorf("backends leaked into the merged config: %#v", got)
		}
	}
}

func TestASectionFillsAKeyTheBaseLacks(t *testing.T) {
	assertKey(t, merged(t, "name: x\nbackends:\n  sbx:\n    packages: [jq]\n", "sbx"), "packages", list("jq"))
	assertKey(t, merged(t, "name: x\nnetwork:\nbackends:\n  sbx:\n    network: [c]\n", "sbx"), "network", list("c"))
}

func TestASectionOfTheWrongKindIsAnError(t *testing.T) {
	body := "name: x\nnetwork: [a]\nbackends:\n  sbx:\n    network:\n      a: b\n"

	err := mergedErr(t, body, "sbx")
	if !strings.Contains(err.Error(), "backends.sbx.network is a map, but the shared config has a list") {
		t.Errorf("err = %v, want the path and both kinds", err)
	}
	if !strings.Contains(err.Error(), "line 6") {
		t.Errorf("err = %v, want the section's line", err)
	}
}

func TestUnknownSectionNamesAreRejected(t *testing.T) {
	err := mergedErr(t, "name: x\nbackends:\n  sbox: {}\n", "sbx")

	if !strings.Contains(err.Error(), `no backend named "sbox"`) || !strings.Contains(err.Error(), "nono, sbx") {
		t.Errorf("err = %v, want the unknown name and the known ones", err)
	}
}

func TestIdentityKeysAreRejectedInASection(t *testing.T) {
	for _, key := range []string{"name", "backend"} {
		err := mergedErr(t, "name: x\nbackends:\n  sbx:\n    "+key+": y\n", "nono")
		if !strings.Contains(err.Error(), "backends.sbx."+key) {
			t.Errorf("err = %v, want it to name backends.sbx.%s", err, key)
		}
	}
}

func TestASectionThatIsNotAMapIsRejected(t *testing.T) {
	err := mergedErr(t, "name: x\nbackends:\n  sbx: [a]\n", "sbx")

	if !strings.Contains(err.Error(), "backends.sbx") {
		t.Errorf("err = %v, want it to name the section", err)
	}
}

func TestMergeKeysAreRejectedInASection(t *testing.T) {
	body := "name: x\nenv: &e\n  A: \"1\"\nbackends:\n  sbx:\n    env:\n      <<: *e\n"

	err := mergedErr(t, body, "sbx")
	if !strings.Contains(err.Error(), "backends.sbx") || !strings.Contains(err.Error(), "<<") {
		t.Errorf("err = %v, want the section and the merge key named", err)
	}
}

func TestAliasesResolveBeforeMerging(t *testing.T) {
	body := "name: x\nbackends:\n  sbx:\n    network: &n [c]\nnetwork: *n\n"

	assertKey(t, merged(t, body, "sbx"), "network", list("c"))
	assertKey(t, merged(t, body, "nono"), "network", list("c"))

	body = "name: x\nnetwork: &n [a, b]\nbackends:\n  nono:\n    network: *n\n"
	assertKey(t, merged(t, body, "nono"), "network", list("a", "b"))
}

func TestBaseMergeKeysStillWork(t *testing.T) {
	body := "name: x\nenv: &e\n  A: \"1\"\nclaude:\n  settings:\n    <<: *e\n    B: \"2\"\nbackends:\n  sbx:\n    claude:\n      settings:\n        C: \"3\"\n"

	settings := merged(t, body, "sbx")["claude"].(map[string]any)["settings"].(map[string]any)
	if !reflect.DeepEqual(settings, map[string]any{"A": "1", "B": "2", "C": "3"}) {
		t.Errorf("settings = %#v, want A, B and C", settings)
	}
}

func TestMergingOneSectionLeavesTheOtherUntouched(t *testing.T) {
	body := "name: x\npackages: [git]\nbackends:\n  sbx:\n    packages: [jq]\n"

	assertKey(t, merged(t, body, "sbx"), "packages", list("git", "jq"))
	assertKey(t, merged(t, body, "nono"), "packages", list("git"))
}

func TestNoBackendsKeyReturnsTheDocumentAsIs(t *testing.T) {
	body := "name: x\nnetwork: [a, a]\nruntimes:\n  python: 3.10\n"

	doc, hasSections, err := effectiveDocument([]byte(body), "sbx")
	if err != nil {
		t.Fatal(err)
	}
	if hasSections {
		t.Error("hasSections = true for a file with no backends key")
	}
	var got, want map[string]any
	if err := doc.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestAnEmptyFileHasNoDocument(t *testing.T) {
	doc, hasSections, err := effectiveDocument(nil, "sbx")
	if doc != nil || hasSections || err != nil {
		t.Errorf("effectiveDocument(nil) = %v, %v, %v; want nil, false, nil", doc, hasSections, err)
	}
}
