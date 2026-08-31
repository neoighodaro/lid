package config

import (
	"embed"
	"strings"
	"text/template"

	"github.com/neoighodaro/lid/internal/failure"
)

//go:embed starter.yaml
var starterFS embed.FS
var starterTmpl = template.Must(template.ParseFS(starterFS, "starter.yaml"))

func Starter(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := starterTmpl.Execute(&b, struct{ Name string }{Name: name}); err != nil {
		return "", failure.New("cannot render the starter config: %v", err).
			WithFix("this is a bug in lid")
	}
	return b.String(), nil
}
