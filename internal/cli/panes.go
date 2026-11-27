package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/herdr"
	"github.com/neoighodaro/lid/internal/panes"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

const (
	paneTimeout    = 15 * time.Second
	panesNonoGrant = "localhost"
	profilePrefix  = "profile-"
	profileSuffix  = ".jsonc"
)

const panesPromptText = `You are running in a Herdr terminal. When the user asks you to run something in another pane (or a new/separate pane), open one with:
curl -fsS -X POST %s/panes -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -d '{"command": "<shell command>"}'
The pane runs the command inside this same sandbox, in the project directory, and the user can take it over. You cannot read its output; if you need it, run the command as '<cmd> 2>&1 | tee .lid-pane.log' and read that file.
Only open a pane when the user asks for one.`

func panesWanted(a *app.App, cfg *config.Config) bool {
	if !cfg.Herdr.Panes {
		return false
	}
	switch {
	case a.Herdr.TerminalID == "":
		fmt.Fprintln(a.Err, ui.Err.Info.Render("herdr.panes is on, but lid is not running in Herdr"))
		return false
	case !config.IsClaudeAgent(cfg.Agent):
		fmt.Fprintln(a.Err, ui.Err.Warn.Render("herdr.panes is on, but the agent is "+cfg.Agent+" and only claude-code can open panes"))
		return false
	}
	return true
}

type paneSession struct {
	listener *panes.Listener
	host     backend.Host
	profile  string
	body     []byte
	once     sync.Once
}

func (s *paneSession) close() {
	s.once.Do(func() {
		s.listener.Close()
		if s.profile != "" {
			os.Remove(s.profile)
		}
	})
}

func (s *paneSession) verify() error {
	if s.profile == "" {
		return nil
	}
	return panes.Verify(s.profile, s.body)
}

