package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

const DefaultAgent = "claude-code"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var candidates = []string{
	"lid.yaml",
	filepath.Join(".config", "lid", "lid.yaml"),
}

type Config struct {
	Name  string `yaml:"name"`
	Agent string `yaml:"agent"`

	Path string `yaml:"-"`
}

func Discover(projectDir string) (string, error) {
	for _, rel := range candidates {
		path := filepath.Join(projectDir, rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no lid.yaml found in %s. Run 'lid init' to create one", projectDir)
}

func Resolve(projectDir, override string) (string, error) {
	if override == "" {
		return Discover(projectDir)
	}
	info, err := os.Stat(override)
	if err != nil {
		return "", fmt.Errorf("config file %s: %w", override, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("config file %s is a directory", override)
	}
	return override, nil
}

func Load(projectDir, override string) (*Config, error) {
	path, err := Resolve(projectDir, override)
	if err != nil {
		return nil, err
	}
	return Parse(path)
}

func Parse(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := &Config{Path: path}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Agent == "" {
		cfg.Agent = DefaultAgent
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("name %q must be lowercase alphanumeric with dashes", c.Name)
	}
	return nil
}
