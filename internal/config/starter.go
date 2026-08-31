package config

import (
	"embed"
	"strings"
	"text/template"

	"github.com/neoighodaro/lid/internal/failure"
)

//go:embed starter-*.yaml
var starterFS embed.FS
var starterTmpl = template.Must(template.ParseFS(starterFS, "starter-*.yaml"))

type RuntimePin struct {
	Name    string
	Version string
}

type StarterOptions struct {
	Name     string
	Backend  string
	Runtimes []RuntimePin
}

func Starter(opts StarterOptions) (string, error) {
	if err := ValidateName(opts.Name); err != nil {
		return "", err
	}
	if opts.Backend == "" {
		opts.Backend = DefaultBackend
	}
	if err := ValidateBackend(opts.Backend); err != nil {
		return "", err
	}

	var b strings.Builder
	if err := starterTmpl.ExecuteTemplate(&b, "starter-"+opts.Backend+".yaml", opts); err != nil {
		return "", failure.New("cannot render the starter config: %v", err).
			WithFix("this is a bug in lid")
	}
	return b.String(), nil
}

func ValidateBackend(name string) error {
	if !contains(Backends, name) {
		return failure.New("lid has no backend named %q", name).
			WithFix("use one of %s", strings.Join(Backends, ", "))
	}
	return nil
}

func ParseRuntimePins(entries []string) ([]RuntimePin, error) {
	versions := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, version, ok := strings.Cut(strings.TrimSpace(entry), "@")
		if !ok || version == "" {
			return nil, failure.New("runtime %q has no version", entry).
				WithFix("pin one, like '%s@1.2'", strings.TrimSuffix(entry, "@"))
		}
		if !contains(Runtimes, name) {
			return nil, failure.New("unknown runtime %q", name).
				WithFix("use one of %s", strings.Join(Runtimes, ", "))
		}
		if seen, ok := versions[name]; ok && seen != version {
			return nil, failure.New("runtime %s is pinned to both %s and %s", name, seen, version).
				WithFix("pass it once")
		}
		versions[name] = version
	}

	pins := make([]RuntimePin, 0, len(versions))
	for _, name := range Runtimes {
		if version, ok := versions[name]; ok {
			pins = append(pins, RuntimePin{Name: name, Version: version})
		}
	}
	return pins, nil
}
