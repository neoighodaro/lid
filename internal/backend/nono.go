package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/panes"
)

const (
	nonoQuietBash              = "BASH_SILENCE_DEPRECATION_WARNING=1"
	nonoBaseProfile            = "default"
	nonoFallbackNetworkProfile = "developer"
	nonoMinVersion             = "0.74.0"
)

// nonoAgentPacks is the registry pack whose profile lid builds on for each
// agent. The pack is where nono maintains what the agent actually needs — its
// credential store, its OAuth origins — so lid extends it instead of
// reassembling it from groups. An agent with no entry extends the base profile.
var nonoAgentPacks = map[string]string{
	config.AgentClaudeCode: "nolabs-ai/claude",
	"codex":                "nolabs-ai/codex",
}

// nonoAgentGroups is what lid adds on top of the agent's base. For an agent
// with a pack this is only the platform variants the pack leaves out, so the
// profile still resolves on a machine other than the one that generated it.
var nonoAgentGroups = map[string][]string{
	config.AgentClaudeCode: {"claude_code_linux"},
	"codex":                {"codex_macos"},
	"opencode":             {"opencode_linux"},
	"shell":                {},
}

var nonoCommandDenies = map[string][]string{
	"dangerous_commands": {
		"rm", "rmdir", "dd", "chmod", "chown", "chgrp", "mv", "cp", "truncate",
		"scp", "rsync", "sftp", "ftp", "xargs", "sudo", "su", "doas", "pip",
		"npm", "kill", "killall", "pkill", "shutdown", "reboot", "halt",
		"poweroff",
	},
	"dangerous_commands_macos": {"srm", "brew", "launchctl"},
	"dangerous_commands_linux": {
		"shred", "mkfs", "mkfs.ext4", "mkfs.xfs", "mkfs.btrfs", "mkswap",
		"fdisk", "parted", "gdisk", "wipefs", "chattr", "init", "systemctl",
		"apt", "apt-get", "dpkg", "yum", "dnf", "pacman", "pkexec",
	},
}

var nonoCommandGroups = func() map[string]string {
	groups := make(map[string]string)
	for group, commands := range nonoCommandDenies {
		for _, command := range commands {
			groups[command] = group
		}
	}
	return groups
}()

var nonoAgentNetworkProfiles = map[string]string{
	config.AgentClaudeCode: "claude-code",
	"codex":                "codex",
	"opencode":             "opencode",
	"shell":                nonoFallbackNetworkProfile,
}

var nonoAgentCommands = map[string][]string{
	config.AgentClaudeCode: {"claude"},
	"codex":                {"codex"},
	"opencode":             {"opencode"},
	"shell":                nil,
}

var nonoRuntimeWrites = map[string][]string{
	"bun":    {"$HOME/.bun/install/cache"},
	"go":     {"$HOME/go/pkg"},
	"node":   {"$HOME/.npm"},
	"pnpm":   {"$HOME/Library/pnpm/store", "$HOME/.local/share/pnpm/store", "$HOME/.config/pnpm"},
	"python": {"$HOME/.local/share/uv"},
	"rust":   {"$HOME/.cargo"},
}

var nonoRuntimeDenies = map[string][]string{
	"rust": {"$HOME/.cargo/bin"},
}

var nonoRuntimeManagers = map[string]string{
	"node":   "npm",
	"php":    "brew",
	"python": "pip",
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
	Packs       []string         `json:"packs,omitempty"`
	Meta        nonoMeta         `json:"meta"`
	Groups      nonoGroups       `json:"groups"`
	Workdir     nonoWorkdir      `json:"workdir"`
	Network     nonoNetwork      `json:"network"`
	Filesystem  *nonoFilesystem  `json:"filesystem,omitempty"`
	Environment *nonoEnvironment `json:"environment,omitempty"`
}

type nonoMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type nonoGroups struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude,omitempty"`
}

type nonoWorkdir struct {
	Access string `json:"access"`
}

type nonoFilesystem struct {
	Allow    []string `json:"allow,omitempty"`
	Read     []string `json:"read,omitempty"`
	ReadFile []string `json:"read_file,omitempty"`
	Deny     []string `json:"deny,omitempty"`
}

