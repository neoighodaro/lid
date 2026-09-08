package backend

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

type Backend interface {
	Name() string
	EnsureAvailable(h Host) error
	Validate(cfg *config.Config) ([]string, error)
	Render(cfg *config.Config) (string, error)
	Provision(h Host, cfg *config.Config, opts Options) (Provisioned, error)
	PrepareAgent(h Host, cfg *config.Config) error
	StartArgv(cfg *config.Config, profilePath string) ([]string, error)
	// ShellArgv is the command that opens a shell in the sandbox, or runs one
	// command in it when command is non-empty. It takes a Host because an
	// interactive session needs its init file materialized first, and only the
	// backend knows whether that lands on this machine or inside a container.
	ShellArgv(h Host, cfg *config.Config, profilePath string, command []string) ([]string, error)
	Status(h Host, cfg *config.Config, opts StatusOptions) (State, error)
	Stop(h Host, cfg *config.Config) error
	// Network changes the firewall of a sandbox that is already up. A backend
	// that can only decide policy when a session launches says so rather than
	// pretending the change landed.
	Network(h Host, cfg *config.Config, action NetworkAction, domains []string) error
}

// ErrNetworkAtLaunch marks a backend that fixes network policy when a session
// launches, so a rule cannot be added to one already running.
var ErrNetworkAtLaunch = errors.New("network policy is set when a session launches")

type NetworkAction string

const (
	NetworkAllow  NetworkAction = "allow"
	NetworkDeny   NetworkAction = "deny"
	NetworkRemove NetworkAction = "remove"
)

type Options struct {
	Rebuild bool
}

type StatusOptions struct {
	Stale bool
}

type State struct {
	Exists  bool
	Running bool
	Stale   bool
	Rows    [][2]string
}

type Provisioned struct {
	Path    string
	Ref     string
	Changed bool
	Stale   bool
	Notes   []string
}

var registry = map[string]Backend{
	config.BackendNono: nono{},
	config.BackendSbx:  sbx{},
}

func Get(name string) Backend {
	return registry[name]
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// jsonBody drops whatever a tool printed around its JSON. A CLI is free to
// announce a new release, or warn about something, on the stream the data comes
// down: the payload starts at the first line that opens an array or an object,
// and a decoder reading from there stops at the end of it.
func jsonBody(out string) string {
	body := strings.TrimSpace(out)
	for offset := 0; offset < len(body); {
		line := body[offset:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			return body[offset:]
		}
		offset += len(line) + 1
	}
	return body
}

func decodeJSON(body string, into any) error {
	return json.NewDecoder(strings.NewReader(body)).Decode(into)
}

// writeBeside replaces a file the user owns through a temporary one in the same
// directory, so a settings file lid only meant to add a key to is never left
// half-written. The file's own permissions are kept.
func writeBeside(path string, body []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return failure.New("cannot write next to %s", path).WithFix("%v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return failure.New("cannot write %s", path).WithFix("%v", err)
	}
	if err := tmp.Close(); err != nil {
		return failure.New("cannot write %s", path).WithFix("%v", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return failure.New("cannot write %s", path).WithFix("%v", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return failure.New("cannot write %s", path).WithFix("%v", err)
	}
	return nil
}

func parseList[T any](out, command, key string) ([]T, error) {
	body := jsonBody(out)
	if body == "" {
		return nil, nil
	}

	var list []T
	if err := decodeJSON(body, &list); err == nil {
		return list, nil
	}

	unreadable := failure.New("cannot read what '%s' printed: %s", command, firstLine(body)).
		WithFix("this is a bug in lid, or a version of the tool that has changed its output")

	var wrapped map[string]json.RawMessage
	if err := decodeJSON(body, &wrapped); err != nil {
		return nil, unreadable
	}

	if raw, ok := wrapped[key]; ok {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, unreadable
		}
		return list, nil
	}

	keys := make([]string, 0, len(wrapped))
	for name := range wrapped {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	empty := false
	for _, name := range keys {
		var candidate []T
		if err := json.Unmarshal(wrapped[name], &candidate); err != nil {
			continue
		}
		if len(candidate) > 0 {
			return candidate, nil
		}
		empty = true
	}
	if empty {
		return nil, nil
	}
	return nil, unreadable
}
