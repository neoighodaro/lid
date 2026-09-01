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
	Provision(h Host, cfg *config.Config) (Provisioned, error)
	StartArgv(cfg *config.Config, profilePath string) ([]string, error)
}

type Provisioned struct {
	Path    string
	Ref     string
	Changed bool
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
