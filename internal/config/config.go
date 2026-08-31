package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/neoighodaro/lid/internal/envfile"
	"gopkg.in/yaml.v3"
)

const AgentClaudeCode = "claude-code"

const (
	DefaultAgent     = AgentClaudeCode
	DefaultBackend   = BackendSbx
	DefaultBaseImage = "docker/sandbox-templates:claude-code-minimal"
	MaxNameLen       = 63
)

const (
	BackendSbx  = "sbx"
	BackendNono = "nono"
)

var (
	VerifiedAgents = []string{
		"claude-code",
	}

	KnownAgents = []string{
		"claude-code",
		"codex",
		"copilot",
		"cursor",
		"docker-agent",
		"droid",
		"gemini",
		"kiro",
		"opencode",
		"shell",
	}

	Backends = []string{
		BackendNono,
		BackendSbx,
	}

	Runtimes = []string{
		"bun",
		"go",
		"node",
		"php",
		"pnpm",
		"python",
		"rust",
	}
)

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)
	portRe     = regexp.MustCompile(`^(?:(\d+):)?(\d+)(?:/(tcp|tcp4|tcp6|udp|udp4|udp6))?$`)

	unknownFieldRe = regexp.MustCompile(`^line (\d+): field (\S+) not found in type \S+$`)
	inTypeRe       = regexp.MustCompile(` in type \S+`)
)

var candidates = []string{
	"lid.yaml",
	filepath.Join(".config", "lid", "lid.yaml"),
}

// Scalar is a string that also accepts unquoted YAML numbers and booleans,
// keeping the literal source text: an unquoted 3.10 stays "3.10" instead of
// round-tripping through float64 and becoming "3.1".
type Scalar string

func (s *Scalar) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: expected a single value", node.Line)
	}
	*s = Scalar(node.Value)
	return nil
}

type Scripts struct {
	OnCreate []string `yaml:"on_create"`
	OnStart  []string `yaml:"on_start"`
}

type Shell struct {
	Directory string            `yaml:"directory"`
	Aliases   map[string]string `yaml:"aliases"`
}

type ClaudePlugins struct {
	Marketplaces []string `yaml:"marketplaces"`
	Install      []string `yaml:"install"`
}

type Claude struct {
	Theme    string         `yaml:"theme"`
	Plugins  ClaudePlugins  `yaml:"plugins"`
	Settings map[string]any `yaml:"settings"`
}

type PHP struct {
	Extensions []string `yaml:"extensions"`
}

type Zellij struct {
	DisplayName string `yaml:"display_name"`
}

type Handoff struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

// Bridge declares a host-service tunnel: a service published on the host at
// HostPort becomes reachable inside the sandbox as Name:Port, so the project's
// .env needs no sandbox-specific edits. Env names a variable to point at the
// bridge when the runtime mounts /etc/hosts read-only.
type Bridge struct {
	Name     string `yaml:"name"`
	Port     int    `yaml:"port"`
	HostPort int    `yaml:"host_port"`
	Env      string `yaml:"env"`
}

type Config struct {
	Name      string            `yaml:"name"`
	Agent     string            `yaml:"agent"`
	Backend   string            `yaml:"backend"`
	BaseImage string            `yaml:"base_image"`
	Template  string            `yaml:"template"`
	Workspace string            `yaml:"workspace"`
	Packages  []string          `yaml:"packages"`
	Runtimes  map[string]Scalar `yaml:"runtimes"`
	Ports     []Scalar          `yaml:"ports"`
	Env       map[string]Scalar `yaml:"env"`
	EnvFile   []string          `yaml:"env_file"`
	Network   []string          `yaml:"network"`
	Scripts   Scripts           `yaml:"scripts"`
	Shell     Shell             `yaml:"shell"`
	Claude    Claude            `yaml:"claude"`
	PHP       PHP               `yaml:"php"`
	Zellij    Zellij            `yaml:"zellij"`
	Memory    string            `yaml:"memory"`
	Handoff   Handoff           `yaml:"handoff"`
	Bridges   []Bridge          `yaml:"bridges"`

	Path       string `yaml:"-"`
	ProjectDir string `yaml:"-"`
}

