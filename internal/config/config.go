package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/neoighodaro/lid/internal/failure"
	"gopkg.in/yaml.v3"
)

const AgentClaudeCode = "claude-code"

const (
	DefaultAgent       = AgentClaudeCode
	DefaultBackend     = BackendSbx
	DefaultBaseImage   = "docker/sandbox-templates:claude-code-minimal"
	DefaultHandoffPort = 8787
	MaxNameLen         = 63
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

	NetworkProfiles = []string{
		"claude-code",
		"codex",
		"developer",
		"enterprise",
		"minimal",
		"opencode",
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
	memoryRe   = regexp.MustCompile(`^(?i)([1-9][0-9]*)([mg])$`)

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
	Marketplaces    []string `yaml:"marketplaces"`
	Install         []string `yaml:"install"`
	InstallOptional []string `yaml:"install_optional"`
}

type Claude struct {
	Theme    string         `yaml:"theme"`
	Plugins  ClaudePlugins  `yaml:"plugins"`
	Settings map[string]any `yaml:"settings"`
}

type PHP struct {
	Extensions []string `yaml:"extensions"`
}

// Zellij holds the old place for the tab name. Deprecated: use the top-level
// display_name, which every terminal lid names a tab in reads.
type Zellij struct {
	DisplayName string `yaml:"display_name"`
}

type Herdr struct {
	Panes bool `yaml:"panes"`
}

type Handoff struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

// Paths grants a nono session access to host directories the sandbox would
// otherwise refuse. A session is caged around the project, so anything the
// agent reads from elsewhere - a hooks directory a dotfiles repo owns, a
// package manager's global config - has to be named here or it is denied.
type Paths struct {
	Read     []string `yaml:"read"`
	ReadFile []string `yaml:"read_file"`
	Allow    []string `yaml:"allow"`
	Deny     []string `yaml:"deny"`
}

func (p Paths) IsSet() bool {
	return len(p.Read) > 0 || len(p.ReadFile) > 0 || len(p.Allow) > 0 || len(p.Deny) > 0
}

// Grants returns the paths in p under the keys nono's profile uses, with a
// leading ~ rewritten to $HOME: nono expands the variable, not the shorthand.
func (p Paths) Grants() map[string][]string {
	grants := map[string][]string{}
	for key, paths := range map[string][]string{
		"read":      p.Read,
		"read_file": p.ReadFile,
		"allow":     p.Allow,
		"deny":      p.Deny,
	} {
		for _, path := range paths {
			grants[key] = append(grants[key], expandHome(path))
		}
	}
	return grants
}

func expandHome(path string) string {
	if path == "~" {
		return "$HOME"
	}
	if strings.HasPrefix(path, "~/") {
		return "$HOME/" + path[2:]
	}
	return path
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

// Resources sizes an sbx sandbox. Both are fixed when the sandbox is created,
// and zero values leave sbx to its own defaults.
type Resources struct {
	CPUs   int    `yaml:"cpus"`
	Memory string `yaml:"memory"`
}

func (r Resources) IsSet() bool {
	return r.CPUs != 0 || r.Memory != ""
}

// ExtraWorkspace is a host directory mounted into an sbx sandbox beside the
// project, written as sbx takes it: HOST, HOST:ro, HOST:SANDBOX_PATH or
// HOST:SANDBOX_PATH:ro.
type ExtraWorkspace struct {
	Host     string
	Sandbox  string
	ReadOnly bool
}

func ParseExtraWorkspace(entry string) (ExtraWorkspace, error) {
	parts := strings.Split(entry, ":")
	w := ExtraWorkspace{Host: parts[0]}
	rest := parts[1:]
	if n := len(rest); n > 0 && rest[n-1] == "ro" {
		w.ReadOnly = true
		rest = rest[:n-1]
	}
	if len(rest) == 1 {
		w.Sandbox = rest[0]
	}
	if len(rest) > 1 || (len(rest) == 1 && !filepath.IsAbs(w.Sandbox)) {
		return ExtraWorkspace{}, failure.New("extra workspace %q is not a workspace mount", entry).
			WithFix("write it as HOST, HOST:ro, HOST:SANDBOX_PATH or HOST:SANDBOX_PATH:ro, with absolute paths")
	}
	if strings.TrimSpace(w.Host) == "" {
		return ExtraWorkspace{}, failure.New("extra workspace %q has no host path", entry).
			WithFix("name the host directory to mount")
	}
	if w.Host != "~" && !strings.HasPrefix(w.Host, "~/") && !filepath.IsAbs(w.Host) {
		return ExtraWorkspace{}, failure.New("extra workspace %q is not an absolute path", entry).
			WithFix("write the host path in full, from / or ~")
	}
	return w, nil
}

// Arg returns w as sbx create takes it, with a leading ~ on the host path
// expanded to home: sbx gets the path as an argument, not through a shell.
func (w ExtraWorkspace) Arg(home string) string {
	host := w.Host
	if host == "~" {
		host = home
	} else if strings.HasPrefix(host, "~/") {
		host = filepath.Join(home, host[2:])
	}
	arg := host
	if w.Sandbox != "" {
		arg += ":" + w.Sandbox
	}
	if w.ReadOnly {
		arg += ":ro"
	}
	return arg
}

type Config struct {
	Name            string            `yaml:"name"`
	Agent           string            `yaml:"agent"`
	DisplayName     string            `yaml:"display_name"`
	Backend         string            `yaml:"backend"`
	BaseImage       string            `yaml:"base_image"`
	Template        string            `yaml:"template"`
	Workspace       string            `yaml:"workspace"`
	Clone           bool              `yaml:"clone"`
	Packages        []string          `yaml:"packages"`
	Runtimes        map[string]Scalar `yaml:"runtimes"`
	Ports           []Scalar          `yaml:"ports"`
	Env             map[string]Scalar `yaml:"env"`
	EnvFile         []string          `yaml:"env_file"`
	Network         []string          `yaml:"network"`
	NetworkProfile  string            `yaml:"network_profile"`
	Audit           bool              `yaml:"audit"`
	AllowCommands   []string          `yaml:"allow_commands"`
	Paths           Paths             `yaml:"paths"`
	Scripts         Scripts           `yaml:"scripts"`
	Shell           Shell             `yaml:"shell"`
	Claude          Claude            `yaml:"claude"`
	PHP             PHP               `yaml:"php"`
	Zellij          Zellij            `yaml:"zellij"`
	Memory          string            `yaml:"memory"`
	Herdr           Herdr             `yaml:"herdr"`
	Handoff         Handoff           `yaml:"handoff"`
	Bridges         []Bridge          `yaml:"bridges"`
	MCP             []string          `yaml:"mcp"`
	Resources       Resources         `yaml:"resources"`
	ExtraWorkspaces []string          `yaml:"extra_workspaces"`

	Path          string        `yaml:"-"`
	ProjectDir    string        `yaml:"-"`
	BackendSource BackendSource `yaml:"-"`
	Selected      string        `yaml:"-"`
}

// Discover returns the first config candidate present under projectDir.
func Discover(projectDir string) (string, error) {
	for _, rel := range candidates {
		path := filepath.Join(projectDir, rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", failure.New("no lid.yaml found").At(projectDir).WithFix("run 'lid init' to create one")
}

// Resolve returns the config path to use: override when set, else discovery.
func Resolve(projectDir, override string) (string, error) {
	if override == "" {
		return Discover(projectDir)
	}
	info, err := os.Stat(override)
	if err != nil {
		return "", failure.New("cannot read the config passed to --config").At(override).WithFix("%v", err)
	}
	if info.IsDir() {
		return "", failure.New("the path passed to --config is a directory").At(override).
			WithFix("point it at the lid.yaml inside")
	}
	return override, nil
}

// BackendSource says where a config's backend came from.
type BackendSource string

const (
	SourceFlag      BackendSource = "flag"
	SourceSelection BackendSource = "selection"
	SourceFile      BackendSource = "file"
	SourceDefault   BackendSource = "default"
)

type fileWithSections struct {
	Config   `yaml:",inline"`
	Backends map[string]Config `yaml:"backends"`
}

// Load resolves and parses the config for projectDir, which becomes the
// config's ProjectDir even when override points somewhere else entirely.
func Load(projectDir, override string) (*Config, error) {
	return LoadFor(projectDir, override, "")
}

// LoadFor is Load with backend, when set, taking precedence over the file's.
func LoadFor(projectDir, override, backend string) (*Config, error) {
	if backend != "" {
		if err := ValidateBackend(backend); err != nil {
			return nil, err
		}
	}
	path, err := Resolve(projectDir, override)
	if err != nil {
		return nil, err
	}
	cfg, err := ParseFor(path, backend)
	if err != nil {
		return nil, err
	}
	cfg.ProjectDir = projectDir
	return cfg, nil
}

// Parse reads the config at path for the backend it declares.
func Parse(path string) (*Config, error) {
	return ParseFor(path, "")
}

// ParseFor reads the config at path as backend sees it, applies defaults and
// validates it; an empty backend means the file's own. Unknown keys are
// rejected: a mistyped 'runtime' would otherwise silently discard every
// runtime. Every declared backends section is validated too, so a mistake
// in one surfaces before the day you switch to it. ProjectDir is inferred
// from the file's location; Load overrides it with the directory actually
// being worked in.
func ParseFor(path, backend string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, failure.New("cannot read the config").At(path).WithFix("%v", err)
	}
	ref := (&Config{Path: path, ProjectDir: projectDirFor(path)}).Ref()

	if _, _, err := effectiveDocument(data, ""); err != nil {
		var f *failure.Error
		if errors.As(err, &f) {
			return nil, failure.At(err, ref)
		}
	}

	var file fileWithSections
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return nil, failure.New("%s", cleanYAMLError(err)).At(ref)
	}

	active, source := backend, SourceFlag
	switch {
	case backend != "":
		if file.Backend != "" {
			if err := ValidateBackend(file.Backend); err != nil {
				return nil, failure.At(err, ref)
			}
		}
	case file.Backend != "":
		active, source = file.Backend, SourceFile
	default:
		active, source = DefaultBackend, SourceDefault
	}

	cfg, hasSections, err := configFor(data, path, active, &file.Config)
	if err != nil {
		return nil, failure.At(err, ref)
	}
	cfg.BackendSource = source
	if err := validated(cfg); err != nil {
		return nil, err
	}
	if !hasSections {
		return cfg, nil
	}

	for _, name := range Backends {
		if _, declared := file.Backends[name]; !declared || name == active {
			continue
		}
		other, _, err := configFor(data, path, name, nil)
		if err == nil {
			err = other.Validate()
		}
		if err != nil {
			var f *failure.Error
			if errors.As(err, &f) {
				f.What = sectionsKey + "." + name + ": " + f.What
				return nil, failure.At(err, ref)
			}
			return nil, fmt.Errorf("%s: %s.%s: %w", path, sectionsKey, name, err)
		}
	}
	return cfg, nil
}

// configFor builds the config backend sees, with its defaults applied. A file
// without sections reuses plain, already strictly decoded, as is.
func configFor(data []byte, path, backend string, plain *Config) (*Config, bool, error) {
	doc, hasSections, err := effectiveDocument(data, backend)
	if err != nil {
		return nil, hasSections, err
	}
	cfg := &Config{}
	switch {
	case !hasSections && plain != nil:
		cfg = plain
	case doc != nil:
		if err := doc.Decode(cfg); err != nil {
			return nil, hasSections, failure.New("%s", cleanYAMLError(err))
		}
	}
	cfg.Path, cfg.ProjectDir = path, projectDirFor(path)
	cfg.Backend = backend
	if cfg.Agent == "" {
		cfg.Agent = DefaultAgent
	}
	if cfg.BaseImage == "" && cfg.BuildsImage() && cfg.Template == "" {
		cfg.BaseImage = DefaultBaseImage
	}
	return cfg, hasSections, nil
}

func validated(cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		var f *failure.Error
		if !errors.As(err, &f) {
			return fmt.Errorf("%s: %w", cfg.Path, err)
		}
		return failure.At(err, cfg.Ref())
	}
	return nil
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
		return failure.New("no name set").WithFix("add 'name: my-project'")
	}
	if len(name) > MaxNameLen {
		return failure.New("name %q is %d characters", name, len(name)).
			WithFix("shorten it to %d or fewer", MaxNameLen)
	}
	if !nameRe.MatchString(name) {
		return failure.New("name %q is not usable as a sandbox name", name).
			WithFix("use lowercase letters, digits and dashes, starting and ending with a letter or digit")
	}
	return nil
}

