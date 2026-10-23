package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/neoighodaro/lid/internal/failure"
)

// Selection is the backend a project was started on by name, remembered
// until 'lid stop' so follow-up commands need no flag. It belongs to the one
// config file it was made with.
type Selection struct {
	Backend string `json:"backend"`
	Config  string `json:"config"`
}

func SelectionPath(projectDir string) string {
	return filepath.Join(kitDirFor(projectDir), "backend.json")
}

// ReadSelection returns the project's remembered backend, or a zero
// Selection when there is none. A record that cannot be trusted is an error
// rather than a silent fall-back, which would send 'lid stop' to the wrong
// backend.
func ReadSelection(projectDir string) (Selection, error) {
	path := SelectionPath(projectDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Selection{}, nil
	}
	unusable := func(what string, args ...any) error {
		return failure.New(what, args...).At(path).
			WithFix("delete it, then run 'lid start <backend>' again")
	}
	if err != nil {
		return Selection{}, unusable("cannot read the remembered backend: %v", err)
	}
	var sel Selection
	if err := json.Unmarshal(data, &sel); err != nil {
		return Selection{}, unusable("the remembered backend is not valid JSON")
	}
	if !contains(Backends, sel.Backend) {
		return Selection{}, unusable("the remembered backend %q is not one lid has (%s)", sel.Backend, strings.Join(Backends, ", "))
	}
	return sel, nil
}

// AppliesTo reports whether s was made with the config at configPath.
func (s Selection) AppliesTo(configPath string) bool {
	return s.Backend != "" && s.Config != "" && physicalPath(s.Config) == physicalPath(configPath)
}

// LoadSelected is LoadFor with the project's remembered backend standing in
// when no backend is given.
func LoadSelected(projectDir, override, backend string) (*Config, error) {
	if backend != "" {
		if err := ValidateBackend(backend); err != nil {
			return nil, err
		}
	}
	path, err := Resolve(projectDir, override)
	if err != nil {
		return nil, err
	}
	sel, err := ReadSelection(projectDir)
	if err != nil {
		return nil, err
	}
	selected := ""
	if sel.AppliesTo(path) {
		selected = sel.Backend
	}

	use := backend
	if use == "" {
		use = selected
	}
	cfg, err := LoadFor(projectDir, override, use)
	if err != nil {
		return nil, err
	}
	if backend == "" && selected != "" {
		cfg.BackendSource = SourceSelection
	}
	cfg.Selected = selected
	return cfg, nil
}

// Remember records c's backend as the project's selection.
func (c *Config) Remember() error {
	path := SelectionPath(c.ProjectDir)
	abs, err := filepath.Abs(c.Path)
	if err != nil {
		abs = c.Path
	}
	body, err := json.MarshalIndent(Selection{Backend: c.Backend, Config: abs}, "", "  ")
	if err != nil {
		return err
	}
	cannot := func(err error) error {
		return failure.New("cannot remember the backend").At(path).WithFix("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cannot(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".backend.json.*")
	if err != nil {
		return cannot(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		tmp.Close()
		return cannot(err)
	}
	if err := tmp.Close(); err != nil {
		return cannot(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return cannot(err)
	}
	return nil
}

// Forget clears the project's selection.
func (c *Config) Forget() error {
	return ForgetSelection(c.ProjectDir)
}

// ForgetSelection clears projectDir's selection without needing its config,
// so 'lid stop' can still clear it when the config no longer loads.
func ForgetSelection(projectDir string) error {
	path := SelectionPath(projectDir)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure.New("cannot forget the remembered backend").At(path).WithFix("%v", err)
	}
	return nil
}

func kitDirFor(projectDir string) string {
	return filepath.Join(projectDir, ".config", "lid", ".kit")
}

func physicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
