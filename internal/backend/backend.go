package backend

import (
	"sort"

	"github.com/neoighodaro/lid/internal/config"
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
}

type Options struct {
	Rebuild bool
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