type nonoNetwork struct {
	NetworkProfile string   `json:"network_profile"`
	AllowDomain    []string `json:"allow_domain,omitempty"`
	ListenPort     []int    `json:"listen_port,omitempty"`
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
func (n nono) Provision(h Host, cfg *config.Config, _ Options) (Provisioned, error) {
	profile, err := n.Render(cfg)
	if err != nil {
		return Provisioned{}, err
	}

	// The pack is checked before the profile is compared: a checkout carried to
	// a new machine has an up-to-date profile and no pack at all.
	var notes []string
	if pack := nonoAgentPacks[cfg.Agent]; pack != "" {
		pulled, err := ensureNonoPack(h, pack)
		if err != nil {
			return Provisioned{}, err
		}
		if pulled {
			notes = append(notes, "Installed "+pack)
		}
	}

	path, ref := nonoProfilePath(cfg), nonoProfileRef(cfg)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == profile {
		return Provisioned{Path: path, Ref: ref, Notes: notes}, nil
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
	notes = append(notes, nonoDeniedManagerNotes(cfg)...)
	return Provisioned{Path: path, Ref: ref, Changed: true, Notes: append(notes, handoffSkillNote(cfg)...)}, nil
}

func nonoCreatedMarker(cfg *config.Config) string {
	return filepath.Join(cfg.KitDir(), "nono-created")
}

func (nono) PrepareAgent(h Host, cfg *config.Config) error {
	return errors.Join(nonoApplyClaudeSettings(cfg), nonoRunOnCreate(h, cfg), nonoRunOnStart(h, cfg))
}

// nonoApplyClaudeSettings merges the claude: block into the project's own
// settings file. nono runs the agent under the user's real home, so the global
// settings there are not lid's to rewrite; the project-local file is, and the
// config lid was given wins over what the file already says. lid merges into
// that file only when the project already keeps one - creating it would be lid
// deciding a project should have settings it never asked for.
func nonoApplyClaudeSettings(cfg *config.Config) error {
	if !config.IsClaudeAgent(cfg.Agent) {
		return nil
	}
	want := claudeHostSettings(cfg)
	if len(want) == 0 {
		return nil
	}

	path := nonoLocalSettingsPath(cfg)
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failure.New("cannot read the agent's settings for this project").
			At(path).
			WithFix("%v", err)
	}

	body, changed, err := applySettings(string(current), want)
	if err != nil || !changed {
		return err
	}
	return writeBeside(path, []byte(body+"\n"))
}

func nonoLocalSettingsPath(cfg *config.Config) string {
	return filepath.Join(cfg.ProjectDir, claudeLocalSettingsFile)
}

// nonoAppliesClaude reports whether the claude: block has somewhere to land.
func nonoAppliesClaude(cfg *config.Config) bool {
	if !config.IsClaudeAgent(cfg.Agent) || len(claudeHostSettings(cfg)) == 0 {
		return false
	}
	_, err := os.Stat(nonoLocalSettingsPath(cfg))
	return err == nil
}

// nonoClaudeWarnings says what became of the claude: block, which lands in one
// file or nowhere at all.
func nonoClaudeWarnings(cfg *config.Config) []string {
	if !cfg.Claude.IsSet() {
		return nil
	}
	if !nonoAppliesClaude(cfg) {
		return []string{fmt.Sprintf("ignoring claude: a nono session runs on your host, and this project has no %s to merge into", claudeLocalSettingsFile)}
	}
	if claudeThemeID(cfg) == customClaudeTheme {
		return []string{"ignoring claude.theme: a nono session reads its themes from your own ~/.claude"}
	}
	return nil
}

func nonoRunOnCreate(h Host, cfg *config.Config) error {
	marker := nonoCreatedMarker(cfg)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	for _, command := range cfg.Scripts.OnCreate {
		if err := h.exec(nonoScriptArgv(cfg, command)); err != nil {
			return failure.New("the on_create command %q failed", command).
				At(cfg.Ref()).
				WithFix("fix it, or drop it from scripts.on_create; lid will try again on the next start")
		}
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
		_ = os.WriteFile(marker, nil, 0o644)
	}
	return nil
}

func nonoRunOnStart(h Host, cfg *config.Config) error {
	for _, command := range cfg.Scripts.OnStart {
		if err := h.exec(nonoScriptArgv(cfg, command)); err != nil {
			return failure.New("the on_start command %q failed", command).
				At(cfg.Ref()).
				WithFix("the output above says why; the agent starts anyway")
		}
	}
	return nil
}

func nonoScriptArgv(cfg *config.Config, command string) []string {
	return []string{"bash", "-lc", fmt.Sprintf("cd %q && %s", cfg.ProjectDir, command)}
}

// ensureNonoPack installs the pack unless the host already has it, and reports
// whether it had to. lid pulls it rather than telling the user to, so a fresh
// clone on a fresh machine needs nothing but 'lid start'.
func ensureNonoPack(h Host, pack string) (bool, error) {
	if out, err := h.output([]string{"nono", "list", "--installed"}); err == nil && strings.Contains(out, pack) {
		return false, nil
	}
	if out, err := h.output([]string{"nono", "pull", pack}); err != nil {
		return false, failure.New("cannot install the %s pack the sandbox profile needs: %s", pack, firstLine(out)).
			WithFix("check that you can reach the nono registry, then run 'nono pull %s' yourself", pack)
	}
	return true, nil
}

// StartArgv is the command that hands the terminal to the sandboxed agent.
// --workdir is passed explicitly so a session started from a subdirectory still
// resolves $WORKDIR to the project root the profile was written for, and
// --allow-cwd answers the share prompt the profile's workdir access already
// implies. The access level still comes from the profile, not the flag.
func (nono) StartArgv(cfg *config.Config, profilePath string, agentArgs []string) ([]string, error) {
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

	argv := nonoShellArgv(cfg, verb, profilePath)
	if len(command) > 0 {
		argv = append(argv, "--")
		argv = append(argv, command...)
		argv = append(argv, agentArgs...)
	}
	return argv, nil
}

// ShellArgv opens a caged shell on the host. With nothing in the shell: block
// there is nothing to add to the session, so it hands over 'nono shell' and the
// user keeps whichever shell they have chosen; a configured shell: block trades
// that for bash, the only shell lid can hand an init file to.
func (nono) ShellArgv(_ Host, cfg *config.Config, profilePath string, command []string) ([]string, error) {
	dir := shellDir(cfg, cfg.ProjectDir)

	if len(command) > 0 {
		script := fmt.Sprintf("cd %s && %s", shellQuote(dir), shellCommandLine(command))
		return append(nonoShellArgv(cfg, "run", profilePath), "--", "bash", "-lc", script), nil
	}

	if len(cfg.Shell.Aliases) == 0 && cfg.Shell.Directory == "" {
		return nonoShellArgv(cfg, "shell", profilePath), nil
	}

	rc, err := nonoWriteShellRC(cfg, dir)
	if err != nil {
		return nil, err
	}
	return append(nonoShellArgv(cfg, "run", profilePath), "--", "env", nonoQuietBash, "bash", "--rcfile", rc, "-i"), nil
}

func nonoWriteShellRC(cfg *config.Config, dir string) (string, error) {
	rc := nonoShellRCPath(cfg)
	if err := writeKitFile(cfg.ProjectDir, rc, []byte(shellRC(cfg, dir))); err != nil {
		return "", failure.New("cannot write the shell's init file: %v", err).
			WithFix("the shell would start without what the shell: block in %s describes", cfg.Ref())
	}
	return rc, nil
}

// writeKitFile writes a file the agent may be able to replace a directory or a
// link under. Every directory from root down must be a real directory, and the
// file lands through a new temporary file renamed into place, so a link where
// the file should be is replaced rather than followed.
func writeKitFile(root, path string, body []byte) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is not under %s", path, root)
	}

	dir := root
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	for _, part := range parts {
		if part == "." {
			continue
		}
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := requireRealDir(dir); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func requireRealDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is a link or not a directory", path)
	}
	return nil
}