func (c *Config) Validate() error {
	if err := ValidateName(c.Name); err != nil {
		return err
	}

	if !contains(KnownAgents, c.Agent) {
		return failure.New("unknown agent %q", c.Agent).WithFix("use one of %s", strings.Join(KnownAgents, ", "))
	}

	if !contains(Backends, c.Backend) {
		return failure.New("unknown backend %q", c.Backend).WithFix("use one of %s", strings.Join(Backends, ", "))
	}

	if err := c.validateBackendExclusions(); err != nil {
		return err
	}

	if err := c.validateTemplateExclusions(); err != nil {
		return err
	}

	for name := range c.Runtimes {
		if !contains(Runtimes, name) {
			return failure.New("unknown runtime %q", name).WithFix("use one of %s", strings.Join(Runtimes, ", "))
		}
	}
	if _, hasPnpm := c.Runtimes["pnpm"]; hasPnpm {
		if _, hasNode := c.Runtimes["node"]; !hasNode {
			return failure.New("the pnpm runtime requires node").WithFix("add node to runtimes")
		}
	}

	if err := c.validatePaths(); err != nil {
		return err
	}

	if c.Workspace != "" && !filepath.IsAbs(c.Workspace) {
		return failure.New("workspace %q is not an absolute path", c.Workspace).
			WithFix("workspace is a path inside the sandbox, so start it with /")
	}

	for _, port := range c.Ports {
		if err := validatePort(string(port)); err != nil {
			return err
		}
	}

	if c.NetworkProfile != "" && !contains(NetworkProfiles, c.NetworkProfile) {
		return failure.New("unknown network profile %q", c.NetworkProfile).
			WithFix("use one of %s", strings.Join(NetworkProfiles, ", "))
	}

	for _, domain := range c.Network {
		if err := ValidateDomain(domain); err != nil {
			return err
		}
	}

	if c.Handoff.URL != "" {
		u, err := url.Parse(c.Handoff.URL)
		if err != nil || u.Host == "" || u.Port() == "" {
			return failure.New("handoff url %q has no host and port", c.Handoff.URL).
				WithFix("write it as http://host:port")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return failure.New("handoff url %q is not http", c.Handoff.URL).
				WithFix("write it as http://host:port")
		}
	}

	for _, b := range c.Bridges {
		if err := b.validate(); err != nil {
			return err
		}
	}

	for _, name := range c.MCP {
		if err := validateMCPName(name); err != nil {
			return err
		}
	}

	if err := c.Resources.validate(); err != nil {
		return err
	}

	for _, entry := range c.ExtraWorkspaces {
		if _, err := ParseExtraWorkspace(entry); err != nil {
			return err
		}
	}

	return nil
}

