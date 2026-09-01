package backend

import (
	"encoding/json"
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
}

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

func parseList[T any](out, command, key string) ([]T, error) {
	body := strings.TrimSpace(out)
	if body == "" {
		return nil, nil
	}

	var list []T
	if err := json.Unmarshal([]byte(body), &list); err == nil {
		return list, nil
	}

	unreadable := failure.New("cannot read what '%s' printed: %s", command, firstLine(body)).
		WithFix("this is a bug in lid, or a version of the tool that has changed its output")

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &wrapped); err != nil {
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
