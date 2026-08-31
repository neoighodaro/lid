package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

const (
	nonoBaseProfile    = "default"
	nonoNetworkProfile = "minimal"
	nonoMinVersion     = "0.63.0"
)

var nonoAgentGroups = map[string][]string{
	config.AgentClaudeCode: {"claude_code_macos", "claude_code_linux"},
	"codex":                {"codex_macos"},
	"opencode":             {"opencode_linux"},
	"shell":                {},
}

var nonoAgentCommands = map[string][]string{
	config.AgentClaudeCode: {"claude"},
	"codex":                {"codex"},
	"opencode":             {"opencode"},
	"shell":                nil,
}

var nonoRuntimeGroups = map[string][]string{
	"bun":    {"bun_runtime"},
	"go":     {"go_runtime"},
	"node":   {"node_runtime"},
	"php":    {"homebrew_macos", "homebrew_linux"},
	"pnpm":   {"node_runtime"},
	"python": {"python_runtime"},
	"rust":   {"rust_runtime"},
}

type nonoProfile struct {
	Extends     string           `json:"extends"`
	Meta        nonoMeta         `json:"meta"`
	Groups      nonoGroups       `json:"groups"`
	Workdir     nonoWorkdir      `json:"workdir"`
	Interactive bool             `json:"interactive"`
	Network     nonoNetwork      `json:"network"`
	Environment *nonoEnvironment `json:"environment,omitempty"`
}

type nonoMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type nonoGroups struct {
	Include []string `json:"include"`
}

type nonoWorkdir struct {
	Access string `json:"access"`
}

type nonoNetwork struct {
	NetworkProfile string   `json:"network_profile"`
	AllowDomain    []string `json:"allow_domain,omitempty"`
}

type nonoEnvironment struct {
	SetVars map[string]string `json:"set_vars"`
}

type nono struct{}

func (nono) Name() string { return config.BackendNono }

func (nono) EnsureAvailable(h Host) error {
	if _, err := h.lookPath("nono"); err != nil {
		return failure.New("nono is not installed").
			WithFix("install it from https://nono.sh, or set 'backend: %s' to use a container instead", config.BackendSbx)
	}
	out, err := h.output([]string{"nono", "--version"})
	if err != nil {
		return failure.New("cannot run 'nono --version': %s", firstLine(out)).
			WithFix("check that the nono on your PATH runs")
	}
	return checkNonoVersion(out)
}

// Provision writes the profile nono will be given and has nono check it. An
// unchanged profile is left alone: re-validating it costs a subprocess and
// tells the user nothing new.
func (n nono) Provision(h Host, cfg *config.Config) (Provisioned, error) {
	profile, err := n.Render(cfg)
	if err != nil {
		return Provisioned{}, err
	}

	path, ref := nonoProfilePath(cfg), nonoProfileRef(cfg)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == profile {
		return Provisioned{Path: path, Ref: ref}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Provisioned{}, failure.New("cannot create the directory for the nono profile").
			At(filepath.Dir(path)).
			WithFix("%v", err)
	}
	if err := os.WriteFile(path, []byte(profile), 0o644); err != nil {
		return Provisioned{}, failure.New("cannot write the nono profile").
			At(ref).
			WithFix("%v", err)
	}

	if out, err := h.output([]string{"nono", "profile", "validate", path}); err != nil {
		return Provisioned{}, failure.New("nono rejected the profile lid generated: %s", firstLine(out)).
			At(ref).
			WithFix("this is a bug in lid; run 'nono profile validate %s' for the full report", ref)
	}
	return Provisioned{Path: path, Ref: ref, Changed: true}, nil
}

// StartArgv is the command that hands the terminal to the sandboxed agent.
// --workdir is passed explicitly so a session started from a subdirectory still
// resolves $WORKDIR to the project root the profile was written for.
func (nono) StartArgv(cfg *config.Config, profilePath string) ([]string, error) {
	command, ok := nonoAgentCommands[cfg.Agent]
	if !ok {
		return nil, failure.New("the nono backend cannot launch agent %q", cfg.Agent).
			At(cfg.Ref()).
			WithFix("use one of %s", strings.Join(nonoAgentNames(), ", "))
	}

	verb := "run"
	if len(command) == 0 {
		verb = "shell"
	}

	argv := []string{"nono", verb, "--profile", profilePath, "--name", cfg.Name, "--workdir", cfg.ProjectDir}
	if len(command) > 0 {
		argv = append(argv, "--")
		argv = append(argv, command...)
	}
	return argv, nil
}

func nonoProfilePath(cfg *config.Config) string {
	return filepath.Join(cfg.KitDir(), "nono.jsonc")
}