func (r Resources) validate() error {
	if r.CPUs < 0 {
		return failure.New("resources.cpus is %d", r.CPUs).
			WithFix("give it a positive number of CPUs, or leave it out to use all of the host's")
	}
	if r.Memory == "" {
		return nil
	}
	m := memoryRe.FindStringSubmatch(r.Memory)
	if m == nil {
		return failure.New("resources.memory %q is not a memory size", r.Memory).
			WithFix("write it in binary units, like 512m or 8g")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || (strings.EqualFold(m[2], "m") && n < 512) {
		return failure.New("resources.memory %q is below 512m", r.Memory).
			WithFix("sbx needs at least 512m")
	}
	return nil
}

func validateMCPName(name string) error {
	if strings.TrimSpace(name) == "" {
		return failure.New("an mcp entry is empty").
			WithFix("remove it, or name a server registered with 'sbx mcp add'")
	}
	if strings.ContainsAny(name, ", \t") {
		return failure.New("mcp entry %q is not a single server name", name).
			WithFix("list each server on its own line")
	}
	if strings.EqualFold(name, "none") {
		return failure.New("mcp entry %q is reserved", name).
			WithFix("leave mcp empty to give the sandbox no MCP servers")
	}
	return nil
}

func (c *Config) BuildsImage() bool {
	return c.Backend != BackendNono
}

func (c *Config) validateBackendExclusions() error {
	if c.BuildsImage() {
		for _, conflict := range []struct {
			field string
			set   bool
			why   string
		}{
			{"allow_commands", len(c.AllowCommands) > 0, "a container image has no host command shims to lift"},
			{"network_profile", c.NetworkProfile != "", "a container image has no nono network policy to pick"},
			{"audit", c.Audit, "a container image has no nono audit trail to keep"},
			{"paths", c.Paths.IsSet(), "a container image has no host filesystem to grant"},
		} {
			if conflict.set {
				return failure.New("the %s backend cannot use %s", c.Backend, conflict.field).
					WithFix("%s, so drop it, or switch to the %s backend", conflict.why, BackendNono)
			}
		}
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
		{"clone", c.Clone, "a nono session works in the project itself, so there is nothing to clone into"},
		{"resources", c.Resources.IsSet(), "a nono session runs on the host's own CPUs and memory"},
		{"extra_workspaces", len(c.ExtraWorkspaces) > 0, "grant host directories with paths instead"},
	} {
		if conflict.set {
			return failure.New("the %s backend cannot use %s", c.Backend, conflict.field).WithFix("%s, or switch to the %s backend", conflict.why, BackendSbx)
		}
	}
	return nil
}

// validatePaths keeps a grant from reaching nono as a path it cannot resolve. A
// relative one would be read against wherever the session happened to launch,
// which is not what the person who wrote it meant.
func (c *Config) validatePaths() error {
	for key, paths := range c.Paths.Grants() {
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				return failure.New("a path in paths.%s is empty", key).
					WithFix("remove it, or name the directory it should grant")
			}
			if !filepath.IsAbs(path) && !strings.HasPrefix(path, "$") {
				return failure.New("path %q in paths.%s is not absolute", path, key).
					WithFix("write it in full, from / or ~, so it means the same wherever the session starts")
			}
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
		{"base_image", c.BaseImage != "", "a custom template is already built, so there is nothing to build it on"},
		{"packages", len(c.Packages) > 0, "install them in your Dockerfile"},
		{"runtimes", len(c.Runtimes) > 0, "install them in your Dockerfile"},
		{"bridges", len(c.Bridges) > 0, "a custom template controls its own socat setup and session hooks"},
		{"scripts.on_start", len(c.Scripts.OnStart) > 0, "lid bakes these into the image's session hook, which a custom template owns"},
	} {
		if conflict.set {
			return failure.New("template and %s cannot both be set", conflict.field).WithFix("%s", conflict.why)
		}
	}
	return nil
}