// PaneArgv runs bash under the rc file even with an empty shell: block, since
// the rc file is what runs the command, and takes the profile path as given.
func (nono) PaneArgv(h Host, cfg *config.Config, profilePath, command string) ([]string, error) {
	exe, err := h.lookPath("nono")
	if err != nil {
		return nil, failure.New("cannot find nono on PATH: %v", err)
	}
	rc, err := nonoWriteShellRC(cfg, shellDir(cfg, cfg.ProjectDir))
	if err != nil {
		return nil, err
	}
	argv := nonoShellArgv(cfg, "run", profilePath)
	argv[0] = exe
	return append(argv, "--", "env", nonoQuietBash, PaneCmdVar+"="+command, "bash", "--rcfile", rc, "-i"), nil
}

// nonoShellArgv leaves the audit trail off unless audit: asks for it. Its
// network buffer holds 4096 events a session, and a long agent session fills
// it, after which nono warns on every request. Only 'nono run' takes the flag.
func nonoShellArgv(cfg *config.Config, verb, profilePath string) []string {
	argv := []string{"nono", verb, "--profile", profilePath, "--name", cfg.Name, "--workdir", cfg.ProjectDir, "--allow-cwd"}
	if verb == "run" && !cfg.Audit {
		argv = append(argv, "--no-audit")
	}
	if nonoLocalBroker(cfg) {
		argv = append(argv, "--open-port", strconv.Itoa(cfg.HandoffPort()))
	}
	return argv
}

