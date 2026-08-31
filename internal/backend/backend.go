package backend

import (
	"sort"

	"github.com/neoighodaro/lid/internal/config"
)

type Backend interface {
	Name() string
	EnsureAvailable() error
	Validate(cfg *config.Config) ([]string, error)
	Render(cfg *config.Config) (string, error)
}

var registry = map[string]Backend{
	config.BackendNono: nono{},
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