func (b Bridge) validate() error {
	if b.Name == "" {
		return failure.New("a bridge has no name").
			WithFix("name is the hostname the app connects to, like 'name: postgres'")
	}
	if !hostnameRe.MatchString(b.Name) {
		return failure.New("bridge name %q is not a hostname", b.Name).
			WithFix("use letters, digits, dashes and dots")
	}
	if !validPortNumber(b.Port) {
		return failure.New("bridge %q has port %d", b.Name, b.Port).
			WithFix("ports run from 1 to 65535")
	}
	if !validPortNumber(b.HostPort) {
		return failure.New("bridge %q has host_port %d", b.Name, b.HostPort).
			WithFix("ports run from 1 to 65535")
	}
	return nil
}

type Port struct {
	Host     int
	Sandbox  int
	Protocol string
}

func ParsePort(port string) (Port, error) {
	if err := validatePort(port); err != nil {
		return Port{}, err
	}
	m := portRe.FindStringSubmatch(port)
	parsed := Port{Protocol: m[3]}
	if m[1] != "" {
		parsed.Host, _ = strconv.Atoi(m[1])
	}
	parsed.Sandbox, _ = strconv.Atoi(m[2])
	return parsed, nil
}

func validatePort(port string) error {
	m := portRe.FindStringSubmatch(port)
	if m == nil {
		return failure.New("port %q is not a port mapping", port).
			WithFix("write it as PORT, HOST:PORT, or HOST:PORT/PROTOCOL")
	}
	for _, field := range []string{m[1], m[2]} {
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || !validPortNumber(n) {
			return failure.New("port %q is out of range", port).WithFix("ports run from 1 to 65535")
		}
	}
	return nil
}