func nonoShellRCPath(cfg *config.Config) string {
	return filepath.Join(cfg.KitDir(), "shellrc")
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

	for _, name := range cfg.AllowCommands {
		if _, ok := nonoCommandGroups[name]; !ok {
			return nil, failure.New("nono does not deny %q, so allow_commands cannot unblock it", name).
				At(cfg.Ref()).
				WithFix("drop it; allow_commands only lifts nono's own denials, like npm, pip, brew or rm")
		}
	}

	for _, entry := range cfg.Ports {
		port, err := config.ParsePort(string(entry))
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(port.Protocol, "udp") {
			return nil, failure.New("nono cannot grant the udp port %d", port.Sandbox).
				At(cfg.Ref()).
				WithFix("nono allows TCP listeners only, so drop the protocol or the entry")
		}
	}

	warnings := nonoUnblockWarnings(cfg)
	warnings = append(warnings, nonoRemapWarnings(cfg)...)
	warnings = append(warnings, handoffAgentWarning(cfg)...)
	warnings = append(warnings, nonoClaudeWarnings(cfg)...)
	for _, group := range []struct {
		why    string
		fields []field
	}{
		{"a nono session runs on your host", []field{
			{"memory", cfg.Memory != ""},
			{"php.extensions", len(cfg.PHP.Extensions) > 0},
			{"workspace", cfg.Workspace != ""},
			{"bridges", len(cfg.Bridges) > 0},
			{"mcp", len(cfg.MCP) > 0},
		}},
	} {
		if set := fieldsSet(group.fields); set != "" {
			warnings = append(warnings, fmt.Sprintf("ignoring %s: %s", set, group.why))
		}
	}
	return warnings, nil
}

func nonoCommandExcludes(cfg *config.Config) []string {
	var groups []string
	seen := make(map[string]bool)
	for _, name := range cfg.AllowCommands {
		group := nonoCommandGroups[name]
		if group != "" && !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
	}
	sort.Strings(groups)
	return groups
}

func nonoUnblockWarnings(cfg *config.Config) []string {
	asked := make(map[string]bool, len(cfg.AllowCommands))
	for _, name := range cfg.AllowCommands {
		asked[name] = true
	}

	var warnings []string
	for _, group := range nonoCommandExcludes(cfg) {
		commands := nonoCommandDenies[group]
		var wanted, extra []string
		for _, command := range commands {
			if asked[command] {
				wanted = append(wanted, command)
			} else if len(extra) < 3 {
				extra = append(extra, command)
			}
		}
		warnings = append(warnings, fmt.Sprintf("allowing %s unblocks all %d commands in nono's %s group, including %s",
			strings.Join(wanted, ", "), len(commands), group, strings.Join(extra, ", ")))
	}
	return warnings
}