// nonoProfileRef names the profile relative to the project, so errors and fix
// lines never carry an absolute path from the machine that produced them.
func nonoProfileRef(cfg *config.Config) string {
	if rel, err := filepath.Rel(cfg.ProjectDir, nonoProfilePath(cfg)); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return nonoProfilePath(cfg)
}

func (nono) Validate(cfg *config.Config) ([]string, error) {
	if _, ok := nonoAgentGroups[cfg.Agent]; !ok {
		return nil, failure.New("the nono backend cannot sandbox agent %q", cfg.Agent).
			At(cfg.Ref()).
			WithFix("use one of %s", strings.Join(nonoAgentNames(), ", "))
	}

	var warnings []string
	for _, check := range []struct {
		set  bool
		what string
		why  string
	}{
		{cfg.Claude.IsSet(), "claude", "a nono session runs the agent on your host, where it already uses your real ~/.claude"},
		{len(cfg.Shell.Aliases) > 0 || cfg.Shell.Directory != "", "shell.aliases", "a nono session uses your host shell config"},
		{cfg.Memory != "", "memory", "a nono session reads the project's real memory file, which lid will not rewrite"},
		{len(cfg.PHP.Extensions) > 0, "php.extensions", "lid cannot install php extensions on your host"},
		{cfg.Workspace != "", "workspace", "a nono session works in the project directory itself, so there is nowhere to mount it"},
		{cfg.Handoff.Enabled, "handoff", "not wired up for the nono backend yet"},
		{len(cfg.Bridges) > 0, "bridges", "a nono session shares the host network, so host services are already reachable"},
		{len(cfg.Scripts.OnStart) > 0, "scripts.on_start", "not wired up for the nono backend yet"},
	} {
		if check.set {
			warnings = append(warnings, fmt.Sprintf("ignoring %s: %s", check.what, check.why))
		}
	}
	return warnings, nil
}

func (n nono) Render(cfg *config.Config) (string, error) {
	if _, err := n.Validate(cfg); err != nil {
		return "", err
	}

	env, err := cfg.ResolvedEnv()
	if err != nil {
		return "", err
	}

	profile := nonoProfile{
		Extends: nonoBaseProfile,
		Meta: nonoMeta{
			Name:        cfg.Name,
			Description: fmt.Sprintf("Generated by lid from %s. Do not edit.", cfg.Ref()),
		},
		Groups:      nonoGroups{Include: nonoGroupsFor(cfg)},
		Workdir:     nonoWorkdir{Access: "readwrite"},
		Interactive: true,
		Network: nonoNetwork{
			NetworkProfile: nonoNetworkProfile,
			AllowDomain:    cfg.Network,
		},
	}
	if len(env) > 0 {
		profile.Environment = &nonoEnvironment{SetVars: env}
	}

	body, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", failure.New("cannot render the nono profile: %v", err).
			WithFix("this is a bug in lid, not a problem with your config")
	}

	header := fmt.Sprintf("// Generated by lid from %s — edit that file, not this one.\n// nono profile for the %q sandbox.\n", cfg.Ref(), cfg.Name)
	return header + string(body) + "\n", nil
}

// nonoGroupsFor lists the agent's groups then each declared runtime's, walking
// config.Runtimes rather than the config's map so the order never shifts.
func nonoGroupsFor(cfg *config.Config) []string {
	groups := make([]string, 0, 8)
	seen := make(map[string]bool)
	add := func(names []string) {
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				groups = append(groups, name)
			}
		}
	}

	add(nonoAgentGroups[cfg.Agent])
	for _, name := range config.Runtimes {
		if _, ok := cfg.Runtimes[name]; ok {
			add(nonoRuntimeGroups[name])
		}
	}
	return groups
}

func nonoAgentNames() []string {
	names := make([]string, 0, len(nonoAgentGroups))
	for name := range nonoAgentGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var nonoVersionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func checkNonoVersion(output string) error {
	found := nonoVersionRe.FindStringSubmatch(output)
	if found == nil {
		return failure.New("cannot read a version from 'nono --version', which printed %q", firstLine(output)).
			WithFix("check that the nono on your PATH is the sandbox tool")
	}

	want := nonoVersionRe.FindStringSubmatch(nonoMinVersion)
	for i := 1; i <= 3; i++ {
		got, _ := strconv.Atoi(found[i])
		min, _ := strconv.Atoi(want[i])
		if got > min {
			return nil
		}
		if got < min {
			return failure.New("nono %s is older than the %s lid's profiles need", strings.Join(found[1:4], "."), nonoMinVersion).
				WithFix("update nono, then run this again")
		}
	}
	return nil
}