func validPortNumber(n int) bool { return n >= 1 && n <= 65535 }

func (c Claude) IsSet() bool {
	return c.Theme != "" ||
		len(c.Plugins.Marketplaces) > 0 ||
		len(c.Plugins.Install) > 0 ||
		len(c.Plugins.InstallOptional) > 0 ||
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

// TabName is the name lid gives the terminal tab it starts in: the top-level
// display_name, or the deprecated zellij.display_name when that is all there is.
func (c *Config) TabName() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Zellij.DisplayName
}

// Deprecations lists the keys in the config that still work but should be
// moved, each with where they go now.
func (c *Config) Deprecations() []string {
	var notes []string
	if c.Zellij.DisplayName != "" {
		note := "zellij.display_name in " + c.Ref() + " is deprecated; move it to a top-level display_name: " + c.Zellij.DisplayName
		if c.DisplayName != "" {
			note = "zellij.display_name in " + c.Ref() + " is deprecated and ignored, since display_name is set; remove it"
		}
		notes = append(notes, note)
	}
	return notes
}

// Ref names the config relative to the project, so generated files and error
// messages never carry an absolute path from the machine that produced them.
func (c *Config) Ref() string {
	if rel, err := filepath.Rel(c.ProjectDir, c.Path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(c.Path)
}

func (c *Config) HandoffURL() string {
	if c.Handoff.URL != "" {
		return c.Handoff.URL
	}
	host := "localhost"
	if c.BuildsImage() {
		host = "host.docker.internal"
	}
	return fmt.Sprintf("http://%s:%d", host, DefaultHandoffPort)
}

func (c *Config) HandoffPort() int {
	u, err := url.Parse(c.HandoffURL())
	if err != nil {
		return DefaultHandoffPort
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return DefaultHandoffPort
	}
	return port
}

func (c *Config) KitDir() string {
	return kitDirFor(c.ProjectDir)
}

func (c *Config) SbxAgent() string {
	if c.Agent == AgentClaudeCode {
		return "claude"
	}
	return c.Agent
}

func (c *Config) SandboxImageName() string {
	return c.Name + "-sandbox"
}

// FileHash fingerprints the config as this backend sees it, leaving out what
// lid applies at start rather than bakes into the image. A file without
// backends sections hashes exactly as it always has.
func (c *Config) FileHash() (string, error) {
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return "", failure.New("cannot read the config to hash it").At(c.Ref()).WithFix("%v", err)
	}
	doc, hasSections, err := effectiveDocument(data, c.Backend)
	if err != nil || !hasSections {
		return hashOf(withoutStartApplied(data)), nil
	}
	var merged map[string]any
	if err := doc.Decode(&merged); err != nil {
		return hashOf(data), nil
	}
	delete(merged, "backend")
	return hashOf(strippedJSON(merged, data)), nil
}

func withoutStartApplied(data []byte) []byte {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return data
	}
	return strippedJSON(doc, data)
}

func strippedJSON(doc map[string]any, fallback []byte) []byte {
	delete(doc, "memory")
	delete(doc, "zellij")
	delete(doc, "herdr")
	delete(doc, "display_name")
	delete(doc, "shell")
	if claude, ok := doc["claude"].(map[string]any); ok {
		delete(claude, "settings")
		delete(claude, "theme")
		if len(claude) == 0 {
			delete(doc, "claude")
		}
	}
	stripped, err := json.Marshal(doc)
	if err != nil {
		return fallback
	}
	return stripped
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
			var f *failure.Error
			if errors.As(err, &f) {
				f.In = entry
			}
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