func nonoDeniedManagerNotes(cfg *config.Config) []string {
	allowed := make(map[string]bool, len(cfg.AllowCommands))
	for _, name := range cfg.AllowCommands {
		allowed[name] = true
	}

	var notes []string
	for _, runtime := range config.Runtimes {
		manager := nonoRuntimeManagers[runtime]
		if _, declared := cfg.Runtimes[runtime]; !declared || manager == "" || allowed[manager] {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s is denied in the sandbox; set 'allow_commands: [%s]' if this project installs %s dependencies", manager, manager, runtime))
	}
	return notes
}

type field struct {
	name string
	set  bool
}

func fieldsSet(fields []field) string {
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.set {
			names = append(names, f.name)
		}
	}
	return strings.Join(names, ", ")
}

func nonoAllowDomains(cfg *config.Config) []string {
	if !cfg.Handoff.Enabled || nonoLocalBroker(cfg) {
		return cfg.Network
	}
	host := handoffHost(cfg)
	if contains(cfg.Network, host) {
		return cfg.Network
	}
	return append(append([]string{}, cfg.Network...), host)
}

func handoffHost(cfg *config.Config) string {
	if u, err := url.Parse(cfg.HandoffURL()); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "localhost"
}

// nonoLocalBroker reports a broker on this machine. nono's domain allowlist
// only covers its proxy, so such a broker is reached with --open-port instead.
func nonoLocalBroker(cfg *config.Config) bool {
	if !cfg.Handoff.Enabled {
		return false
	}
	host := handoffHost(cfg)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (n nono) Render(cfg *config.Config) (string, error) {
	if _, err := n.Validate(cfg); err != nil {
		return "", err
	}

	env, err := cfg.ResolvedEnv()
	if err != nil {
		return "", err
	}

	base, pack := nonoBaseProfile, nonoAgentPacks[cfg.Agent]
	var packs []string
	if pack != "" {
		base, packs = pack, []string{pack}
	}

	profile := nonoProfile{
		Extends: base,
		Packs:   packs,
		Meta: nonoMeta{
			Name:        cfg.Name,
			Description: fmt.Sprintf("Generated by lid from %s. Do not edit.", cfg.Ref()),
		},
		Groups:  nonoGroups{Include: nonoGroupsFor(cfg), Exclude: nonoCommandExcludes(cfg)},
		Workdir: nonoWorkdir{Access: "readwrite"},
		Network: nonoNetwork{
			NetworkProfile: nonoNetworkProfileFor(cfg),
			AllowDomain:    nonoAllowDomains(cfg),
			ListenPort:     nonoListenPorts(cfg),
		},
	}
	profile.Filesystem = nonoFilesystemFor(cfg)
	for name, value := range handoffEnv(cfg) {
		env[name] = value
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
func nonoListenPorts(cfg *config.Config) []int {
	ports := make([]int, 0, len(cfg.Ports))
	for _, entry := range cfg.Ports {
		port, err := config.ParsePort(string(entry))
		if err != nil || strings.HasPrefix(port.Protocol, "udp") {
			continue
		}
		ports = append(ports, port.Sandbox)
	}
	if len(ports) == 0 {
		return nil
	}
	return ports
}

func nonoRemapWarnings(cfg *config.Config) []string {
	var warnings []string
	for _, entry := range cfg.Ports {
		port, err := config.ParsePort(string(entry))
		if err != nil || port.Host == 0 || port.Host == port.Sandbox {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("nono cannot remap a port, so %s listens on %d, not %d", entry, port.Sandbox, port.Host))
	}
	return warnings
}

func (nono) Network(_ Host, cfg *config.Config, action NetworkAction, _ []string) error {
	fix := map[NetworkAction]string{
		NetworkAllow:  "add it to 'network:' there, which 'lid network allow <host> --persist' does, then run 'lid start'",
		NetworkDeny:   "nono blocks whatever 'network:' does not list, so take it out of there with 'lid network remove <host> --persist', then run 'lid start'",
		NetworkRemove: "drop it from 'network:' there, which 'lid network remove <host> --persist' does, then run 'lid start'",
	}[action]
	return failure.New("nono fixes what a session may reach when the session launches").
		At(cfg.Ref()).
		WithFix("%s", fix).
		Because(ErrNetworkAtLaunch)
}

func nonoNetworkProfileFor(cfg *config.Config) string {
	if cfg.NetworkProfile != "" {
		return cfg.NetworkProfile
	}
	if profile := nonoAgentNetworkProfiles[cfg.Agent]; profile != "" {
		return profile
	}
	return nonoFallbackNetworkProfile
}

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

func nonoFilesystemFor(cfg *config.Config) *nonoFilesystem {
	var fs nonoFilesystem
	for _, name := range config.Runtimes {
		if _, ok := cfg.Runtimes[name]; !ok {
			continue
		}
		fs.Allow = append(fs.Allow, nonoRuntimeWrites[name]...)
		fs.Deny = append(fs.Deny, nonoRuntimeDenies[name]...)
	}

	grants := cfg.Paths.Grants()
	fs.Read = append(fs.Read, grants["read"]...)
	fs.ReadFile = append(fs.ReadFile, grants["read_file"]...)
	fs.Allow = append(fs.Allow, grants["allow"]...)
	fs.Deny = append(fs.Deny, grants["deny"]...)
	fs.Deny = append(fs.Deny, nonoLidDenies()...)

	if len(fs.Allow) == 0 && len(fs.Deny) == 0 && len(fs.Read) == 0 && len(fs.ReadFile) == 0 {
		return nil
	}
	return &fs
}

// nonoLidDenies keeps the agent out of lid's cache dir, where the pane files
// and private profiles that decide what runs on the host are kept.
func nonoLidDenies() []string {
	root, err := panes.Root()
	if err != nil {
		return nil
	}
	dir, err := panes.Path()
	if err != nil {
		return nil
	}
	return []string{homeRelative(root), homeRelative(dir)}
}

func homeRelative(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, ok := strings.CutPrefix(path, filepath.Clean(home)+string(filepath.Separator)); ok {
		return "$HOME/" + rel
	}
	return path
}

func nonoAgentNames() []string {
	names := make([]string, 0, len(nonoAgentGroups))
	for name := range nonoAgentGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func checkNonoVersion(output string) error {
	return checkMinVersion(minVersion{
		tool:    "nono",
		command: "nono --version",
		min:     nonoMinVersion,
		why:     "lid's profiles need",
		which:   "the sandbox tool",
	}, output)
}

type nonoSession struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

func (n nono) Status(h Host, cfg *config.Config, opts StatusOptions) (State, error) {
	profile, err := os.ReadFile(nonoProfilePath(cfg))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, failure.New("cannot read the profile lid wrote for %s: %s", cfg.Name, err).
			At(nonoProfileRef(cfg)).
			WithFix("check who can read that file, then run 'lid start --rebuild' to write it again")
	}

	state := State{Exists: true, Rows: [][2]string{{"profile", nonoProfileRef(cfg)}}}
	if opts.Stale {
		if rendered, err := n.Render(cfg); err == nil {
			state.Stale = string(profile) != rendered
		}
	}

	sessions, err := nonoSessions(h, cfg)
	if err != nil {
		return State{}, err
	}
	state.Running = len(sessions) > 0
	if state.Running {
		state.Rows = append(state.Rows, [2]string{"sessions", strconv.Itoa(len(sessions))})
	}
	return state, nil
}

func (nono) Stop(h Host, cfg *config.Config) error {
	sessions, err := nonoSessions(h, cfg)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		id := session.ID
		if id == "" {
			id = session.Name
		}
		if err := h.exec([]string{"nono", "stop", id}); err != nil {
			return failure.New("cannot stop the nono session %s", id).
				WithFix("the output above says why; stop it yourself with 'nono stop %s'", id)
		}
	}
	return nil
}

func nonoSessions(h Host, cfg *config.Config) ([]nonoSession, error) {
	out, err := h.output([]string{"nono", "ps", "--json"})
	if err != nil {
		return nil, failure.New("cannot list the sessions nono is running: %s", firstLine(out)).
			WithFix("check that 'nono ps' runs, then try again")
	}

	sessions, err := parseList[nonoSession](out, "nono ps --json", "sessions")
	if err != nil {
		return nil, err
	}

	path := nonoProfilePath(cfg)
	matched := make([]nonoSession, 0, len(sessions))
	for _, session := range sessions {
		if session.Profile == path || session.Profile == cfg.Name || session.Name == cfg.Name {
			matched = append(matched, session)
		}
	}
	return matched, nil
}
