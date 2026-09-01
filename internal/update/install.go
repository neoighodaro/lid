package update

import (
	"os"
	"path/filepath"
	"strings"
)

type Manager struct {
	Name    string
	Upgrade string
}

func Target() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		return self, nil
	}
	return resolved, nil
}

func ManagedBy(path string) (Manager, bool) {
	slashed := filepath.ToSlash(path)
	switch {
	case strings.Contains(slashed, "/Cellar/"), strings.Contains(slashed, "/homebrew/"), strings.Contains(slashed, "/linuxbrew/"):
		return Manager{Name: "Homebrew", Upgrade: "brew upgrade lid"}, true
	case strings.HasPrefix(slashed, "/nix/store/"):
		return Manager{Name: "Nix", Upgrade: "nix profile upgrade lid"}, true
	case strings.Contains(slashed, "/go/bin/"):
		return Manager{Name: "go install", Upgrade: "go install github.com/neoighodaro/lid@latest"}, true
	}
	return Manager{}, false
}

func Writable(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	probe, err := os.CreateTemp(filepath.Dir(path), ".lid-update-*")
	if err != nil {
		return err
	}
	probe.Close()
	return os.Remove(probe.Name())
}

func Replace(target string, binary []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".lid-update-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm() | 0o111
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, target)
}
