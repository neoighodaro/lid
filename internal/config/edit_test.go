package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lid.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

// The config is a file the user hand-wrote and keeps editing. Losing the
// comments in it to a one-line network change would be a bad trade.
func TestAddingADomainKeepsTheCommentsTheUserWrote(t *testing.T) {
	path := writeConfig(t, "# the project this sandbox is for\nname: api\n\n# hosts the agent may reach\nnetwork:\n  - github.com\n")

	if err := AddNetworkDomains(path, []string{"pypi.org"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	body := read(t, path)
	for _, want := range []string{"# the project this sandbox is for", "# hosts the agent may reach", "pypi.org", "github.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("config lost %q:\n%s", want, body)
		}
	}
}

func TestAddingADomainAlreadyListedChangesNothing(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork:\n  - github.com\n")

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	if got := strings.Count(read(t, path), "github.com"); got != 1 {
		t.Errorf("github.com listed %d times, want 1:\n%s", got, read(t, path))
	}
}

func TestAddingADomainToAConfigWithNoNetworkKeyCreatesOne(t *testing.T) {
	path := writeConfig(t, "name: api\n")

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	cfg, err := Load(filepath.Dir(path), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Network) != 1 || cfg.Network[0] != "github.com" {
		t.Errorf("network = %v, want [github.com]", cfg.Network)
	}
}

// An empty 'network:' parses as null, not as a list. Writing into it as if it
// were a sequence is what turns the key into one.
func TestAddingADomainFillsAnEmptyNetworkKey(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork:\n")

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	cfg, err := Load(filepath.Dir(path), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Network) != 1 || cfg.Network[0] != "github.com" {
		t.Errorf("network = %v, want [github.com]", cfg.Network)
	}
}

func TestTheRewrittenNetworkListIsOneHostPerLine(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork: [github.com]\n")

	if err := AddNetworkDomains(path, []string{"pypi.org"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	body := read(t, path)
	if !strings.Contains(body, "- github.com") || !strings.Contains(body, "- pypi.org") {
		t.Errorf("network did not render as a block list:\n%s", body)
	}
}

func TestRemovingADomainLeavesTheRestAlone(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork:\n  - github.com\n  - pypi.org\n")

	if err := RemoveNetworkDomains(path, []string{"github.com"}); err != nil {
		t.Fatalf("RemoveNetworkDomains: %v", err)
	}

	cfg, err := Load(filepath.Dir(path), path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Network) != 1 || cfg.Network[0] != "pypi.org" {
		t.Errorf("network = %v, want [pypi.org]", cfg.Network)
	}
}

// The key carries the comments explaining what it is for. Emptying the list is
// not a reason to throw those away.
func TestRemovingTheLastDomainKeepsTheKeyAndItsComments(t *testing.T) {
	path := writeConfig(t, "name: api\n\n# hosts the agent may reach\nnetwork:\n  - github.com\n")

	if err := RemoveNetworkDomains(path, []string{"github.com"}); err != nil {
		t.Fatalf("RemoveNetworkDomains: %v", err)
	}

	body := read(t, path)
	if !strings.Contains(body, "network:") || !strings.Contains(body, "# hosts the agent may reach") {
		t.Errorf("config lost the network key or its comment:\n%s", body)
	}
}

func TestRemovingADomainThatIsNotListedChangesNothing(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork:\n  - github.com\n")
	before := read(t, path)

	if err := RemoveNetworkDomains(path, []string{"pypi.org"}); err != nil {
		t.Fatalf("RemoveNetworkDomains: %v", err)
	}

	if after := read(t, path); !strings.Contains(after, "github.com") {
		t.Errorf("config went from %q to %q", before, after)
	}
}

func TestEditingTheConfigKeepsItsPermissions(t *testing.T) {
	path := writeConfig(t, "name: api\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode = %v, want 0640", got)
	}
}

// Rewriting through a temp file is only safe if nothing is left behind when it
// works, or the directory fills with half-written configs.
func TestEditingTheConfigLeavesNoTemporaryFileBehind(t *testing.T) {
	path := writeConfig(t, "name: api\n")

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "lid.yaml" {
		t.Errorf("directory holds %v, want just lid.yaml", entries)
	}
}

func TestEditingAConfigWhoseNetworkIsNotAListSaysSo(t *testing.T) {
	path := writeConfig(t, "name: api\nnetwork: github.com\n")

	err := AddNetworkDomains(path, []string{"pypi.org"}, "sbx")
	if err == nil {
		t.Fatal("AddNetworkDomains accepted a scalar network key")
	}
	if !strings.Contains(err.Error(), "not a list") {
		t.Errorf("error = %q, want it to say the key is not a list", err)
	}
}

func TestEditingAConfigThatIsNotAMappingSaysSo(t *testing.T) {
	path := writeConfig(t, "- github.com\n")

	err := AddNetworkDomains(path, []string{"pypi.org"}, "sbx")
	if err == nil {
		t.Fatal("AddNetworkDomains accepted a config that is a list")
	}
	if !strings.Contains(err.Error(), "not a set of keys") {
		t.Errorf("error = %q, want it to say the config is not a mapping", err)
	}
}

func TestEditingABrokenConfigLeavesItUntouched(t *testing.T) {
	path := writeConfig(t, "name: api\n  bad indent: [\n")

	if err := AddNetworkDomains(path, []string{"github.com"}, "sbx"); err == nil {
		t.Fatal("AddNetworkDomains accepted invalid YAML")
	}

	if body := read(t, path); !strings.Contains(body, "bad indent") {
		t.Errorf("config was rewritten despite the parse failing:\n%s", body)
	}
}

func TestAddNetworkDomainsWritesThroughASymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "shared.yaml")
	if err := os.WriteFile(real, []byte("name: api\nnetwork:\n  - example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lid.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AddNetworkDomains(link, []string{"pypi.org"}, "sbx"); err != nil {
		t.Fatalf("AddNetworkDomains: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced by a file of its own, so the shared config no longer feeds this project")
	}
	body, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "pypi.org") {
		t.Errorf("the host never reached the file the link points at:\n%s", body)
	}
}