// Discover returns the first config candidate present under projectDir.
func Discover(projectDir string) (string, error) {
	for _, rel := range candidates {
		path := filepath.Join(projectDir, rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no lid.yaml found in %s. Run 'lid init' to create one", projectDir)
}

// Resolve returns the config path to use: override when set, else discovery.
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

// Load resolves and parses the config for projectDir, which becomes the
// config's ProjectDir even when override points somewhere else entirely.
func Load(projectDir, override string) (*Config, error) {
	path, err := Resolve(projectDir, override)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(path)
	if err != nil {
		return nil, err
	}
	cfg.ProjectDir = projectDir
	return cfg, nil
}

// Parse reads the config at path, applies defaults and validates it. Unknown
// keys are rejected: a mistyped 'runtime' would otherwise silently discard
// every runtime. ProjectDir is inferred from the file's location; Load
// overrides it with the directory actually being worked in.
func Parse(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	cfg := &Config{Path: path, ProjectDir: projectDirFor(path)}

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %s", path, cleanYAMLError(err))
	}

	if cfg.Agent == "" {
		cfg.Agent = DefaultAgent
	}
	if cfg.Backend == "" {
		cfg.Backend = DefaultBackend
	}
	if cfg.BaseImage == "" && cfg.BuildsImage() {
		cfg.BaseImage = DefaultBaseImage
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func projectDirFor(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "lid" && filepath.Base(filepath.Dir(dir)) == ".config" {
		return filepath.Dir(filepath.Dir(dir))
	}
	return dir
}

// cleanYAMLError rewrites yaml.v3's messages so they describe the config file
// rather than the Go types behind it.
func cleanYAMLError(err error) string {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err.Error()
	}

	lines := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		if m := unknownFieldRe.FindStringSubmatch(msg); m != nil {
			lines = append(lines, fmt.Sprintf("line %s: unknown field %q", m[1], m[2]))
			continue
		}
		lines = append(lines, inTypeRe.ReplaceAllString(msg, ""))
	}
	return strings.Join(lines, "; ")
}

func ValidateName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > MaxNameLen {
		return fmt.Errorf("name %q is %d characters; the limit is %d", name, len(name), MaxNameLen)
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name %q must be lowercase alphanumeric with dashes, starting and ending with a letter or digit", name)
	}
	return nil
}

