package config

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
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
		return "", fmt.Errorf("render starter config: %w", err)
	}
	return b.String(), nil
}
