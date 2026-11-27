package panes

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func isolate(t *testing.T) {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
}

func TestTakePaneFileRefusesUnsafeFiles(t *testing.T) {
	isolate(t)
	argv := []string{"sbx", "run", "--name", "api"}

	path, err := WriteArgv(argv)
	if err != nil {
		t.Fatalf("WriteArgv: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pane file mode: %v %v", info, err)
	}
	got, err := TakeArgv(path)
	if err != nil || !reflect.DeepEqual(got, argv) {
		t.Fatalf("TakeArgv = %v, %v", got, err)
	}
	if _, err := TakeArgv(path); err == nil {
		t.Error("a pane file was taken twice")
	}

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte(`["rm"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"symlink":     link,
		"outside dir": "/etc/passwd",
		"dotdot":      filepath.Join(dir, "..", "x"),
		"relative":    "x",
	} {
		if _, err := TakeArgv(p); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	path, err = WriteArgv(argv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeArgv(path); err == nil {
		t.Error("a 0755 pane dir was trusted")
	}
	if _, err := WriteArgv(argv); err == nil {
		t.Error("wrote into a 0755 pane dir")
	}
}

func TestTakePaneFileRefusesFifosLinksAndLooseModes(t *testing.T) {
	isolate(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{"sh", "-c", "id"}

	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := TakeArgv(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was taken as a pane file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TakeArgv blocked on a FIFO")
	}

	linked, err := WriteArgv(argv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(linked, filepath.Join(dir, "second")); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeArgv(linked); err == nil {
		t.Error("a hard-linked pane file was taken")
	}

	loose, err := WriteArgv(argv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeArgv(loose); err == nil {
		t.Error("a 0644 pane file was taken")
	}
}

func TestPaneFilesAreWrittenPrivateWhateverTheUmask(t *testing.T) {
	isolate(t)
	if _, err := Dir(); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o277)
	defer syscall.Umask(old)
	path, err := WriteArgv([]string{"ls"})
	syscall.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TakeArgv(path); err != nil {
		t.Errorf("lid refused its own pane file: %v", err)
	}
}

func TestPaneDirRefusesALinkedCacheDir(t *testing.T) {
	isolate(t)
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err == nil {
		t.Error("trusted a pane dir under a linked cache dir")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "panes")); err == nil {
		t.Error("created the pane dir through the link")
	}
}

func TestVerifyRefusesAChangedFile(t *testing.T) {
	isolate(t)
	body := []byte(`{"extends": "claude-code"}`)
	path, err := Create("profile-1-aa.jsonc", body)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, body); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"extends": "nothing"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, body); err == nil {
		t.Error("a rewritten profile passed")
	}
	other := filepath.Join(filepath.Dir(path), "other")
	if err := os.WriteFile(other, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, body); err == nil {
		t.Error("a profile swapped for a link passed")
	}
}