func TestAddSkipsADomainTheActiveSectionAlreadyAllows(t *testing.T) {
	path := writeConfig(t, "name: x\nnetwork:\n  - b\nbackends:\n  sbx:\n    network:\n      - a\n")

	if err := AddNetworkDomains(path, []string{"a"}, "sbx"); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseFor(path, "nono")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Network, ",") != "b" {
		t.Errorf("shared network = %v, want a left in the sbx section only", cfg.Network)
	}
}

func TestAddStillWritesADomainOnlyTheOtherSectionHas(t *testing.T) {
	path := writeConfig(t, "name: x\nbackends:\n  nono:\n    network:\n      - a\n")

	if err := AddNetworkDomains(path, []string{"a"}, "sbx"); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseFor(path, "sbx")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Network, ",") != "a" {
		t.Errorf("sbx network = %v, want a added to the shared list", cfg.Network)
	}
}

func TestRemoveStripsTheDomainFromEverySection(t *testing.T) {
	path := writeConfig(t, "name: x\n# shared hosts\nnetwork:\n  - a\n  - b\nbackends:\n  sbx:\n    network:\n      - a\n  nono:\n    network: [a, c]\n")

	if err := RemoveNetworkDomains(path, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"sbx", "nono"} {
		cfg, err := ParseFor(path, backend)
		if err != nil {
			t.Fatal(err)
		}
		for _, domain := range cfg.Network {
			if domain == "a" {
				t.Errorf("%s still allows a:\n%s", backend, data)
			}
		}
	}
	if !strings.Contains(string(data), "# shared hosts") {
		t.Errorf("comment lost:\n%s", data)
	}
}
