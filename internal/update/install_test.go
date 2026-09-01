package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestALidThatAPackageManagerOwnsIsLeftToThatPackageManager(t *testing.T) {
	managed := map[string]string{
		"/opt/homebrew/bin/lid":                  "Homebrew",
		"/opt/homebrew/Cellar/lid/1.2.3/bin/lid": "Homebrew",
		"/usr/local/Cellar/lid/1.2.3/bin/lid":    "Homebrew",
		"/home/linuxbrew/.linuxbrew/bin/lid":     "Homebrew",
		"/nix/store/abc123-lid-1.2.3/bin/lid":    "Nix",
		"/Users/neo/go/bin/lid":                  "go install",
	}
	for path, want := range managed {
		manager, ok := ManagedBy(path)
		if !ok {
			t.Errorf("ManagedBy(%q) found nothing, so lid would overwrite a file %s owns", path, want)
			continue
		}
		if manager.Name != want {
			t.Errorf("ManagedBy(%q) = %q, want %q", path, manager.Name, want)
		}
		if manager.Upgrade == "" {
			t.Errorf("ManagedBy(%q) names no command to upgrade with", path)
		}
	}

	for _, path := range []string{"/usr/local/bin/lid", "/Users/neo/.local/bin/lid"} {
		if manager, ok := ManagedBy(path); ok {
			t.Errorf("ManagedBy(%q) = %q, but lid installed itself there and may replace it", path, manager.Name)
		}
	}
}

func TestReplacingTheBinaryLeavesSomethingRunnableBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lid")
	if err := os.WriteFile(target, []byte("the old lid"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Replace(target, []byte("the new lid")); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the new lid" {
		t.Errorf("binary = %q, want the downloaded one", body)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want an executable file: the update would leave lid unrunnable", info.Mode())
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 {
		t.Errorf("dir holds %d files, want only lid: a half-written update was left behind", len(left))
	}
}

func TestAFailedReplacementDoesNotDestroyTheLidYouHave(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "lid")

	if err := Replace(target, []byte("the new lid")); err == nil {
		t.Fatal("Replace into a directory that does not exist reported success")
	}
	if _, err := os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Error("a failed update left files behind")
	}
}

func TestWritableSaysSoBeforeAnythingIsDownloaded(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lid")
	if err := os.WriteFile(target, []byte("lid"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Writable(target); err != nil {
		t.Errorf("Writable: %v", err)
	}
	if err := Writable(filepath.Join(dir, "missing")); err == nil {
		t.Error("a binary that is not there was reported writable")
	}

	if err := Writable(os.Args[0]); err != nil {
		t.Errorf("the running binary was called unwritable (%v), but replacing it renames over it rather than opening it", err)
	}
}