// openPanes starts this session's pane listener, or returns nil when panes are
// off. On nono it also renders the session's private profile, the only one
// that lets the agent and its panes reach the listener.
func openPanes(a *app.App, b backend.Backend, cfg *config.Config) (*paneSession, error) {
	if !panesWanted(a, cfg) {
		return nil, nil
	}
	bins, exe, err := paneBinaries(a, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := panes.Dir(); err != nil {
		return nil, err
	}
	s := &paneSession{host: pinnedHost(boundedHost(a.SandboxHost()), bins)}
	if cfg.Backend == config.BackendNono {
		if s.profile, s.body, err = writePrivateProfile(s.host, b, cfg); err != nil {
			return nil, err
		}
	}
	h := a.Herdr
	h.Bin = bins[herdrApp]
	s.listener, err = panes.Listen(func(command string) error {
		return openPane(s, b, cfg, h, exe, command)
	})
	if err != nil {
		if s.profile != "" {
			os.Remove(s.profile)
		}
		return nil, err
	}
	return s, nil
}

const herdrApp = "herdr-app"

// paneBinaries finds, once, every program a pane request runs on the host, and
// refuses any the agent could replace.
func paneBinaries(a *app.App, cfg *config.Config) (map[string]string, string, error) {
	h := a.SandboxHost()
	bins := make(map[string]string, 2)
	for _, name := range []string{cfg.Backend, herdrApp} {
		path, err := resolveBinary(h, name)
		if err != nil {
			return nil, "", fmt.Errorf("cannot find %s: %v", name, err)
		}
		bins[name] = path
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return nil, "", fmt.Errorf("cannot find lid's own binary: %v", err)
	}
	roots := agentWritableRoots(cfg)
	for _, path := range []string{bins[cfg.Backend], bins[herdrApp], exe} {
		for _, root := range roots {
			if within(root, path) {
				return nil, "", fmt.Errorf("%s is inside %s, which the agent can write", path, root)
			}
		}
	}
	return bins, exe, nil
}

func resolveBinary(h backend.Host, name string) (string, error) {
	look := h.LookPath
	if look == nil {
		look = exec.LookPath
	}
	path, err := look(name)
	if err != nil {
		return "", err
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func agentWritableRoots(cfg *config.Config) []string {
	home, _ := os.UserHomeDir()
	roots := []string{cfg.ProjectDir}
	for _, entry := range cfg.ExtraWorkspaces {
		w, err := config.ParseExtraWorkspace(entry)
		if err != nil {
			continue
		}
		host := w.Host
		if host == "~" {
			host = home
		} else if rest, ok := strings.CutPrefix(host, "~/"); ok {
			host = filepath.Join(home, rest)
		}
		roots = append(roots, host)
	}
	for i, root := range roots {
		if real, err := filepath.EvalSymlinks(root); err == nil {
			roots[i] = real
		} else if abs, err := filepath.Abs(root); err == nil {
			roots[i] = abs
		}
	}
	return roots
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pinnedHost runs only the binaries resolved when the session started, so a
// program the agent puts on PATH later is never the one a pane runs.
func pinnedHost(h backend.Host, bins map[string]string) backend.Host {
	pin := func(argv []string) []string {
		if path, ok := bins[argv[0]]; ok {
			return append([]string{path}, argv[1:]...)
		}
		return argv
	}
	output, run := h.Output, h.Exec
	h.LookPath = func(name string) (string, error) {
		if path, ok := bins[name]; ok {
			return path, nil
		}
		return "", fmt.Errorf("%s was not found when the session started", name)
	}
	h.Output = func(argv []string) ([]byte, error) {
		return output(pin(argv))
	}
	h.Exec = func(argv []string, stdout, stderr io.Writer) error {
		argv = pin(argv)
		if run != nil {
			return run(argv, stdout, stderr)
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		return cmd.Run()
	}
	return h
}

// runAgent provisions the sandbox and hands the terminal to the agent, with a
// pane listener for the session when panes are on.
func runAgent(cmd *cobra.Command, a *app.App, b backend.Backend, cfg *config.Config, opts backend.Options, agentArgs []string) error {
	var held chan os.Signal
	defer func() {
		if held != nil {
			signal.Stop(held)
		}
	}()

	removeLeftoverPaneFiles(a, b, cfg)

	session, err := openPanes(a, b, cfg)
	if err != nil {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render("cannot open Herdr panes for this session: "+err.Error()))
		session = nil
	}
	if session != nil {
		defer session.close()
	}

	provisioned, err := provisionSandbox(cmd, a, b, cfg, opts)
	if err != nil {
		return err
	}
	if err := rememberIfNamed(cfg); err != nil {
		return err
	}

	profile := provisioned.Path
	if session != nil {
		held = make(chan os.Signal, 1)
		signal.Notify(held, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
		undo := func() {}
		err := session.verify()
		if err == nil {
			undo, err = allowPanes(a, b, cfg, session)
		}
		if err != nil {
			fmt.Fprintln(a.Err, ui.Err.Warn.Render("cannot open Herdr panes for this session: "+err.Error()))
			session.close()
			signal.Stop(held)
			held = nil
		} else {
			defer undo()
			agentArgs = append(slices.Clip(agentArgs), "--append-system-prompt", panesPrompt(cfg, session.listener))
			if session.profile != "" {
				profile = session.profile
			}
		}
	}

	argv, err := b.StartArgv(cfg, profile, agentArgs)
	if err != nil {
		return err
	}
	return a.Launch(argv)
}

func openPane(s *paneSession, b backend.Backend, cfg *config.Config, h herdr.Session, exe, command string) error {
	if err := s.verify(); err != nil {
		return err
	}
	argv, err := b.PaneArgv(s.host, cfg, s.profile, command)
	if err != nil {
		return err
	}
	path, err := panes.WriteArgv(argv)
	if err != nil {
		return err
	}
	if err := h.SplitPane([]string{exe, "pane-run", path}); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// writePrivateProfile renders the nono profile with the listener grant into
// lid's private dir, so neither the agent nor a pane runs under a profile the
// agent could rewrite, and the project's own profile never carries the grant.
func writePrivateProfile(h backend.Host, b backend.Backend, cfg *config.Config) (string, []byte, error) {
	granted := *cfg
	if !slices.Contains(granted.Network, panesNonoGrant) {
		granted.Network = append(slices.Clone(granted.Network), panesNonoGrant)
	}
	rendered, err := b.Render(&granted)
	if err != nil {
		return "", nil, err
	}
	body := []byte(rendered)
	var name [16]byte
	if _, err := rand.Read(name[:]); err != nil {
		return "", nil, err
	}
	path, err := panes.Create(fmt.Sprintf("%s%d-%s%s", profilePrefix, os.Getpid(), hex.EncodeToString(name[:]), profileSuffix), body)
	if err != nil {
		return "", nil, err
	}
	if out, err := h.Output([]string{config.BackendNono, "profile", "validate", path}); err != nil {
		os.Remove(path)
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return "", nil, fmt.Errorf("nono rejected the session's profile: %s", line)
	}
	if err := panes.Verify(path, body); err != nil {
		os.Remove(path)
		return "", nil, err
	}
	return path, body, nil
}

func removeOrphanedProfiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		rest, ok := strings.CutPrefix(entry.Name(), profilePrefix)
		if !ok || !strings.HasSuffix(rest, profileSuffix) {
			continue
		}
		pid, _, _ := strings.Cut(rest, "-")
		if n, err := strconv.Atoi(pid); err == nil && alive(n) {
			continue
		}
		os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func boundedHost(h backend.Host) backend.Host {
	if h.Output == nil {
		h.Output = func(argv []string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), paneTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.WaitDelay = time.Second
			return cmd.CombinedOutput()
		}
	}
	return h
}

func quietHost(a *app.App) backend.Host {
	return quiet(a.SandboxHost())
}

func quiet(h backend.Host) backend.Host {
	h.Stdout, h.Stderr = io.Discard, io.Discard
	return h
}

// allowPanes lets an sbx sandbox reach the listener. The rule is recorded
// before it is added, so a session that dies at any point without cleaning up
// has it removed by the next start or stop.
func allowPanes(a *app.App, b backend.Backend, cfg *config.Config, s *paneSession) (func(), error) {
	if cfg.Backend != config.BackendSbx {
		return func() {}, nil
	}
	dir, err := panes.Dir()
	if err != nil {
		return nil, err
	}

	port := s.listener.Port()
	resource := fmt.Sprintf("localhost:%d", port)
	record := filepath.Join(dir, fmt.Sprintf("%s.%d.port", cfg.Name, port))
	if err := os.WriteFile(record, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return nil, err
	}
	remove := func() {
		if b.Network(quiet(s.host), cfg, backend.NetworkRemove, []string{resource}) == nil {
			os.Remove(record)
		}
	}
	if err := b.Network(s.host, cfg, backend.NetworkAllow, []string{resource}); err != nil {
		b.Network(quiet(s.host), cfg, backend.NetworkRemove, []string{resource})
		os.Remove(record)
		return nil, err
	}
	return remove, nil
}

func removeLeftoverPaneRules(a *app.App, b backend.Backend, cfg *config.Config, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		port, ok := strings.CutPrefix(entry.Name(), cfg.Name+".")
		if !ok {
			continue
		}
		if port, ok = strings.CutSuffix(port, ".port"); !ok {
			continue
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			continue
		}
		record := filepath.Join(dir, entry.Name())
		if data, err := os.ReadFile(record); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && alive(pid) {
				continue
			}
		}
		if b.Network(quietHost(a), cfg, backend.NetworkRemove, []string{"localhost:" + port}) == nil {
			os.Remove(record)
		}
	}
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// removeLeftoverPaneFiles clears what dead sessions left, without creating
// lid's pane dir for a user who never turned panes on.
func removeLeftoverPaneFiles(a *app.App, b backend.Backend, cfg *config.Config) {
	path, err := panes.Path()
	if err != nil {
		return
	}
	if _, err := os.Lstat(path); err != nil {
		return
	}
	dir, err := panes.Dir()
	if err != nil {
		return
	}
	removeOrphanedProfiles(dir)
	if cfg.Backend == config.BackendSbx {
		removeLeftoverPaneRules(a, b, cfg, dir)
	}
}

func panesPrompt(cfg *config.Config, l *panes.Listener) string {
	host := "localhost"
	if cfg.BuildsImage() {
		host = "host.docker.internal"
	}
	return fmt.Sprintf(panesPromptText, fmt.Sprintf("http://%s:%d", host, l.Port()), l.Token)
}