func (c *Config) Validate() error {
	if err := ValidateName(c.Name); err != nil {
		return err
	}

	if !contains(KnownAgents, c.Agent) {
		return fmt.Errorf("unknown agent %q: must be one of %s", c.Agent, strings.Join(KnownAgents, ", "))
	}

	if !contains(Backends, c.Backend) {
		return fmt.Errorf("unknown backend %q: must be one of %s", c.Backend, strings.Join(Backends, ", "))
	}

	if err := c.validateBackendExclusions(); err != nil {
		return err
	}

	if err := c.validateTemplateExclusions(); err != nil {
		return err
	}

	for name := range c.Runtimes {
		if !contains(Runtimes, name) {
			return fmt.Errorf("unknown runtime %q: must be one of %s", name, strings.Join(Runtimes, ", "))
		}
	}
	if _, hasPnpm := c.Runtimes["pnpm"]; hasPnpm {
		if _, hasNode := c.Runtimes["node"]; !hasNode {
			return errors.New("the pnpm runtime requires node: add node to runtimes")
		}
	}

	if c.Workspace != "" && !filepath.IsAbs(c.Workspace) {
		return fmt.Errorf("workspace %q must be an absolute path inside the sandbox", c.Workspace)
	}

	for _, port := range c.Ports {
		if err := validatePort(string(port)); err != nil {
			return err
		}
	}

	for _, domain := range c.Network {
		if domain == "" {
			return errors.New("network entries must not be empty")
		}
		if strings.Contains(domain, "://") {
			return fmt.Errorf("network entry %q must be a host pattern, not a URL", domain)
		}
	}

	if c.Handoff.URL != "" {
		u, err := url.Parse(c.Handoff.URL)
		if err != nil || u.Host == "" || u.Port() == "" {
			return fmt.Errorf("invalid handoff url %q: must include a host and port", c.Handoff.URL)
		}
	}

	for _, b := range c.Bridges {
		if err := b.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) BuildsImage() bool {
	return c.Backend != BackendNono
}

func (c *Config) validateBackendExclusions() error {
	if c.BuildsImage() {
		return nil
	}
	for _, conflict := range []struct {
		field string
		set   bool
		why   string
	}{
		{"base_image", c.BaseImage != "", "a nono session is a host process, not a container"},
		{"template", c.Template != "", "a nono session is a host process, not a container"},
		{"packages", len(c.Packages) > 0, "install them on your host yourself"},
		{"ports", len(c.Ports) > 0, "a nono session shares the host network, so ports need no publishing"},
	} {
		if conflict.set {
			return fmt.Errorf("the %s backend cannot use %s: %s", c.Backend, conflict.field, conflict.why)
		}
	}
	return nil
}

func (c *Config) validateTemplateExclusions() error {
	if c.Template == "" {
		return nil
	}
	for _, conflict := range []struct {
		field string
		set   bool
		why   string
	}{
		{"packages", len(c.Packages) > 0, "install them in your Dockerfile"},
		{"runtimes", len(c.Runtimes) > 0, "install them in your Dockerfile"},
		{"bridges", len(c.Bridges) > 0, "a custom template controls its own socat setup and session hooks"},
	} {
		if conflict.set {
			return fmt.Errorf("cannot set both template and %s: %s", conflict.field, conflict.why)
		}
	}
	return nil
}

func (b Bridge) validate() error {
	if b.Name == "" {
		return errors.New("bridge is missing name, the hostname the app connects to")
	}
	if !hostnameRe.MatchString(b.Name) {
		return fmt.Errorf("invalid bridge name %q: must be a valid hostname", b.Name)
	}
	if !validPortNumber(b.Port) {
		return fmt.Errorf("invalid bridge %q port %d: must be between 1 and 65535", b.Name, b.Port)
	}
	if !validPortNumber(b.HostPort) {
		return fmt.Errorf("invalid bridge %q host_port %d: must be between 1 and 65535", b.Name, b.HostPort)
	}
	return nil
}

func validatePort(port string) error {
	m := portRe.FindStringSubmatch(port)
	if m == nil {
		return fmt.Errorf("invalid port %q: use PORT, HOST:PORT, or HOST:PORT/PROTOCOL", port)
	}
	for _, field := range []string{m[1], m[2]} {
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || !validPortNumber(n) {
			return fmt.Errorf("invalid port %q: must be between 1 and 65535", port)
		}
	}
	return nil
}

func validPortNumber(n int) bool { return n >= 1 && n <= 65535 }

func (c Claude) IsSet() bool {
	return c.Theme != "" ||
		len(c.Plugins.Marketplaces) > 0 ||
		len(c.Plugins.Install) > 0 ||
		len(c.Settings) > 0
}

func IsClaudeAgent(name string) bool {
	return name == AgentClaudeCode
}

func AgentVerified(name string) bool {
	return contains(VerifiedAgents, name)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func (c *Config) KitDir() string {
	return filepath.Join(c.ProjectDir, ".config", "lid", ".kit")
}

func (c *Config) SandboxImageName() string {
	return c.Name + "-sandbox"
}

func (c *Config) FileHash() (string, error) {
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return "", fmt.Errorf("cannot hash %s: %w", c.Path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ResolvedEnv merges env_file entries in order, then inline env on top.
// It reads from disk, so it is deliberately not part of parsing: a missing
// .env should fail the command that needs it, not every command.
func (c *Config) ResolvedEnv() (map[string]string, error) {
	merged := make(map[string]string)

	for _, entry := range c.EnvFile {
		path := entry
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.ProjectDir, path)
		}
		parsed, err := envfile.Parse(path)
		if err != nil {
			return nil, err
		}
		for k, v := range parsed {
			merged[k] = v
		}
	}

	for k, v := range c.Env {
		merged[k] = string(v)
	}
	return merged, nil
}

func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r < 128:
			b.WriteByte('-')
		}
	}

	name := b.String()
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")

	if len(name) > MaxNameLen {
		name = name[:MaxNameLen]
	}
	return strings.Trim(name, "-")
}
