package template

import (
	"embed"
	"sort"
	"strings"
	"text/template"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/runtime"
)

//go:embed Dockerfile.tmpl
var templateFS embed.FS
var dockerfileTmpl = template.Must(template.ParseFS(templateFS, "Dockerfile.tmpl"))

type data struct {
	BaseImage   string
	Packages    []string
	RootBlocks  []string
	AgentBlocks []string
	EnvBlocks   []string
	Claude      bool
	Plugins     config.ClaudePlugins
}

func Render(cfg *config.Config) (string, error) {
	if !cfg.BuildsImage() {
		return "", failure.New("the %s backend builds no image, so there is no Dockerfile to render", cfg.Backend).
			At(cfg.Ref()).
			WithFix("run 'lid start' instead")
	}
	if cfg.Template != "" {
		return "", failure.New("template %q is yours to build", cfg.Template).
			At(cfg.Ref()).
			WithFix("lid only generates a Dockerfile when no template is set")
	}

	d := data{
		BaseImage: cfg.BaseImage,
		Packages:  cfg.Packages,
		Claude:    config.IsClaudeAgent(cfg.Agent),
	}
	if d.Claude {
		d.Plugins = cfg.Claude.Plugins
	}

	opts := runtime.Options{PHPExtensions: cfg.PHP.Extensions}
	for _, name := range sortedRuntimes(cfg) {
		version := string(cfg.Runtimes[name])
		r := runtime.Get(name, opts)
		if r == nil {
			return "", failure.New("unknown runtime %q", name).
				At(cfg.Ref()).
				WithFix("use one of %s", strings.Join(config.Runtimes, ", "))
		}
		if err := r.Validate(version); err != nil {
			return "", failure.At(err, cfg.Ref())
		}
		for _, b := range []struct {
			block string
			into  *[]string
		}{
			{r.RootBlock(version), &d.RootBlocks},
			{r.AgentBlock(version), &d.AgentBlocks},
			{r.EnvBlock(version), &d.EnvBlocks},
		} {
			if b.block != "" {
				*b.into = append(*b.into, b.block)
			}
		}
	}

	var out strings.Builder
	if err := dockerfileTmpl.Execute(&out, d); err != nil {
		return "", failure.New("cannot render the Dockerfile: %v", err).
			WithFix("this is a bug in lid, not a problem with your config")
	}
	return strings.TrimRight(out.String(), "\n") + "\n", nil
}

func sortedRuntimes(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Runtimes))
	for name := range cfg.Runtimes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
